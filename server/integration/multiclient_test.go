package integration

import (
	"io"
	"log"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/openrtc/openrtc/server/internal/config"
	runtimeapp "github.com/openrtc/openrtc/server/internal/runtime"
)

func TestTwoClientsMessaging(t *testing.T) {
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

	// Client A connects and joins room
	clientA := wsConnect(t, server.URL+cfg.Server.WSPath+"?token="+signToken(t, "openrtc-clients", map[string]any{
		"tenant":   "tenant-a",
		"join":     []string{"tenant-a:*"},
		"publish":  []string{"tenant-a:*"},
		"presence": []string{"tenant-a:*"},
	}))
	defer clientA.Close()

	helloA := readJSON(t, clientA)
	t.Logf("Client A HELLO: %v", helloA)

	mustWriteJSON(t, clientA, map[string]any{"t": "JOIN", "id": "a-join", "room": "tenant-a:chat"})
	joinedA := readJSON(t, clientA)
	t.Logf("Client A JOINED: %v", joinedA)
	if joinedA["t"] != "JOINED" {
		t.Fatalf("Client A expected JOINED, got %v", joinedA["t"])
	}

	// Client B connects and joins same room
	clientB := wsConnect(t, server.URL+cfg.Server.WSPath+"?token="+signToken(t, "openrtc-clients", map[string]any{
		"tenant":   "tenant-a",
		"join":     []string{"tenant-a:*"},
		"publish":  []string{"tenant-a:*"},
		"presence": []string{"tenant-a:*"},
	}))
	defer clientB.Close()

	helloB := readJSON(t, clientB)
	t.Logf("Client B HELLO: %v", helloB)

	mustWriteJSON(t, clientB, map[string]any{"t": "JOIN", "id": "b-join", "room": "tenant-a:chat"})
	joinedB := readJSON(t, clientB)
	t.Logf("Client B JOINED: %v", joinedB)
	if joinedB["t"] != "JOINED" {
		t.Fatalf("Client B expected JOINED, got %v", joinedB["t"])
	}

	// Client A sends a message
	mustWriteJSON(t, clientA, map[string]any{
		"t":       "EMIT",
		"id":      "a-msg-1",
		"room":    "tenant-a:chat",
		"event":   "chat.message",
		"payload": map[string]any{"text": "Hello from A!"},
	})

	// Client B should receive Client A's message
	eventB := readJSON(t, clientB)
	t.Logf("Client B received: %v", eventB)
	if eventB["t"] != "EVENT" || eventB["event"] != "chat.message" {
		t.Fatalf("Client B expected EVENT chat.message, got %v %v", eventB["t"], eventB["event"])
	}
	payload := eventB["payload"].(map[string]any)
	if payload["text"] != "Hello from A!" {
		t.Fatalf("Client B expected 'Hello from A!', got %v", payload["text"])
	}

	// Client A also gets its own message back (sender receives too)
	eventA := readJSON(t, clientA)
	t.Logf("Client A received own event: %v", eventA)

	// Client B sends a reply
	mustWriteJSON(t, clientB, map[string]any{
		"t":       "EMIT",
		"id":      "b-msg-1",
		"room":    "tenant-a:chat",
		"event":   "chat.message",
		"payload": map[string]any{"text": "Hey A, this is B!"},
	})

	// Client A should receive B's message
	replyA := readJSON(t, clientA)
	t.Logf("Client A received reply: %v", replyA)
	if replyA["t"] != "EVENT" || replyA["event"] != "chat.message" {
		t.Fatalf("Client A expected EVENT chat.message, got %v %v", replyA["t"], replyA["event"])
	}
	replyPayload := replyA["payload"].(map[string]any)
	if replyPayload["text"] != "Hey A, this is B!" {
		t.Fatalf("Client A expected 'Hey A, this is B!', got %v", replyPayload["text"])
	}

	// Client B also gets its own reply back
	ownReplyB := readJSON(t, clientB)
	t.Logf("Client B received own reply: %v", ownReplyB)

	// Test presence
	mustWriteJSON(t, clientA, map[string]any{
		"t":       "PRESENCE_SET",
		"id":      "a-pres",
		"room":    "tenant-a:chat",
		"payload": map[string]any{"status": "typing"},
	})

	// Both clients should see presence update
	presA := readJSON(t, clientA)
	t.Logf("Client A presence: %v", presA)

	presB := readJSON(t, clientB)
	t.Logf("Client B presence: %v", presB)
	if presB["t"] != "PRESENCE" {
		t.Fatalf("Client B expected PRESENCE, got %v", presB["t"])
	}

	// Client B leaves
	mustWriteJSON(t, clientB, map[string]any{"t": "LEAVE", "id": "b-leave", "room": "tenant-a:chat"})
	leftB := readJSON(t, clientB)
	t.Logf("Client B LEFT: %v", leftB)
	if leftB["t"] != "LEFT" {
		t.Fatalf("Client B expected LEFT, got %v", leftB["t"])
	}

	t.Log("All two-client messaging tests passed!")
}

