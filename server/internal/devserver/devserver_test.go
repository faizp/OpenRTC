package devserver

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"

	"github.com/openrtc/openrtc/server/internal/config"
)

func TestMainHelpReturnsSuccess(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Main([]string{"--help"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "Usage of openrtc dev") {
		t.Fatalf("expected help on stdout, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestMainRejectsUnexpectedArgs(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Main([]string{"extra"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "unexpected arguments: extra") {
		t.Fatalf("expected unexpected argument error, got %q", stderr.String())
	}
}

func TestHandleTokenAllowsLocalDevAnonymousAuth(t *testing.T) {
	privateKey = testPrivateKey(t)
	req := httptest.NewRequest(http.MethodGet, "/dev/token?pubkey=pk_localdev&tenant=acme&groups=editors,reviewers", nil)
	rec := httptest.NewRecorder()

	handleToken(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Token    string   `json:"token"`
		Kind     string   `json:"kind"`
		Username string   `json:"username"`
		Tenant   string   `json:"tenant"`
		Groups   []string `json:"groups"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if body.Token == "" {
		t.Fatalf("expected signed token")
	}
	if body.Kind != "client" {
		t.Fatalf("expected client token, got %q", body.Kind)
	}
	if !strings.HasPrefix(body.Username, "anon-") {
		t.Fatalf("expected anonymous username, got %q", body.Username)
	}
	if body.Tenant != "acme" {
		t.Fatalf("expected tenant acme, got %q", body.Tenant)
	}
	if len(body.Groups) != 2 || body.Groups[0] != "editors" || body.Groups[1] != "reviewers" {
		t.Fatalf("unexpected groups: %#v", body.Groups)
	}

	claims := parseTokenClaims(t, body.Token)
	if claims["iss"] != devIssuer {
		t.Fatalf("unexpected issuer: %v", claims["iss"])
	}
	if !hasAudience(claims["aud"], clientAudience) {
		t.Fatalf("expected audience %q, got %v", clientAudience, claims["aud"])
	}
	if claims["tenant"] != "acme" {
		t.Fatalf("unexpected tenant claim: %v", claims["tenant"])
	}
	if got := stringSliceClaim(claims["join"]); len(got) != 1 || got[0] != "*" {
		t.Fatalf("expected wildcard join grant, got %#v", got)
	}
	if got := stringSliceClaim(claims["presence"]); len(got) != 1 || got[0] != "*" {
		t.Fatalf("expected wildcard presence grant, got %#v", got)
	}
}

func TestHandleTokenRejectsUnknownPubkey(t *testing.T) {
	privateKey = testPrivateKey(t)
	req := httptest.NewRequest(http.MethodGet, "/dev/token?pubkey=pk_bad", nil)
	rec := httptest.NewRecorder()

	handleToken(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", rec.Code)
	}
}

func TestHandleTokenIssuesAdminScope(t *testing.T) {
	privateKey = testPrivateKey(t)
	req := httptest.NewRequest(http.MethodGet, "/dev/token?username=ops&kind=admin&scope=rooms:*", nil)
	rec := httptest.NewRecorder()

	handleToken(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Token string `json:"token"`
		Kind  string `json:"kind"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if body.Kind != "admin" {
		t.Fatalf("expected admin token, got %q", body.Kind)
	}
	claims := parseTokenClaims(t, body.Token)
	if !hasAudience(claims["aud"], adminAudience) {
		t.Fatalf("expected audience %q, got %v", adminAudience, claims["aud"])
	}
	if claims["scope"] != "rooms:*" {
		t.Fatalf("expected custom scope, got %v", claims["scope"])
	}
	if _, ok := claims["join"]; ok {
		t.Fatalf("admin token should not include client join grants")
	}
}

func TestDevConfigBuildsClusterRuntimeConfig(t *testing.T) {
	cfg, err := devConfig("dev-node", "127.0.0.1", 18080, "/ws", "http://127.0.0.1:3000", "redis://localhost:6379/0", "http://127.0.0.1:3000/jwks")
	if err != nil {
		t.Fatalf("devConfig: %v", err)
	}
	if cfg.Mode != config.ModeCluster {
		t.Fatalf("expected cluster mode, got %s", cfg.Mode)
	}
	if cfg.NodeID != "dev-node" || cfg.Server.Host != "127.0.0.1" || cfg.Server.Port != 18080 || cfg.Server.WSPath != "/ws" {
		t.Fatalf("unexpected server config: %+v node=%s", cfg.Server, cfg.NodeID)
	}
	if len(cfg.Server.AllowedOrigins) != 1 || cfg.Server.AllowedOrigins[0] != "http://127.0.0.1:3000" {
		t.Fatalf("unexpected origins: %#v", cfg.Server.AllowedOrigins)
	}
	if cfg.Redis == nil || cfg.Redis.URL != "redis://localhost:6379/0" || cfg.Redis.ChannelPrefix != "room:" {
		t.Fatalf("unexpected redis config: %+v", cfg.Redis)
	}
	if cfg.Auth.Issuer != devIssuer || cfg.Auth.Audience != clientAudience || cfg.Auth.JWKSURL != "http://127.0.0.1:3000/jwks" {
		t.Fatalf("unexpected auth config: %+v", cfg.Auth)
	}
	if cfg.AdminAuth == nil || cfg.AdminAuth.Issuer != devIssuer || cfg.AdminAuth.Audience != adminAudience {
		t.Fatalf("unexpected admin auth config: %+v", cfg.AdminAuth)
	}
	if cfg.Tenant.EnforcePrefix {
		t.Fatalf("expected tenant prefix enforcement disabled for local dev")
	}
}

func testPrivateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate private key: %v", err)
	}
	return key
}

func parseTokenClaims(t *testing.T, tokenString string) jwt.MapClaims {
	t.Helper()
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		return &privateKey.PublicKey, nil
	})
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	if !token.Valid {
		t.Fatalf("expected valid token")
	}
	return claims
}

func stringSliceClaim(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if value, ok := item.(string); ok {
			out = append(out, value)
		}
	}
	return out
}

func hasAudience(value any, audience string) bool {
	switch typed := value.(type) {
	case string:
		return typed == audience
	case []any:
		for _, item := range typed {
			if item == audience {
				return true
			}
		}
	case []string:
		for _, item := range typed {
			if item == audience {
				return true
			}
		}
	}
	return false
}
