package integration

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/openrtc/openrtc/server/internal/config"
	runtimeapp "github.com/openrtc/openrtc/server/internal/runtime"
)

func setupHardeningServer(t *testing.T, overrides map[string]string) (*httptest.Server, func(t *testing.T, audience string, extra map[string]any) string, config.RuntimeConfig) {
	t.Helper()
	jwks, signToken := newJWKS(t)
	t.Cleanup(jwks.Close)

	base := map[string]string{
		"OPENRTC_NODE_ID":             "node-a",
		"OPENRTC_AUTH_ISSUER":         "https://issuer.example.com",
		"OPENRTC_AUTH_AUDIENCE":       "openrtc-clients",
		"OPENRTC_AUTH_JWKS_URL":       jwks.URL,
		"OPENRTC_ADMIN_AUTH_ISSUER":   "https://issuer.example.com",
		"OPENRTC_ADMIN_AUTH_AUDIENCE": "openrtc-admin",
	}
	for k, v := range overrides {
		base[k] = v
	}

	cfg, err := config.LoadFromMap(base)
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	svc, err := runtimeapp.NewService(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	t.Cleanup(func() { svc.Close() })

	server := httptest.NewServer(svc.Handler())
	t.Cleanup(server.Close)

	return server, signToken, cfg
}

func clientToken(signToken func(t *testing.T, audience string, extra map[string]any) string, t *testing.T) string {
	return signToken(t, "openrtc-clients", map[string]any{
		"tenant":   "tenant-a",
		"join":     []string{"tenant-a:*"},
		"publish":  []string{"tenant-a:*"},
		"presence": []string{"tenant-a:*"},
	})
}

func TestRejectMissingAuth(t *testing.T) {
	server, _, cfg := setupHardeningServer(t, nil)

	// No token at all
	wsURL := strings.Replace(server.URL, "http://", "ws://", 1) + cfg.Server.WSPath
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected connection to fail without token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	// Invalid token
	_, resp, err = websocket.DefaultDialer.Dial(wsURL+"?token=garbage", nil)
	if err == nil {
		t.Fatal("expected connection to fail with invalid token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestRejectExpiredToken(t *testing.T) {
	jwks, _ := newJWKS(t)
	defer jwks.Close()

	_, signExpired := newJWKS(t)

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

	svc, err := runtimeapp.NewService(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	defer svc.Close()
	server := httptest.NewServer(svc.Handler())
	defer server.Close()

	// Token signed by different key should fail
	badToken := signExpired(t, "openrtc-clients", map[string]any{
		"tenant": "tenant-a",
		"join":   []string{"tenant-a:*"},
	})
	wsURL := strings.Replace(server.URL, "http://", "ws://", 1) + cfg.Server.WSPath + "?token=" + badToken
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected connection to fail with wrong-key token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestMaxRoomsPerConnection(t *testing.T) {
	server, signToken, cfg := setupHardeningServer(t, map[string]string{
		"OPENRTC_LIMIT_ROOMS_PER_CONNECTION": "3",
	})

	conn := wsConnect(t, server.URL+cfg.Server.WSPath+"?token="+clientToken(signToken, t))
	defer conn.Close()
	readJSON(t, conn) // HELLO

	// Join 3 rooms (should succeed)
	for i := 1; i <= 3; i++ {
		mustWriteJSON(t, conn, map[string]any{"t": "JOIN", "id": "j" + string(rune('0'+i)), "room": "tenant-a:room-" + string(rune('0'+i))})
		msg := readJSON(t, conn)
		if msg["t"] != "JOINED" {
			t.Fatalf("expected JOINED for room %d, got %v", i, msg["t"])
		}
	}

	// 4th room should fail
	mustWriteJSON(t, conn, map[string]any{"t": "JOIN", "id": "j4", "room": "tenant-a:room-4"})
	errMsg := readJSON(t, conn)
	if errMsg["t"] != "ERROR" {
		t.Fatalf("expected ERROR for 4th room, got %v", errMsg["t"])
	}
	t.Logf("Max rooms rejection: %v", errMsg)
}

func TestRoomForbiddenWithoutClaim(t *testing.T) {
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

	svc, err := runtimeapp.NewService(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	defer svc.Close()
	server := httptest.NewServer(svc.Handler())
	defer server.Close()

	// Token with tenant-a:* join claim trying to join tenant-b room
	restrictedToken := signToken(t, "openrtc-clients", map[string]any{
		"tenant": "tenant-a",
		"join":   []string{"tenant-a:*"},
	})
	conn := wsConnect(t, server.URL+cfg.Server.WSPath+"?token="+restrictedToken)
	defer conn.Close()
	readJSON(t, conn) // HELLO

	mustWriteJSON(t, conn, map[string]any{"t": "JOIN", "id": "j1", "room": "tenant-b:secret-room"})
	errMsg := readJSON(t, conn)
	if errMsg["t"] != "ERROR" {
		t.Fatalf("expected ERROR for forbidden room, got %v", errMsg["t"])
	}
	t.Logf("Room forbidden rejection: %v", errMsg)
}

func TestMalformedMessages(t *testing.T) {
	server, signToken, cfg := setupHardeningServer(t, nil)

	conn := wsConnect(t, server.URL+cfg.Server.WSPath+"?token="+clientToken(signToken, t))
	defer conn.Close()
	readJSON(t, conn) // HELLO

	tests := []struct {
		name string
		msg  map[string]any
	}{
		{"missing type", map[string]any{"id": "1", "room": "tenant-a:r"}},
		{"unknown type", map[string]any{"t": "INVALID_TYPE", "id": "1", "room": "tenant-a:r"}},
		{"join missing room", map[string]any{"t": "JOIN", "id": "1"}},
		{"emit missing event", map[string]any{"t": "EMIT", "id": "1", "room": "tenant-a:r", "payload": map[string]any{"x": 1}}},
		{"emit missing payload", map[string]any{"t": "EMIT", "id": "1", "room": "tenant-a:r", "event": "test"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mustWriteJSON(t, conn, tc.msg)
			errMsg := readJSON(t, conn)
			if errMsg["t"] != "ERROR" {
				t.Fatalf("expected ERROR for %s, got %v", tc.name, errMsg["t"])
			}
			t.Logf("%s → %v", tc.name, errMsg)
		})
	}
}

func TestEnvelopeTooLarge(t *testing.T) {
	// Set very small envelope limit
	server, signToken, cfg := setupHardeningServer(t, map[string]string{
		"OPENRTC_LIMIT_ENVELOPE_MAX_BYTES": "200",
		"OPENRTC_LIMIT_PAYLOAD_MAX_BYTES":  "100",
	})

	conn := wsConnect(t, server.URL+cfg.Server.WSPath+"?token="+clientToken(signToken, t))
	defer conn.Close()
	readJSON(t, conn) // HELLO

	mustWriteJSON(t, conn, map[string]any{"t": "JOIN", "id": "j1", "room": "tenant-a:r1"})
	joined := readJSON(t, conn)
	if joined["t"] != "JOINED" {
		t.Fatalf("expected JOINED, got %v", joined["t"])
	}

	// Send a message with a large payload that exceeds the limit
	largePayload := map[string]any{"data": strings.Repeat("x", 150)}
	mustWriteJSON(t, conn, map[string]any{
		"t":       "EMIT",
		"id":      "e1",
		"room":    "tenant-a:r1",
		"event":   "test",
		"payload": largePayload,
	})

	// Should get an error or the connection should close
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var msg map[string]any
	err := conn.ReadJSON(&msg)
	if err != nil {
		// Connection closed due to oversized message — expected
		t.Logf("Connection closed on oversized envelope (expected): %v", err)
		return
	}
	if msg["t"] == "ERROR" {
		t.Logf("Got error for oversized envelope: %v", msg)
		return
	}
	t.Fatalf("expected error or disconnect for oversized envelope, got %v", msg)
}

func TestRateLimiting(t *testing.T) {
	// Very low rate limit: 3 emits per second
	server, signToken, cfg := setupHardeningServer(t, map[string]string{
		"OPENRTC_LIMIT_EMITS_PER_SECOND": "3",
	})

	conn := wsConnect(t, server.URL+cfg.Server.WSPath+"?token="+clientToken(signToken, t))
	defer conn.Close()
	readJSON(t, conn) // HELLO

	mustWriteJSON(t, conn, map[string]any{"t": "JOIN", "id": "j1", "room": "tenant-a:r1"})
	readJSON(t, conn) // JOINED

	// Send more emits than the limit
	rateLimited := false
	for i := 0; i < 10; i++ {
		mustWriteJSON(t, conn, map[string]any{
			"t":       "EMIT",
			"id":      "e" + string(rune('0'+i)),
			"room":    "tenant-a:r1",
			"event":   "test",
			"payload": map[string]any{"i": i},
		})
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var msg map[string]any
		err := conn.ReadJSON(&msg)
		if err != nil {
			t.Logf("Connection closed after %d emits", i+1)
			rateLimited = true
			break
		}
		if msg["t"] == "ERROR" {
			code, _ := msg["payload"].(map[string]any)["code"]
			t.Logf("Rate limited after %d emits: code=%v", i+1, code)
			rateLimited = true
			break
		}
	}

	if !rateLimited {
		t.Fatal("expected rate limiting but all emits succeeded")
	}
}

func TestHealthAndReadyEndpoints(t *testing.T) {
	server, _, _ := setupHardeningServer(t, nil)

	// Healthz
	resp, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatalf("healthz request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /healthz, got %d", resp.StatusCode)
	}

	// Readyz
	resp, err = http.Get(server.URL + "/readyz")
	if err != nil {
		t.Fatalf("readyz request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /readyz, got %d", resp.StatusCode)
	}

	// Metrics
	resp, err = http.Get(server.URL + "/metrics")
	if err != nil {
		t.Fatalf("metrics request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /metrics, got %d", resp.StatusCode)
	}
}

func TestTenantPrefixEnforcement(t *testing.T) {
	server, signToken, cfg := setupHardeningServer(t, nil)

	token := signToken(t, "openrtc-clients", map[string]any{
		"tenant":   "tenant-a",
		"join":     []string{"tenant-a:*"},
		"publish":  []string{"tenant-a:*"},
		"presence": []string{"tenant-a:*"},
	})
	conn := wsConnect(t, server.URL+cfg.Server.WSPath+"?token="+token)
	defer conn.Close()
	readJSON(t, conn) // HELLO

	// Try joining a room without tenant prefix
	mustWriteJSON(t, conn, map[string]any{"t": "JOIN", "id": "j1", "room": "no-prefix-room"})
	errMsg := readJSON(t, conn)
	if errMsg["t"] != "ERROR" {
		t.Fatalf("expected ERROR for room without tenant prefix, got %v", errMsg["t"])
	}
	t.Logf("Tenant prefix enforcement: %v", errMsg)
}
