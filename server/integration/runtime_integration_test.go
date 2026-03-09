package integration

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"

	"github.com/openrtc/openrtc/server/internal/admin"
	"github.com/openrtc/openrtc/server/internal/config"
	runtimeapp "github.com/openrtc/openrtc/server/internal/runtime"
)

func TestRuntimeSingleNodeLifecycle(t *testing.T) {
	jwks, signToken := newJWKS(t)
	defer jwks.Close()

	cfg, err := config.LoadFromMap(map[string]string{
		"OPENRTC_NODE_ID":             "node-a",
		"OPENRTC_AUTH_ISSUER":         "https://issuer.example.com",
		"OPENRTC_AUTH_AUDIENCE":       "openrtc-clients",
		"OPENRTC_AUTH_JWKS_URL":       jwks.URL,
		"OPENRTC_ADMIN_AUTH_ISSUER":   "https://issuer.example.com",
		"OPENRTC_ADMIN_AUTH_AUDIENCE": "openrtc-admin",
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	runtimeService, err := runtimeapp.NewService(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	defer runtimeService.Close()

	server := httptest.NewServer(runtimeService.Handler())
	defer server.Close()

	conn := wsConnect(t, server.URL+cfg.Server.WSPath+"?token="+signToken(t, "openrtc-clients", map[string]any{
		"tenant":   "tenant-a",
		"join":     []string{"tenant-a:*"},
		"publish":  []string{"tenant-a:*"},
		"presence": []string{"tenant-a:*"},
	}))
	defer conn.Close()

	readJSON(t, conn) // HELLO

	mustWriteJSON(t, conn, map[string]any{"t": "JOIN", "id": "req-join", "room": "tenant-a:room-1"})
	joined := readJSON(t, conn)
	if joined["t"] != "JOINED" {
		t.Fatalf("expected JOINED, got %#v", joined)
	}

	mustWriteJSON(t, conn, map[string]any{
		"t":       "PRESENCE_SET",
		"id":      "req-presence",
		"room":    "tenant-a:room-1",
		"payload": map[string]any{"status": "online"},
	})
	presence := readJSON(t, conn)
	if presence["t"] != "PRESENCE" {
		t.Fatalf("expected PRESENCE, got %#v", presence)
	}

	mustWriteJSON(t, conn, map[string]any{
		"t":       "EMIT",
		"id":      "req-emit",
		"room":    "tenant-a:room-1",
		"event":   "chat.message",
		"payload": map[string]any{"text": "hello"},
	})
	event := readJSON(t, conn)
	if event["t"] != "EVENT" || event["event"] != "chat.message" {
		t.Fatalf("expected EVENT, got %#v", event)
	}

	mustWriteJSON(t, conn, map[string]any{"t": "LEAVE", "id": "req-leave", "room": "tenant-a:room-1"})
	left := readJSON(t, conn)
	if left["t"] != "LEFT" {
		t.Fatalf("expected LEFT, got %#v", left)
	}
}

func TestAdminPublishReachesClusterRuntime(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	jwks, signToken := newJWKS(t)
	defer jwks.Close()

	base := map[string]string{
		"OPENRTC_MODE":                "cluster",
		"OPENRTC_REDIS_URL":           "redis://" + redisServer.Addr(),
		"OPENRTC_NODE_ID":             "node-a",
		"OPENRTC_AUTH_ISSUER":         "https://issuer.example.com",
		"OPENRTC_AUTH_AUDIENCE":       "openrtc-clients",
		"OPENRTC_AUTH_JWKS_URL":       jwks.URL,
		"OPENRTC_ADMIN_AUTH_ISSUER":   "https://issuer.example.com",
		"OPENRTC_ADMIN_AUTH_AUDIENCE": "openrtc-admin",
	}

	runtimeCfg, err := config.LoadFromMap(base)
	if err != nil {
		t.Fatalf("runtime config: %v", err)
	}
	runtimeService, err := runtimeapp.NewService(runtimeCfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	defer runtimeService.Close()
	runtimeServer := httptest.NewServer(runtimeService.Handler())
	defer runtimeServer.Close()

	adminCfg, err := config.LoadFromMap(base)
	if err != nil {
		t.Fatalf("admin config: %v", err)
	}
	adminService, err := admin.NewService(adminCfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("new admin: %v", err)
	}
	defer adminService.Close()
	adminServer := httptest.NewServer(adminService.Handler())
	defer adminServer.Close()

	conn := wsConnect(t, runtimeServer.URL+runtimeCfg.Server.WSPath+"?token="+signToken(t, "openrtc-clients", map[string]any{
		"tenant":   "tenant-a",
		"join":     []string{"tenant-a:*"},
		"publish":  []string{"tenant-a:*"},
		"presence": []string{"tenant-a:*"},
	}))
	defer conn.Close()

	readJSON(t, conn) // HELLO
	mustWriteJSON(t, conn, map[string]any{"t": "JOIN", "id": "req-join", "room": "tenant-a:room-1"})
	_ = readJSON(t, conn)

	requestBody := map[string]any{
		"room":    "tenant-a:room-1",
		"event":   "admin.broadcast",
		"payload": map[string]any{"message": "admin"},
	}
	body, _ := json.Marshal(requestBody)
	request, _ := http.NewRequest(http.MethodPost, adminServer.URL+"/v1/publish", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+signToken(t, "openrtc-admin", map[string]any{
		"tenant": "tenant-a",
		"scope":  "publish:tenant-a:*",
	}))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("publish request: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("unexpected publish status: %d", response.StatusCode)
	}

	event := readJSON(t, conn)
	if event["t"] != "EVENT" || event["event"] != "admin.broadcast" {
		t.Fatalf("expected broadcast event, got %#v", event)
	}

	statsRequest, _ := http.NewRequest(http.MethodGet, adminServer.URL+"/v1/stats", nil)
	statsRequest.Header.Set("Authorization", "Bearer "+signToken(t, "openrtc-admin", map[string]any{
		"tenant": "tenant-a",
		"scope":  "publish:tenant-a:*",
	}))
	statsResponse, err := http.DefaultClient.Do(statsRequest)
	if err != nil {
		t.Fatalf("stats request: %v", err)
	}
	defer statsResponse.Body.Close()
	if statsResponse.StatusCode != http.StatusOK {
		t.Fatalf("unexpected stats status: %d", statsResponse.StatusCode)
	}
}

func newJWKS(t *testing.T) (*httptest.Server, func(t *testing.T, audience string, extra map[string]any) string) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{
				{
					"kty": "RSA",
					"kid": "integration-key",
					"n":   base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
					"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.PublicKey.E)).Bytes()),
				},
			},
		})
	}))

	sign := func(t *testing.T, audience string, extra map[string]any) string {
		t.Helper()

		claims := jwt.MapClaims{
			"iss": "https://issuer.example.com",
			"aud": audience,
			"exp": time.Now().Add(time.Hour).Unix(),
			"sub": "user-1",
		}
		for key, value := range extra {
			claims[key] = value
		}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		token.Header["kid"] = "integration-key"
		raw, err := token.SignedString(privateKey)
		if err != nil {
			t.Fatalf("sign token: %v", err)
		}
		return raw
	}

	return server, sign
}

func wsConnect(t *testing.T, rawURL string) *websocket.Conn {
	t.Helper()
	httpURL := strings.Replace(rawURL, "http://", "ws://", 1)
	conn, _, err := websocket.DefaultDialer.Dial(httpURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	return conn
}

func mustWriteJSON(t *testing.T, conn *websocket.Conn, payload map[string]any) {
	t.Helper()
	if err := conn.WriteJSON(payload); err != nil {
		t.Fatalf("write json: %v", err)
	}
}

func readJSON(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var payload map[string]any
	if err := conn.ReadJSON(&payload); err != nil {
		t.Fatalf("read json: %v", err)
	}
	return payload
}