func TestClusterTwoNodeMessaging(t *testing.T) {
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
		"OPENRTC_AUTH_ISSUER":         "https://issuer.example.com",
		"OPENRTC_AUTH_AUDIENCE":       "openrtc-clients",
		"OPENRTC_AUTH_JWKS_URL":       jwks.URL,
		"OPENRTC_ADMIN_AUTH_ISSUER":   "https://issuer.example.com",
		"OPENRTC_ADMIN_AUTH_AUDIENCE": "openrtc-admin",
	}

	// Node 1
	cfg1 := copyMap(base)
	cfg1["OPENRTC_NODE_ID"] = "node-1"
	nodeCfg1, err := config.LoadFromMap(cfg1)
	if err != nil {
		t.Fatalf("node1 config: %v", err)
	}
	svc1, err := runtimeapp.NewService(nodeCfg1, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("node1 service: %v", err)
	}
	defer svc1.Close()
	server1 := httptest.NewServer(svc1.Handler())
	defer server1.Close()

	// Node 2
	cfg2 := copyMap(base)
	cfg2["OPENRTC_NODE_ID"] = "node-2"
	nodeCfg2, err := config.LoadFromMap(cfg2)
	if err != nil {
		t.Fatalf("node2 config: %v", err)
	}
	svc2, err := runtimeapp.NewService(nodeCfg2, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("node2 service: %v", err)
	}
	defer svc2.Close()
	server2 := httptest.NewServer(svc2.Handler())
	defer server2.Close()

	tokenClaims := map[string]any{
		"tenant":   "tenant-a",
		"join":     []string{"tenant-a:*"},
		"publish":  []string{"tenant-a:*"},
		"presence": []string{"tenant-a:*"},
	}

	// Client A on Node 1
	clientA := wsConnect(t, server1.URL+nodeCfg1.Server.WSPath+"?token="+signToken(t, "openrtc-clients", tokenClaims))
	defer clientA.Close()
	readJSON(t, clientA) // HELLO
	mustWriteJSON(t, clientA, map[string]any{"t": "JOIN", "id": "a-join", "room": "tenant-a:cross-node"})
	readJSON(t, clientA) // JOINED

	// Client B on Node 2
	clientB := wsConnect(t, server2.URL+nodeCfg2.Server.WSPath+"?token="+signToken(t, "openrtc-clients", tokenClaims))
	defer clientB.Close()
	readJSON(t, clientB) // HELLO
	mustWriteJSON(t, clientB, map[string]any{"t": "JOIN", "id": "b-join", "room": "tenant-a:cross-node"})
	readJSON(t, clientB) // JOINED

	// Small delay for Redis pub/sub subscription to settle
	time.Sleep(100 * time.Millisecond)

	// Client A emits from Node 1
	mustWriteJSON(t, clientA, map[string]any{
		"t":       "EMIT",
		"id":      "a-cross",
		"room":    "tenant-a:cross-node",
		"event":   "cross.node.test",
		"payload": map[string]any{"from": "node-1"},
	})

	// Client B on Node 2 should receive it via Redis pub/sub
	eventB := readJSON(t, clientB)
	t.Logf("Client B (node-2) received cross-node event: %v", eventB)
	if eventB["t"] != "EVENT" || eventB["event"] != "cross.node.test" {
		t.Fatalf("Expected cross-node EVENT, got %v", eventB)
	}

	t.Log("Cross-node cluster messaging test passed!")
}

func copyMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
