package integration

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gorilla/websocket"

	"github.com/openrtc/openrtc/server/internal/cluster"
	"github.com/openrtc/openrtc/server/internal/config"
	runtimeapp "github.com/openrtc/openrtc/server/internal/runtime"
)

const (
	yjsTestUpdateFrame   byte = 1
	yjsTestSnapshotFrame byte = 2
)

func TestYJSPersistsUpdatesAndRejectsClientSnapshotsForReconnect(t *testing.T) {
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
		"tenant":  "tenant-a",
		"join":    []string{"tenant-a:*"},
		"publish": []string{"tenant-a:*"},
	})

	connA := yjsConnect(t, server.URL, "tenant-a:doc-1", token)
	mustWriteYJSFrame(t, connA, yjsTestUpdateFrame, []byte("incremental-update"))
	mustWriteYJSFrame(t, connA, yjsTestSnapshotFrame, []byte("full-snapshot"))
	expectYJSClose(t, connA)

	connB := yjsConnect(t, server.URL, "tenant-a:doc-1", token)
	defer connB.Close()
	kind, payload := readYJSFrame(t, connB)
	if kind != yjsTestUpdateFrame || string(payload) != "incremental-update" {
		t.Fatalf("expected persisted incremental update after snapshot, got kind=%d payload=%q", kind, string(payload))
	}
	expectNoYJSFrame(t, connB)
}

func TestYJSUpdatesFanOutAcrossCluster(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	cfg1, server1, sign1 := clusterConfig(t, redisServer.Addr(), "node-1")
	_, server2, sign2 := clusterConfig(t, redisServer.Addr(), "node-2")

	tokenClaims := map[string]any{
		"tenant":  "tenant-a",
		"join":    []string{"tenant-a:*"},
		"publish": []string{"tenant-a:*"},
	}

	clientA := yjsConnect(t, server1.URL, "tenant-a:shared-doc", sign1(t, "openrtc-clients", tokenClaims))
	defer clientA.Close()
	clientB := yjsConnect(t, server2.URL, "tenant-a:shared-doc", sign2(t, "openrtc-clients", tokenClaims))
	defer clientB.Close()

	time.Sleep(100 * time.Millisecond)

	mustWriteYJSFrame(t, clientA, yjsTestUpdateFrame, []byte("node-1-update"))
	kind, payload := readYJSFrame(t, clientB)
	if kind != yjsTestUpdateFrame || string(payload) != "node-1-update" {
		t.Fatalf("expected cross-node update, got kind=%d payload=%q", kind, string(payload))
	}

	clientC := yjsConnect(t, server2.URL, "tenant-a:shared-doc", sign2(t, "openrtc-clients", tokenClaims))
	defer clientC.Close()
	kind, payload = readYJSFrame(t, clientC)
	if kind != yjsTestUpdateFrame || string(payload) != "node-1-update" {
		t.Fatalf("expected persisted update replay, got kind=%d payload=%q", kind, string(payload))
	}

	_ = cfg1
}

func TestYJSUsesRoomAccessGrants(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	_, server1, sign1 := clusterConfig(t, redisServer.Addr(), "node-1")
	_, server2, sign2 := clusterConfig(t, redisServer.Addr(), "node-2")

	store, err := cluster.NewRedisStore("redis://"+redisServer.Addr(), "room:")
	if err != nil {
		t.Fatalf("seed redis store: %v", err)
	}
	defer store.Close()
	if _, err := store.CreateRoom(context.Background(), cluster.RoomRecord{
		ID:              "tenant-a:yjs-grants",
		DefaultAccesses: []string{cluster.PermissionRoomRead},
		GroupsAccesses: map[string][]string{
			"editors": {cluster.PermissionRoomWrite},
		},
	}); err != nil {
		t.Fatalf("create yjs room grant record: %v", err)
	}

	readToken := sign1(t, "openrtc-clients", map[string]any{"tenant": "tenant-a"})
	readConn := yjsConnect(t, server1.URL, "tenant-a:yjs-grants", readToken)
	readConn.Close()

	editorClaims := map[string]any{
		"tenant":   "tenant-a",
		"groupIds": []string{"editors"},
	}
	editorA := yjsConnect(t, server1.URL, "tenant-a:yjs-grants", sign1(t, "openrtc-clients", editorClaims))
	defer editorA.Close()
	editorB := yjsConnect(t, server2.URL, "tenant-a:yjs-grants", sign2(t, "openrtc-clients", editorClaims))
	defer editorB.Close()
	time.Sleep(100 * time.Millisecond)

	mustWriteYJSFrame(t, editorA, yjsTestUpdateFrame, []byte("grant-update"))
	kind, payload := readYJSFrame(t, editorB)
	if kind != yjsTestUpdateFrame || string(payload) != "grant-update" {
		t.Fatalf("expected grant-authorized yjs update, got kind=%d payload=%q", kind, string(payload))
	}

	crossTenantToken := sign1(t, "openrtc-clients", map[string]any{
		"tenant":   "tenant-b",
		"groupIds": []string{"editors"},
	})
	path := "/yjs/" + url.PathEscape("tenant-a:yjs-grants") + "?token=" + url.QueryEscape(crossTenantToken)
	_, resp, err := websocket.DefaultDialer.Dial("ws"+server1.URL[len("http"):]+path, nil)
	if err == nil {
		t.Fatal("expected cross-tenant yjs grant connection to fail")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got response=%v err=%v", resp, err)
	}
}

