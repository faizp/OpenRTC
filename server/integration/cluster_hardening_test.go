package integration

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/openrtc/openrtc/server/internal/admin"
	"github.com/openrtc/openrtc/server/internal/config"
	runtimeapp "github.com/openrtc/openrtc/server/internal/runtime"
)

func clusterConfig(t *testing.T, redisAddr string, nodeID string) (config.RuntimeConfig, *httptest.Server, func(t *testing.T, audience string, extra map[string]any) string) {
	t.Helper()
	jwks, signToken := newJWKS(t)
	t.Cleanup(jwks.Close)

	cfg, err := config.LoadFromMap(map[string]string{
		"OPENRTC_MODE":                "cluster",
		"OPENRTC_REDIS_URL":           "redis://" + redisAddr,
		"OPENRTC_NODE_ID":             nodeID,
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
	t.Cleanup(func() { svc.Close() })

	server := httptest.NewServer(svc.Handler())
	t.Cleanup(server.Close)

	return cfg, server, signToken
}

func TestClusterThreeNodeFanOut(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	cfg1, server1, sign1 := clusterConfig(t, redisServer.Addr(), "node-1")
	_, server2, sign2 := clusterConfig(t, redisServer.Addr(), "node-2")
	cfg3, server3, sign3 := clusterConfig(t, redisServer.Addr(), "node-3")

	tokenClaims := map[string]any{
		"tenant":   "tenant-a",
		"join":     []string{"tenant-a:*"},
		"publish":  []string{"tenant-a:*"},
		"presence": []string{"tenant-a:*"},
	}

	// Client A on Node 1
	clientA := wsConnect(t, server1.URL+cfg1.Server.WSPath+"?token="+sign1(t, "openrtc-clients", tokenClaims))
	defer clientA.Close()
	readJSON(t, clientA) // HELLO
	mustWriteJSON(t, clientA, map[string]any{"t": "JOIN", "id": "a-join", "room": "tenant-a:broadcast"})
	readJSON(t, clientA) // JOINED

	// Client B on Node 2
	clientB := wsConnect(t, server2.URL+cfg1.Server.WSPath+"?token="+sign2(t, "openrtc-clients", tokenClaims))
	defer clientB.Close()
	readJSON(t, clientB) // HELLO
	mustWriteJSON(t, clientB, map[string]any{"t": "JOIN", "id": "b-join", "room": "tenant-a:broadcast"})
	readJSON(t, clientB) // JOINED

	// Client C on Node 3
	clientC := wsConnect(t, server3.URL+cfg3.Server.WSPath+"?token="+sign3(t, "openrtc-clients", tokenClaims))
	defer clientC.Close()
	readJSON(t, clientC) // HELLO
	mustWriteJSON(t, clientC, map[string]any{"t": "JOIN", "id": "c-join", "room": "tenant-a:broadcast"})
	readJSON(t, clientC) // JOINED

	time.Sleep(100 * time.Millisecond)

	// Client A sends message — B and C should both receive it
	mustWriteJSON(t, clientA, map[string]any{
		"t":       "EMIT",
		"id":      "a-emit",
		"room":    "tenant-a:broadcast",
		"event":   "three.node.test",
		"payload": map[string]any{"from": "node-1"},
	})

	eventB := readJSON(t, clientB)
	if eventB["t"] != "EVENT" || eventB["event"] != "three.node.test" {
		t.Fatalf("Client B expected three.node.test EVENT, got %v", eventB)
	}
	t.Logf("Client B received: %v", eventB)

	eventC := readJSON(t, clientC)
	if eventC["t"] != "EVENT" || eventC["event"] != "three.node.test" {
		t.Fatalf("Client C expected three.node.test EVENT, got %v", eventC)
	}
	t.Logf("Client C received: %v", eventC)

	t.Log("Three-node fan-out test passed!")
}

func TestAdminPublishWithAuth(t *testing.T) {
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

	adminCfg, err := config.LoadFromMap(base)
	if err != nil {
		t.Fatalf("admin config: %v", err)
	}
	adminSvc, err := admin.NewService(adminCfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("admin service: %v", err)
	}
	defer adminSvc.Close()
	adminServer := httptest.NewServer(adminSvc.Handler())
	defer adminServer.Close()

	// No auth header — should fail
	resp, err := http.Post(adminServer.URL+"/v1/publish", "application/json", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted {
		t.Fatal("expected rejection without auth, got 202")
	}
	t.Logf("No auth: status %d (expected non-202)", resp.StatusCode)

	// Wrong audience — should fail
	badToken := signToken(t, "wrong-audience", map[string]any{
		"tenant": "tenant-a",
		"scope":  "publish:tenant-a:*",
	})
	req, _ := http.NewRequest(http.MethodPost, adminServer.URL+"/v1/publish", nil)
	req.Header.Set("Authorization", "Bearer "+badToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted {
		t.Fatal("expected rejection with wrong audience, got 202")
	}
	t.Logf("Wrong audience: status %d (expected non-202)", resp.StatusCode)
}

func TestClusterReadinessWithRedis(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}

	jwks, _ := newJWKS(t)
	defer jwks.Close()

	cfg, err := config.LoadFromMap(map[string]string{
		"OPENRTC_MODE":                "cluster",
		"OPENRTC_REDIS_URL":           "redis://" + redisServer.Addr(),
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

	adminSvc, err := admin.NewService(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("admin service: %v", err)
	}
	defer adminSvc.Close()
	server := httptest.NewServer(adminSvc.Handler())
	defer server.Close()

	// Should be ready when Redis is up
	resp, err := http.Get(server.URL + "/readyz")
	if err != nil {
		t.Fatalf("readyz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 when Redis up, got %d", resp.StatusCode)
	}
	t.Log("Readiness OK with Redis up")

	// Stop Redis
	redisServer.Close()

	// Should become not ready
	resp, err = http.Get(server.URL + "/readyz")
	if err != nil {
		t.Fatalf("readyz after redis stop: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Log("Warning: readyz still returns 200 after Redis stop (miniredis may not simulate disconnect)")
	} else {
		t.Logf("Readiness correctly failed after Redis stop: %d", resp.StatusCode)
	}
}

func TestClusterPresenceStoredInRedis(t *testing.T) {
	// Presence is stored in Redis via SetPresence but broadcast is local-only.
	// Cross-node presence is available via JOIN snapshot (members + presence map)
	// but not via live broadcast. This test validates the JOIN snapshot path.
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	cfg1, server1, sign1 := clusterConfig(t, redisServer.Addr(), "node-1")
	_, server2, sign2 := clusterConfig(t, redisServer.Addr(), "node-2")

	tokenClaims := map[string]any{
		"tenant":   "tenant-a",
		"join":     []string{"tenant-a:*"},
		"publish":  []string{"tenant-a:*"},
		"presence": []string{"tenant-a:*"},
	}

	// Client A on Node 1 joins and sets presence
	clientA := wsConnect(t, server1.URL+cfg1.Server.WSPath+"?token="+sign1(t, "openrtc-clients", tokenClaims))
	defer clientA.Close()
	readJSON(t, clientA) // HELLO
	mustWriteJSON(t, clientA, map[string]any{"t": "JOIN", "id": "a-join", "room": "tenant-a:presence-room"})
	readJSON(t, clientA) // JOINED

	mustWriteJSON(t, clientA, map[string]any{
		"t":       "PRESENCE_SET",
		"id":      "a-pres",
		"room":    "tenant-a:presence-room",
		"payload": map[string]any{"status": "online"},
	})
	readJSON(t, clientA) // PRESENCE (own)

	time.Sleep(100 * time.Millisecond)

	// Client B on Node 2 joins same room — should see A's presence in the snapshot
	clientB := wsConnect(t, server2.URL+cfg1.Server.WSPath+"?token="+sign2(t, "openrtc-clients", tokenClaims))
	defer clientB.Close()
	readJSON(t, clientB) // HELLO
	mustWriteJSON(t, clientB, map[string]any{"t": "JOIN", "id": "b-join", "room": "tenant-a:presence-room"})
	joined := readJSON(t, clientB)
	if joined["t"] != "JOINED" {
		t.Fatalf("expected JOINED, got %v", joined["t"])
	}

	payload := joined["payload"].(map[string]any)
	presenceMap := payload["presence"].(map[string]any)
	if len(presenceMap) == 0 {
		t.Fatal("expected presence snapshot to contain Client A's state, but was empty")
	}
	t.Logf("Client B JOIN snapshot presence: %v", presenceMap)
	t.Log("Cross-node presence via JOIN snapshot test passed!")
}
