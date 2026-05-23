package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestVerifierValidateAndClaimsAuthorize(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{
				{
					"kty": "RSA",
					"kid": "runtime-key",
					"n":   base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
					"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.PublicKey.E)).Bytes()),
				},
			},
		})
	}))
	defer jwksServer.Close()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    "https://issuer.example.com",
			Audience:  []string{"openrtc-clients"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		Tenant:   "tenant-a",
		Join:     []string{"tenant-a:*"},
		Publish:  []string{"tenant-a:*"},
		Groups:   []string{"legacy-group", "engineering"},
		GroupIDs: []string{"engineering", "design"},
	})
	token.Header["kid"] = "runtime-key"

	rawToken, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	verifier := NewVerifier("https://issuer.example.com", "openrtc-clients", jwksServer.URL)
	claims, err := verifier.Verify(context.Background(), rawToken)
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}

	if !claims.Allows("join", "tenant-a:room-1", true, ":") {
		t.Fatalf("expected join to be allowed")
	}
	if claims.Allows("publish", "tenant-b:room-1", true, ":") {
		t.Fatalf("expected publish to be rejected")
	}
	if claims.Allows("presence", "tenant-a:room-1", true, ":") {
		t.Fatalf("expected missing presence action claim to be rejected")
	}
	groups := claims.RoomGroupIDs()
	if len(groups) != 3 || groups[0] != "engineering" || groups[1] != "design" || groups[2] != "legacy-group" {
		t.Fatalf("unexpected room group ids: %#v", groups)
	}
	claims.Tenant = ""
	if claims.Allows("join", "tenant-a:room-1", true, ":") {
		t.Fatalf("expected prefix enforcement to require a tenant claim")
	}
	if (&Claims{Join: []string{"["}}).Allows("join", "tenant-a:room-1", false, ":") {
		t.Fatalf("expected invalid glob pattern to be ignored")
	}
	groups = (&Claims{
		GroupIDs: []string{"", "engineering", "engineering"},
		Groups:   []string{"", "design", "engineering"},
	}).RoomGroupIDs()
	if len(groups) != 2 || groups[0] != "engineering" || groups[1] != "design" {
		t.Fatalf("unexpected normalized room group ids: %#v", groups)
	}
}

func TestVerifierRejectsInvalidTokens(t *testing.T) {
	verifier := NewVerifier("https://issuer.example.com", "openrtc-clients", "https://issuer.example.com/jwks.json")
	if _, err := verifier.Verify(context.Background(), "not-a-jwt"); err == nil {
		t.Fatalf("expected malformed token to fail")
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": "https://issuer.example.com",
		"aud": "openrtc-clients",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	raw, err := token.SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("sign hmac token: %v", err)
	}
	if _, err := verifier.Verify(context.Background(), raw); err == nil {
		t.Fatalf("expected unsupported signing method to fail")
	}
}

func TestVerifierRejectsJWKSHTTPError(t *testing.T) {
	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer jwksServer.Close()

	verifier := NewVerifier("https://issuer.example.com", "openrtc-clients", jwksServer.URL)
	_, err := verifier.lookupKey(context.Background(), "runtime-key")
	if err == nil {
		t.Fatalf("expected jwks status error")
	}
}

func TestVerifierLookupKeyCacheBranches(t *testing.T) {
	key := &rsa.PublicKey{N: big.NewInt(3), E: 65537}
	verifier := NewVerifier("https://issuer.example.com", "openrtc-clients", "https://issuer.example.com/jwks.json")
	verifier.keys = map[string]*rsa.PublicKey{"runtime-key": key}
	verifier.cachedAt = time.Now()
	verifier.cachedTTL = time.Hour

	got, err := verifier.lookupKey(context.Background(), "")
	if err != nil {
		t.Fatalf("lookup single cached key without kid: %v", err)
	}
	if got != key {
		t.Fatalf("expected cached key")
	}

	if _, err := verifier.lookupKey(context.Background(), "missing-key"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing key error, got %v", err)
	}
}

func TestVerifierFetchKeysRejectsMalformedJWKS(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{
			name: "invalid json",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"keys":`))
			},
			want: "unexpected EOF",
		},
		{
			name: "no rsa keys",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"keys": []map[string]any{{"kty": "oct", "kid": "oct-key"}},
				})
			},
			want: "no rsa keys",
		},
		{
			name: "invalid rsa key",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"keys": []map[string]any{{
						"kty": "RSA",
						"kid": "bad-rsa",
						"n":   "*",
						"e":   "AQAB",
					}},
				})
			},
			want: "illegal base64",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			jwksServer := httptest.NewServer(tc.handler)
			defer jwksServer.Close()

			verifier := NewVerifier("https://issuer.example.com", "openrtc-clients", jwksServer.URL)
			_, err := verifier.fetchKeys(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}

	verifier := NewVerifier("https://issuer.example.com", "openrtc-clients", "://bad-url")
	if _, err := verifier.fetchKeys(context.Background()); err == nil {
		t.Fatalf("expected invalid jwks URL to fail")
	}
}

func TestRSAKeyFromJWKRejectsInvalidComponents(t *testing.T) {
	if _, err := rsaKeyFromJWK("*", "AQAB"); err == nil {
		t.Fatalf("expected invalid modulus encoding to fail")
	}
	if _, err := rsaKeyFromJWK("AQAB", "*"); err == nil {
		t.Fatalf("expected invalid exponent encoding to fail")
	}
	if _, err := rsaKeyFromJWK("", ""); err == nil {
		t.Fatalf("expected empty key components to fail")
	}
}