func TestYJSRejectsUnsafeRoomName(t *testing.T) {
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
		"tenant":  "tenant-a",
		"join":    []string{"tenant-a:*"},
		"publish": []string{"tenant-a:*"},
	})
	path := "/yjs/" + url.PathEscape("tenant-a:bad room") + "?token=" + url.QueryEscape(token)
	_, resp, err := websocket.DefaultDialer.Dial("ws"+server.URL[len("http"):]+path, nil)
	if err == nil {
		t.Fatal("expected unsafe room name to fail")
	}
	if resp == nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got response=%v err=%v", resp, err)
	}
}

func TestYJSRateLimitsFrames(t *testing.T) {
	jwks, signToken := newJWKS(t)
	defer jwks.Close()

	cfg, err := config.LoadFromMap(map[string]string{
		"OPENRTC_NODE_ID":                "node-a",
		"OPENRTC_AUTH_ISSUER":            "https://issuer.example.com",
		"OPENRTC_AUTH_AUDIENCE":          "openrtc-clients",
		"OPENRTC_AUTH_JWKS_URL":          jwks.URL,
		"OPENRTC_ADMIN_AUTH_ISSUER":      "https://issuer.example.com",
		"OPENRTC_ADMIN_AUTH_AUDIENCE":    "openrtc-admin",
		"OPENRTC_LIMIT_EMITS_PER_SECOND": "1",
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
		"tenant":  "tenant-a",
		"join":    []string{"tenant-a:*"},
		"publish": []string{"tenant-a:*"},
	})
	conn := yjsConnect(t, server.URL, "tenant-a:rate-limit", token)
	defer conn.Close()

	mustWriteYJSFrame(t, conn, yjsTestUpdateFrame, []byte("first"))
	mustWriteYJSFrame(t, conn, yjsTestUpdateFrame, []byte("second"))

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected yjs connection to close after rate limit")
	}
}

func TestYJSRejectsInvalidAndUnauthorizedFrames(t *testing.T) {
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

	joinOnlyToken := signToken(t, "openrtc-clients", map[string]any{
		"tenant": "tenant-a",
		"join":   []string{"tenant-a:*"},
	})
	joinOnlyConn := yjsConnect(t, server.URL, "tenant-a:read-only", joinOnlyToken)
	mustWriteYJSFrame(t, joinOnlyConn, yjsTestUpdateFrame, []byte("blocked"))
	expectYJSClose(t, joinOnlyConn)

	writeToken := signToken(t, "openrtc-clients", map[string]any{
		"tenant":  "tenant-a",
		"join":    []string{"tenant-a:*"},
		"publish": []string{"tenant-a:*"},
	})

	textConn := yjsConnect(t, server.URL, "tenant-a:bad-text", writeToken)
	if err := textConn.WriteMessage(websocket.TextMessage, []byte("not-binary")); err != nil {
		t.Fatalf("write text yjs frame: %v", err)
	}
	expectYJSClose(t, textConn)

	shortConn := yjsConnect(t, server.URL, "tenant-a:bad-short", writeToken)
	if err := shortConn.WriteMessage(websocket.BinaryMessage, []byte{yjsTestUpdateFrame}); err != nil {
		t.Fatalf("write short yjs frame: %v", err)
	}
	expectYJSClose(t, shortConn)

	invalidKindConn := yjsConnect(t, server.URL, "tenant-a:bad-kind", writeToken)
	if err := invalidKindConn.WriteMessage(websocket.BinaryMessage, []byte{99, 'x'}); err != nil {
		t.Fatalf("write invalid yjs frame: %v", err)
	}
	expectYJSClose(t, invalidKindConn)
}

func TestYJSClosesWhenRedisBecomesUnavailable(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}

	_, server, signToken := clusterConfig(t, redisServer.Addr(), "node-redis-failure")
	token := signToken(t, "openrtc-clients", map[string]any{
		"tenant":  "tenant-a",
		"join":    []string{"tenant-a:*"},
		"publish": []string{"tenant-a:*"},
	})

	conn := yjsConnect(t, server.URL, "tenant-a:redis-failure", token)
	redisServer.Close()
	mustWriteYJSFrame(t, conn, yjsTestUpdateFrame, []byte("fails-to-store"))
	expectYJSClose(t, conn)

	connAfterClose, resp, err := websocket.DefaultDialer.Dial("ws"+server.URL[len("http"):]+"/yjs/"+url.PathEscape("tenant-a:redis-load-failure")+"?token="+url.QueryEscape(token), nil)
	if err != nil {
		t.Fatalf("expected websocket upgrade before redis load failure, got response=%v err=%v", resp, err)
	}
	expectYJSClose(t, connAfterClose)
}

func yjsConnect(t *testing.T, serverURL string, room string, token string) *websocket.Conn {
	t.Helper()
	path := "/yjs/" + url.PathEscape(room)
	return wsConnect(t, serverURL+path+"?token="+url.QueryEscape(token))
}

func mustWriteYJSFrame(t *testing.T, conn *websocket.Conn, kind byte, payload []byte) {
	t.Helper()
	frame := make([]byte, 1+len(payload))
	frame[0] = kind
	copy(frame[1:], payload)
	if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		t.Fatalf("write yjs frame: %v", err)
	}
}

func readYJSFrame(t *testing.T, conn *websocket.Conn) (byte, []byte) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	messageType, frame, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read yjs frame: %v", err)
	}
	if messageType != websocket.BinaryMessage {
		t.Fatalf("expected binary yjs frame, got message type %d", messageType)
	}
	if len(frame) < 2 {
		t.Fatalf("expected non-empty yjs frame, got %d bytes", len(frame))
	}
	return frame[0], frame[1:]
}

func expectNoYJSFrame(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected no extra yjs frame")
	}
}

func expectYJSClose(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected yjs connection to close")
	}
}
