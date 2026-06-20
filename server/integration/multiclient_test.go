package integration

import (
	"io"
	"log"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gorilla/websocket"

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

func TestPresenceCursorFanoutAndOffline(t *testing.T) {
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

	token := signToken(t, "openrtc-clients", map[string]any{
		"tenant":   "tenant-a",
		"join":     []string{"tenant-a:*"},
		"publish":  []string{"tenant-a:*"},
		"presence": []string{"tenant-a:*"},
	})
	clients := []*websocket.Conn{
		wsConnect(t, server.URL+cfg.Server.WSPath+"?token="+token),
		wsConnect(t, server.URL+cfg.Server.WSPath+"?token="+token),
		wsConnect(t, server.URL+cfg.Server.WSPath+"?token="+token),
	}
	for _, client := range clients {
		defer client.Close()
	}

	connIDs := make([]string, 0, len(clients))
	for index, client := range clients {
		hello := readJSON(t, client)
		payload := hello["payload"].(map[string]any)
		connIDs = append(connIDs, payload["conn_id"].(string))
		mustWriteJSON(t, client, map[string]any{"t": "JOIN", "id": "join-cursor", "room": "tenant-a:cursor-room"})
		joined := readJSON(t, client)
		if joined["t"] != "JOINED" {
			t.Fatalf("client %d expected JOINED, got %#v", index, joined)
		}
	}

	cursorState := map[string]any{
		"user":   map[string]any{"id": "user-a", "name": "Ada", "color": "#4fd1b6"},
		"status": "editing",
		"color":  "#4fd1b6",
		"mode":   "comment",
		"cursor": map[string]any{"x": 144, "y": 288, "mode": "comment", "label": "Ada"},
	}
	mustWriteJSON(t, clients[0], map[string]any{
		"t":       "PRESENCE_SET",
		"id":      "cursor-presence",
		"room":    "tenant-a:cursor-room",
		"payload": cursorState,
	})

	for index, client := range clients {
		message := readJSON(t, client)
		if message["t"] != "PRESENCE" {
			t.Fatalf("client %d expected cursor PRESENCE, got %#v", index, message)
		}
		payload := message["payload"].(map[string]any)
		if payload["conn_id"] != connIDs[0] {
			t.Fatalf("client %d expected conn %s presence, got %#v", index, connIDs[0], payload)
		}
		state := payload["state"].(map[string]any)
		cursor := state["cursor"].(map[string]any)
		if state["mode"] != "comment" || cursor["mode"] != "comment" || cursor["x"] != float64(144) || cursor["y"] != float64(288) {
			t.Fatalf("client %d got malformed cursor state: %#v", index, state)
		}
		user := state["user"].(map[string]any)
		if user["name"] != "Ada" || state["color"] != "#4fd1b6" {
			t.Fatalf("client %d got malformed user state: %#v", index, state)
		}
	}

	mustWriteJSON(t, clients[1], map[string]any{"t": "LEAVE", "id": "leave-b", "room": "tenant-a:cursor-room"})
	left := readJSON(t, clients[1])
	if left["t"] != "LEFT" {
		t.Fatalf("leaving client expected LEFT, got %#v", left)
	}
	for _, client := range []*websocket.Conn{clients[0], clients[2]} {
		message := readJSON(t, client)
		payload := message["payload"].(map[string]any)
		if message["t"] != "PRESENCE" || payload["conn_id"] != connIDs[1] || payload["offline"] != true {
			t.Fatalf("expected offline presence for %s, got %#v", connIDs[1], message)
		}
	}
}

func TestPresenceBenchmarkFanoutCompleteness(t *testing.T) {
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

	token := signToken(t, "openrtc-clients", map[string]any{
		"tenant":   "tenant-a",
		"join":     []string{"tenant-a:*"},
		"publish":  []string{"tenant-a:*"},
		"presence": []string{"tenant-a:*"},
	})
	const clientCount = 4
	const rounds = 3
	clients := make([]*websocket.Conn, 0, clientCount)
	for index := 0; index < clientCount; index++ {
		client := wsConnect(t, server.URL+cfg.Server.WSPath+"?token="+token)
		defer client.Close()
		clients = append(clients, client)
	}

	connIDs := make([]string, 0, clientCount)
	for index, client := range clients {
		hello := readJSON(t, client)
		payload := hello["payload"].(map[string]any)
		connIDs = append(connIDs, payload["conn_id"].(string))
		mustWriteJSON(t, client, map[string]any{"t": "JOIN", "id": "bench-join", "room": "tenant-a:bench-room"})
		joined := readJSON(t, client)
		if joined["t"] != "JOINED" {
			t.Fatalf("client %d expected JOINED, got %#v", index, joined)
		}
	}

	runID := "bench-integration"
	expected := make(map[string]bool, clientCount*clientCount*rounds)
	for round := 1; round <= rounds; round++ {
		for senderIndex, client := range clients {
			sender := connIDs[senderIndex]
			expected[benchmarkDeliveryKey(round, sender)] = false
			mustWriteJSON(t, client, map[string]any{
				"t":    "PRESENCE_SET",
				"id":   "bench-presence",
				"room": "tenant-a:bench-room",
				"payload": map[string]any{
					"user": map[string]any{
						"id":    sender,
						"name":  "bench-" + sender,
						"color": "#4fd1b6",
					},
					"status": "benchmark",
					"cursor": map[string]any{
						"x":     20 + round + senderIndex,
						"y":     40 + round + senderIndex,
						"mode":  "pointer",
						"label": sender,
					},
					"benchmark": map[string]any{
						"run_id": runID,
						"round":  round,
						"sender": sender,
					},
				},
			})
		}
	}

	deliveries := make([]map[string]bool, clientCount)
	for index := range deliveries {
		deliveries[index] = make(map[string]bool, len(expected))
	}
	for receiverIndex, client := range clients {
		for len(deliveries[receiverIndex]) < len(expected) {
			message := readJSON(t, client)
			if message["t"] != "PRESENCE" {
				t.Fatalf("client %d expected PRESENCE during benchmark, got %#v", receiverIndex, message)
			}
			payload := message["payload"].(map[string]any)
			state := payload["state"].(map[string]any)
			benchmark := state["benchmark"].(map[string]any)
			if benchmark["run_id"] != runID {
				t.Fatalf("client %d got unrelated benchmark payload: %#v", receiverIndex, benchmark)
			}
			round := int(benchmark["round"].(float64))
			sender := benchmark["sender"].(string)
			key := benchmarkDeliveryKey(round, sender)
			if _, ok := expected[key]; !ok {
				t.Fatalf("client %d got unexpected benchmark delivery %s", receiverIndex, key)
			}
			if deliveries[receiverIndex][key] {
				t.Fatalf("client %d got duplicate benchmark delivery %s", receiverIndex, key)
			}
			deliveries[receiverIndex][key] = true
		}
	}

	for receiverIndex, seen := range deliveries {
		for key := range expected {
			if !seen[key] {
				t.Fatalf("client %d missed benchmark delivery %s", receiverIndex, key)
			}
		}
	}
}

func benchmarkDeliveryKey(round int, sender string) string {
	return strconv.Itoa(round) + "|" + sender
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
