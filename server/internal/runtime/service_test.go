package runtimeapp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"

	"github.com/openrtc/openrtc/server/internal/auth"
	"github.com/openrtc/openrtc/server/internal/cluster"
	"github.com/openrtc/openrtc/server/internal/config"
	openrtcerr "github.com/openrtc/openrtc/server/internal/errors"
	"github.com/openrtc/openrtc/server/internal/protocol"
	"github.com/openrtc/openrtc/server/internal/roomengine"
	"github.com/openrtc/openrtc/server/internal/stats"
)

func TestNewServiceSingleModeAndReadiness(t *testing.T) {
	cfg := runtimeTestConfig()
	service, err := NewService(cfg, nil)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	defer service.Close()

	recorder := httptest.NewRecorder()
	service.handleReady(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "ready" {
		t.Fatalf("unexpected ready response: status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestNewServiceClusterModeWithRedis(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	cfg := runtimeTestConfig()
	cfg.Mode = config.ModeCluster
	cfg.Redis = &struct {
		URL           string
		ChannelPrefix string
	}{
		URL:           "redis://" + redisServer.Addr(),
		ChannelPrefix: "openrtc-test:",
	}
	service, err := NewService(cfg, nil)
	if err != nil {
		t.Fatalf("new cluster service: %v", err)
	}
	defer service.Close()
	if service.store == nil {
		t.Fatalf("expected redis-backed service")
	}

	recorder := httptest.NewRecorder()
	service.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected cluster service to be ready, got %d", recorder.Code)
	}
}

func TestNewServiceRejectsInvalidRedisURL(t *testing.T) {
	cfg := runtimeTestConfig()
	cfg.Mode = config.ModeCluster
	cfg.Redis = &struct {
		URL           string
		ChannelPrefix string
	}{
		URL:           "redis://%",
		ChannelPrefix: "openrtc-test:",
	}
	if service, err := NewService(cfg, nil); err == nil {
		_ = service.Close()
		t.Fatalf("expected invalid redis URL to fail")
	}
}

func TestNewServiceClosesStoreOnSubscriptionFailures(t *testing.T) {
	cfg := runtimeTestConfig()
	cfg.Mode = config.ModeCluster
	cfg.Redis = &struct {
		URL           string
		ChannelPrefix string
	}{
		URL:           "redis://example.invalid:6379",
		ChannelPrefix: "openrtc-test:",
	}
	expected := errors.New("subscribe failed")
	oldNewClusterStore := newClusterStore
	defer func() {
		newClusterStore = oldNewClusterStore
	}()

	tests := []struct {
		name  string
		store *fakeRuntimeStore
	}{
		{name: "events", store: &fakeRuntimeStore{subscribeErr: expected}},
		{name: "presence", store: &fakeRuntimeStore{subscribePresenceErr: expected}},
		{name: "yjs", store: &fakeRuntimeStore{subscribeYJSErr: expected}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			newClusterStore = func(string, string) (cluster.Store, error) {
				return tc.store, nil
			}
			service, err := NewService(cfg, nil)
			if err == nil {
				_ = service.Close()
				t.Fatalf("expected subscription failure")
			}
			if !errors.Is(err, expected) {
				t.Fatalf("expected subscription error, got %v", err)
			}
			if !tc.store.closed {
				t.Fatalf("expected store to be closed after subscription failure")
			}
		})
	}
}

func TestRuntimeReadinessWithStore(t *testing.T) {
	store := &fakeRuntimeStore{}
	service := &Service{ctx: context.Background(), store: store}

	recorder := httptest.NewRecorder()
	service.handleReady(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "ready" {
		t.Fatalf("unexpected ready response: status=%d body=%q", recorder.Code, recorder.Body.String())
	}

	store.healthyErr = errors.New("redis down")
	recorder = httptest.NewRecorder()
	service.handleReady(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected unhealthy store to return 503, got %d", recorder.Code)
	}
}

func TestRuntimeRequestHelpers(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/ws?token=query-token", nil)
	if token := tokenFromRequest(request); token != "query-token" {
		t.Fatalf("unexpected query token: %s", token)
	}

	request.Header.Set("Authorization", "Bearer header-token")
	if token := tokenFromRequest(request); token != "header-token" {
		t.Fatalf("expected bearer token to take precedence, got %s", token)
	}

	request.Header.Set("Authorization", "Bearer ")
	if token := tokenFromRequest(request); token != "query-token" {
		t.Fatalf("expected empty bearer to fall back to query token, got %s", token)
	}

	service := &Service{}
	if !service.checkOrigin(httptest.NewRequest(http.MethodGet, "/ws", nil)) {
		t.Fatalf("expected open origin policy to allow request")
	}
	service.cfg.Server.AllowedOrigins = []string{"https://app.example.com"}
	if service.checkOrigin(httptest.NewRequest(http.MethodGet, "/ws", nil)) {
		t.Fatalf("expected missing origin to be rejected")
	}
	request = httptest.NewRequest(http.MethodGet, "/ws", nil)
	request.Header.Set("Origin", "https://evil.example.com")
	if service.checkOrigin(request) {
		t.Fatalf("expected untrusted origin to be rejected")
	}
	request.Header.Set("Origin", "https://app.example.com")
	if !service.checkOrigin(request) {
		t.Fatalf("expected trusted origin to be allowed")
	}

	service.cfg.Tenant.EnforcePrefix = true
	service.cfg.Tenant.Separator = ":"
	if prefix := service.tenantPrefix(&auth.Claims{Tenant: "tenant-a"}); prefix != "tenant-a:" {
		t.Fatalf("unexpected tenant prefix: %s", prefix)
	}
	if prefix := service.tenantPrefix(&auth.Claims{}); prefix != "" {
		t.Fatalf("expected empty tenant prefix for missing tenant, got %s", prefix)
	}
	service.cfg.Tenant.EnforcePrefix = false
	if prefix := service.tenantPrefix(&auth.Claims{Tenant: "tenant-a"}); prefix != "" {
		t.Fatalf("expected empty tenant prefix when disabled, got %s", prefix)
	}
}

func TestRoomFromYJSPath(t *testing.T) {
	room, err := roomFromYJSPath("/yjs/" + url.PathEscape("tenant-a:doc-1"))
	if err != nil {
		t.Fatalf("valid yjs path rejected: %v", err)
	}
	if room != "tenant-a:doc-1" {
		t.Fatalf("unexpected yjs room: %s", room)
	}

	tests := []string{
		"/ws/tenant-a%3Adoc-1",
		"/yjs/",
		"/yjs/%zz",
		"/yjs/" + url.PathEscape("tenant-a:bad room"),
		"/yjs/" + url.PathEscape("tenant-a/doc-1"),
	}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			if _, err := roomFromYJSPath(path); err == nil {
				t.Fatalf("expected invalid yjs path to fail")
			}
		})
	}
}

func TestHandleWSAuthUpgradeAndHelloBranches(t *testing.T) {
	service := newRuntimeUnitService(t)
	defer service.Close()

	recorder := httptest.NewRecorder()
	service.handleWS(recorder, httptest.NewRequest(http.MethodGet, "/ws", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing token 401, got %d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	service.handleWS(recorder, httptest.NewRequest(http.MethodGet, "/ws?token=not-a-jwt", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected invalid token 401, got %d", recorder.Code)
	}

	authorized, token, cleanup := newRuntimeAuthorizedService(t, map[string]any{
		"tenant": "tenant-a",
		"join":   []string{"tenant-a:*"},
	})
	defer cleanup()
	recorder = httptest.NewRecorder()
	authorized.handleWS(recorder, httptest.NewRequest(http.MethodGet, "/ws?token="+url.QueryEscape(token), nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected non-websocket upgrade 400, got %d", recorder.Code)
	}

	server := httptest.NewServer(authorized.Handler())
	defer server.Close()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[len("http"):]+"/ws?token="+url.QueryEscape(token), nil)
	if err != nil {
		t.Fatalf("dial runtime websocket: %v", err)
	}
	var hello outboundMessage
	if err := ws.ReadJSON(&hello); err != nil {
		t.Fatalf("read hello: %v", err)
	}
	if hello.T != "HELLO" {
		t.Fatalf("unexpected hello message: %+v", hello)
	}
	if err := ws.WriteControl(websocket.PongMessage, []byte("pong"), time.Now().Add(time.Second)); err != nil {
		t.Fatalf("write runtime pong: %v", err)
	}
	_ = ws.Close()

	overflow, overflowToken, overflowCleanup := newRuntimeAuthorizedService(t, map[string]any{
		"tenant": "tenant-a",
		"join":   []string{"tenant-a:*"},
	})
	defer overflowCleanup()
	overflow.cfg.Limits.OutboundQueueDepth = 0
	overflowServer := httptest.NewServer(overflow.Handler())
	defer overflowServer.Close()
	overflowWS, _, err := websocket.DefaultDialer.Dial("ws"+overflowServer.URL[len("http"):]+"/ws?token="+url.QueryEscape(overflowToken), nil)
	if err != nil {
		t.Fatalf("dial overflow runtime websocket: %v", err)
	}
	expectWebSocketClose(t, overflowWS, "runtime hello enqueue overflow")

	errorService, errorToken, errorCleanup := newRuntimeAuthorizedService(t, map[string]any{
		"tenant":  "tenant-a",
		"join":    []string{"tenant-a:*"},
		"publish": []string{"tenant-a:*"},
		"scope":   "join:tenant-a:* publish:tenant-a:*",
	})
	defer errorCleanup()
	errorServer := httptest.NewServer(errorService.Handler())
	defer errorServer.Close()
	errorWS, _, err := websocket.DefaultDialer.Dial("ws"+errorServer.URL[len("http"):]+"/ws?token="+url.QueryEscape(errorToken), nil)
	if err != nil {
		t.Fatalf("dial runtime error websocket: %v", err)
	}
	var errorHello outboundMessage
	if err := errorWS.ReadJSON(&errorHello); err != nil {
		t.Fatalf("read error websocket hello: %v", err)
	}
	if err := errorWS.WriteJSON(map[string]any{"t": "JOIN", "id": "join-error", "room": "tenant-a:room-1"}); err != nil {
		t.Fatalf("write error websocket join: %v", err)
	}
	var joined outboundMessage
	if err := errorWS.ReadJSON(&joined); err != nil {
		t.Fatalf("read error websocket join: %v", err)
	}
	blocked := runtimeTestConn(errorService, "conn-blocked", &auth.Claims{Tenant: "tenant-a"}, 1)
	blocked.closed = true
	blocked.send <- outboundMessage{T: "FULL"}
	joinRuntimeRoom(t, errorService, blocked, "tenant-a:room-1")
	if err := errorWS.WriteJSON(map[string]any{"t": "EMIT", "id": "emit-error", "room": "tenant-a:room-1", "event": "doc.update", "payload": map[string]any{"ok": true}}); err != nil {
		t.Fatalf("write error websocket emit: %v", err)
	}
	expectWebSocketClose(t, errorWS, "runtime client message error")
}

func TestHandleYJSMissingToken(t *testing.T) {
	service := newRuntimeUnitService(t)
	defer service.Close()

	recorder := httptest.NewRecorder()
	service.handleYJS(recorder, httptest.NewRequest(http.MethodGet, "/yjs/tenant-a%3Adoc-1", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing yjs token 401, got %d", recorder.Code)
	}
}

func TestHandleYJSAuthUpgradeAndDocumentBranches(t *testing.T) {
	service := newRuntimeUnitService(t)
	defer service.Close()

	recorder := httptest.NewRecorder()
	service.handleYJS(recorder, httptest.NewRequest(http.MethodGet, "/yjs/", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid yjs path 400, got %d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	service.handleYJS(recorder, httptest.NewRequest(http.MethodGet, "/yjs/tenant-a%3Adoc-1?token=not-a-jwt", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected invalid yjs token 401, got %d", recorder.Code)
	}

	joinDenied, deniedToken, deniedCleanup := newRuntimeAuthorizedService(t, map[string]any{
		"tenant": "tenant-a",
	})
	defer deniedCleanup()
	recorder = httptest.NewRecorder()
	joinDenied.handleYJS(recorder, httptest.NewRequest(http.MethodGet, "/yjs/tenant-a%3Adoc-1?token="+url.QueryEscape(deniedToken), nil))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected yjs join forbidden 403, got %d", recorder.Code)
	}

	authorized, token, cleanup := newRuntimeAuthorizedService(t, map[string]any{
		"tenant":  "tenant-a",
		"join":    []string{"tenant-a:*"},
		"publish": []string{"tenant-a:*"},
		"scope":   "join:tenant-a:* publish:tenant-a:*",
	})
	defer cleanup()
	authorized.cfg.Limits.OutboundQueueDepth = 4
	recorder = httptest.NewRecorder()
	authorized.handleYJS(recorder, httptest.NewRequest(http.MethodGet, "/yjs/tenant-a%3Adoc-1?token="+url.QueryEscape(token), nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected non-websocket yjs upgrade 400, got %d", recorder.Code)
	}

	store := &fakeRuntimeStore{
		yjsDocument: cluster.YJSDocument{
			Snapshot: []byte("snapshot"),
			Updates:  [][]byte{[]byte("update-1")},
		},
	}
	authorized.store = store
	server := httptest.NewServer(authorized.Handler())
	defer server.Close()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[len("http"):]+"/yjs/tenant-a%3Adoc-1?token="+url.QueryEscape(token), nil)
	if err != nil {
		t.Fatalf("dial yjs websocket: %v", err)
	}
	_, snapshot, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("read yjs snapshot: %v", err)
	}
	if len(snapshot) == 0 || snapshot[0] != yjsFrameSnapshot || string(snapshot[1:]) != "snapshot" {
		t.Fatalf("unexpected yjs snapshot frame: %v", snapshot)
	}
	_, update, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("read yjs update: %v", err)
	}
	if len(update) == 0 || update[0] != yjsFrameUpdate || string(update[1:]) != "update-1" {
		t.Fatalf("unexpected yjs update frame: %v", update)
	}
	if err := ws.WriteControl(websocket.PongMessage, []byte("pong"), time.Now().Add(time.Second)); err != nil {
		t.Fatalf("write yjs pong: %v", err)
	}
	_ = ws.Close()
}

func TestHandleYJSFrameBranches(t *testing.T) {
	t.Run("initial send error", func(t *testing.T) {
		service, token, cleanup := newRuntimeAuthorizedService(t, map[string]any{
			"tenant": "tenant-a",
			"join":   []string{"tenant-a:*"},
			"scope":  "join:tenant-a:*",
		})
		defer cleanup()
		service.store = &fakeRuntimeStore{yjsDocument: cluster.YJSDocument{Snapshot: []byte("snapshot")}}
		oldSendInitialYJSDocument := sendInitialYJSDocument
		sendInitialYJSDocument = func(*yjsConn, cluster.YJSDocument) error {
			return errors.New("initial send failed")
		}
		defer func() {
			sendInitialYJSDocument = oldSendInitialYJSDocument
		}()
		ws, closeConn := dialRuntimeYJS(t, service, token)
		defer closeConn()
		expectWebSocketClose(t, ws, "yjs initial send error")
	})

	t.Run("load error", func(t *testing.T) {
		service, token, cleanup := newRuntimeAuthorizedService(t, map[string]any{
			"tenant":  "tenant-a",
			"join":    []string{"tenant-a:*"},
			"publish": []string{"tenant-a:*"},
			"scope":   "join:tenant-a:* publish:tenant-a:*",
		})
		defer cleanup()
		service.store = &fakeRuntimeStore{yjsErr: errors.New("load failed")}
		ws, closeConn := dialRuntimeYJS(t, service, token)
		defer closeConn()
		expectWebSocketClose(t, ws, "yjs load error")
	})

	t.Run("invalid frame shape", func(t *testing.T) {
		service, token, cleanup := newRuntimeAuthorizedService(t, map[string]any{
			"tenant":  "tenant-a",
			"join":    []string{"tenant-a:*"},
			"publish": []string{"tenant-a:*"},
			"scope":   "join:tenant-a:* publish:tenant-a:*",
		})
		defer cleanup()
		ws, closeConn := dialRuntimeYJS(t, service, token)
		defer closeConn()
		if err := ws.WriteMessage(websocket.TextMessage, []byte("bad")); err != nil {
			t.Fatalf("write invalid yjs frame: %v", err)
		}
		expectWebSocketClose(t, ws, "invalid yjs frame")
	})

	t.Run("publish forbidden", func(t *testing.T) {
		service, token, cleanup := newRuntimeAuthorizedService(t, map[string]any{
			"tenant": "tenant-a",
			"join":   []string{"tenant-a:*"},
		})
		defer cleanup()
		ws, closeConn := dialRuntimeYJS(t, service, token)
		defer closeConn()
		if err := ws.WriteMessage(websocket.BinaryMessage, append([]byte{yjsFrameUpdate}, []byte("update")...)); err != nil {
			t.Fatalf("write forbidden yjs frame: %v", err)
		}
		expectWebSocketClose(t, ws, "forbidden yjs publish")
	})

	t.Run("rate limited", func(t *testing.T) {
		service, token, cleanup := newRuntimeAuthorizedService(t, map[string]any{
			"tenant":  "tenant-a",
			"join":    []string{"tenant-a:*"},
			"publish": []string{"tenant-a:*"},
			"scope":   "join:tenant-a:* publish:tenant-a:*",
		})
		defer cleanup()
		service.cfg.Limits.EmitsPerSecond = 0
		ws, closeConn := dialRuntimeYJS(t, service, token)
		defer closeConn()
		if err := ws.WriteMessage(websocket.BinaryMessage, append([]byte{yjsFrameUpdate}, []byte("update")...)); err != nil {
			t.Fatalf("write rate limited yjs frame: %v", err)
		}
		expectWebSocketClose(t, ws, "rate limited yjs publish")
	})

	t.Run("invalid kind", func(t *testing.T) {
		service, token, cleanup := newRuntimeAuthorizedService(t, map[string]any{
			"tenant":  "tenant-a",
			"join":    []string{"tenant-a:*"},
			"publish": []string{"tenant-a:*"},
			"scope":   "join:tenant-a:* publish:tenant-a:*",
		})
		defer cleanup()
		ws, closeConn := dialRuntimeYJS(t, service, token)
		defer closeConn()
		if err := ws.WriteMessage(websocket.BinaryMessage, []byte{99, 'x'}); err != nil {
			t.Fatalf("write invalid kind yjs frame: %v", err)
		}
		expectWebSocketClose(t, ws, "invalid yjs kind")
	})

	t.Run("client snapshot rejected", func(t *testing.T) {
		service, token, cleanup := newRuntimeAuthorizedService(t, map[string]any{
			"tenant":  "tenant-a",
			"join":    []string{"tenant-a:*"},
			"publish": []string{"tenant-a:*"},
			"scope":   "join:tenant-a:* publish:tenant-a:*",
		})
		defer cleanup()
		ws, closeConn := dialRuntimeYJS(t, service, token)
		defer closeConn()
		if err := ws.WriteMessage(websocket.BinaryMessage, append([]byte{yjsFrameSnapshot}, []byte("snapshot")...)); err != nil {
			t.Fatalf("write client snapshot yjs frame: %v", err)
		}
		expectWebSocketClose(t, ws, "client yjs snapshot rejected")
	})

	t.Run("state vector request is transient read sync", func(t *testing.T) {
		service, token, cleanup := newRuntimeAuthorizedService(t, map[string]any{
			"tenant": "tenant-a",
			"join":   []string{"tenant-a:*"},
			"scope":  "join:tenant-a:*",
		})
		defer cleanup()
		ws, closeConn := dialRuntimeYJS(t, service, token)
		defer closeConn()
		receiver := &yjsConn{id: "state-vector-peer", room: "tenant-a:doc-1", send: make(chan []byte, 1), done: make(chan struct{})}
		registerRuntimeYJSConn(service, receiver)
		if err := ws.WriteMessage(websocket.BinaryMessage, append([]byte{yjsFrameStateVector}, []byte("state-vector")...)); err != nil {
			t.Fatalf("write yjs state vector frame: %v", err)
		}
		frame := receiveRuntimeTestValue(t, receiver.send, "relayed yjs state vector")
		if len(frame) == 0 || frame[0] != yjsFrameStateVector || string(frame[1:]) != "state-vector" {
			t.Fatalf("unexpected yjs state vector frame: %v", frame)
		}
	})

	t.Run("state vector request publishes across cluster without write permission", func(t *testing.T) {
		service, token, cleanup := newRuntimeAuthorizedService(t, map[string]any{
			"tenant": "tenant-a",
			"join":   []string{"tenant-a:*"},
			"scope":  "join:tenant-a:*",
		})
		defer cleanup()
		store := &fakeRuntimeStore{
			roomRecord:   runtimeWritableRoomRecord(),
			publishYJSCh: make(chan cluster.YJSEvent, 1),
		}
		service.store = store
		ws, closeConn := dialRuntimeYJS(t, service, token)
		defer closeConn()
		receiver := &yjsConn{id: "state-vector-cluster-peer", room: "tenant-a:doc-1", send: make(chan []byte, 1), done: make(chan struct{})}
		registerRuntimeYJSConn(service, receiver)
		if err := ws.WriteMessage(websocket.BinaryMessage, append([]byte{yjsFrameStateVector}, []byte("state-vector")...)); err != nil {
			t.Fatalf("write yjs state vector frame: %v", err)
		}
		frame := receiveRuntimeTestValue(t, receiver.send, "relayed yjs state vector")
		if len(frame) == 0 || frame[0] != yjsFrameStateVector || string(frame[1:]) != "state-vector" {
			t.Fatalf("unexpected yjs state vector frame: %v", frame)
		}
		published := receiveRuntimeTestValue(t, store.publishYJSCh, "published yjs state vector")
		if published.Kind != cluster.YJSEventStateVectorRequest || string(published.Update) != "state-vector" {
			t.Fatalf("unexpected published yjs state vector: %+v", published)
		}
	})

	t.Run("state vector diff requires publish", func(t *testing.T) {
		service, token, cleanup := newRuntimeAuthorizedService(t, map[string]any{
			"tenant": "tenant-a",
			"join":   []string{"tenant-a:*"},
			"scope":  "join:tenant-a:*",
		})
		defer cleanup()
		ws, closeConn := dialRuntimeYJS(t, service, token)
		defer closeConn()
		if err := ws.WriteMessage(websocket.BinaryMessage, append([]byte{yjsFrameStateVectorDiff}, []byte("diff")...)); err != nil {
			t.Fatalf("write forbidden yjs diff frame: %v", err)
		}
		expectWebSocketClose(t, ws, "forbidden yjs diff")
	})

	t.Run("state vector diff is relayed but not stored", func(t *testing.T) {
		service, token, cleanup := newRuntimeAuthorizedService(t, map[string]any{
			"tenant":  "tenant-a",
			"join":    []string{"tenant-a:*"},
			"publish": []string{"tenant-a:*"},
			"scope":   "join:tenant-a:* publish:tenant-a:*",
		})
		defer cleanup()
		store := &fakeRuntimeStore{
			roomRecord:   runtimeWritableRoomRecord(),
			appendCh:     make(chan []byte, 1),
			publishYJSCh: make(chan cluster.YJSEvent, 1),
		}
		service.store = store
		ws, closeConn := dialRuntimeYJS(t, service, token)
		defer closeConn()
		receiver := &yjsConn{id: "state-vector-diff-peer", room: "tenant-a:doc-1", send: make(chan []byte, 1), done: make(chan struct{})}
		registerRuntimeYJSConn(service, receiver)
		if err := ws.WriteMessage(websocket.BinaryMessage, append([]byte{yjsFrameStateVectorDiff}, []byte("diff")...)); err != nil {
			t.Fatalf("write yjs diff frame: %v", err)
		}
		frame := receiveRuntimeTestValue(t, receiver.send, "relayed yjs diff")
		if len(frame) == 0 || frame[0] != yjsFrameStateVectorDiff || string(frame[1:]) != "diff" {
			t.Fatalf("unexpected yjs diff frame: %v", frame)
		}
		published := receiveRuntimeTestValue(t, store.publishYJSCh, "published yjs diff")
		if published.Kind != cluster.YJSEventStateVectorDiff || string(published.Update) != "diff" {
			t.Fatalf("unexpected published yjs diff: %+v", published)
		}
		select {
		case update := <-store.appendCh:
			t.Fatalf("transient yjs diff should not be stored, got %q", string(update))
		default:
		}
	})

	t.Run("subdoc state vector request is transient read sync", func(t *testing.T) {
		service, token, cleanup := newRuntimeAuthorizedService(t, map[string]any{
			"tenant": "tenant-a",
			"join":   []string{"tenant-a:*"},
			"scope":  "join:tenant-a:*",
		})
		defer cleanup()
		ws, closeConn := dialRuntimeYJS(t, service, token)
		defer closeConn()
		receiver := &yjsConn{id: "subdoc-state-vector-peer", room: "tenant-a:doc-1", send: make(chan []byte, 1), done: make(chan struct{})}
		registerRuntimeYJSConn(service, receiver)
		if err := ws.WriteMessage(websocket.BinaryMessage, append([]byte{yjsFrameSubdocStateVector}, []byte("subdoc-state-vector")...)); err != nil {
			t.Fatalf("write yjs subdoc state vector frame: %v", err)
		}
		frame := receiveRuntimeTestValue(t, receiver.send, "relayed yjs subdoc state vector")
		if len(frame) == 0 || frame[0] != yjsFrameSubdocStateVector || string(frame[1:]) != "subdoc-state-vector" {
			t.Fatalf("unexpected yjs subdoc state vector frame: %v", frame)
		}
	})

	t.Run("subdoc state vector diff is relayed but not stored", func(t *testing.T) {
		service, token, cleanup := newRuntimeAuthorizedService(t, map[string]any{
			"tenant":  "tenant-a",
			"join":    []string{"tenant-a:*"},
			"publish": []string{"tenant-a:*"},
			"scope":   "join:tenant-a:* publish:tenant-a:*",
		})
		defer cleanup()
		store := &fakeRuntimeStore{
			roomRecord:   runtimeWritableRoomRecord(),
			appendCh:     make(chan []byte, 1),
			publishYJSCh: make(chan cluster.YJSEvent, 1),
		}
		service.store = store
		ws, closeConn := dialRuntimeYJS(t, service, token)
		defer closeConn()
		receiver := &yjsConn{id: "subdoc-state-vector-diff-peer", room: "tenant-a:doc-1", send: make(chan []byte, 1), done: make(chan struct{})}
		registerRuntimeYJSConn(service, receiver)
		if err := ws.WriteMessage(websocket.BinaryMessage, append([]byte{yjsFrameSubdocDiff}, []byte("subdoc-diff")...)); err != nil {
			t.Fatalf("write yjs subdoc diff frame: %v", err)
		}
		frame := receiveRuntimeTestValue(t, receiver.send, "relayed yjs subdoc diff")
		if len(frame) == 0 || frame[0] != yjsFrameSubdocDiff || string(frame[1:]) != "subdoc-diff" {
			t.Fatalf("unexpected yjs subdoc diff frame: %v", frame)
		}
		published := receiveRuntimeTestValue(t, store.publishYJSCh, "published yjs subdoc diff")
		if published.Kind != cluster.YJSEventSubdocDiff || string(published.Update) != "subdoc-diff" {
			t.Fatalf("unexpected published yjs subdoc diff: %+v", published)
		}
		select {
		case update := <-store.appendCh:
			t.Fatalf("transient yjs subdoc diff should not be stored, got %q", string(update))
		default:
		}
	})

	t.Run("subdoc update is durable", func(t *testing.T) {
		service, token, cleanup := newRuntimeAuthorizedService(t, map[string]any{
			"tenant":  "tenant-a",
			"join":    []string{"tenant-a:*"},
			"publish": []string{"tenant-a:*"},
			"scope":   "join:tenant-a:* publish:tenant-a:*",
		})
		defer cleanup()
		store := &fakeRuntimeStore{
			roomRecord:   runtimeWritableRoomRecord(),
			appendCh:     make(chan []byte, 1),
			publishYJSCh: make(chan cluster.YJSEvent, 1),
		}
		service.store = store
		ws, closeConn := dialRuntimeYJS(t, service, token)
		defer closeConn()
		receiver := &yjsConn{id: "subdoc-update-peer", room: "tenant-a:doc-1", send: make(chan []byte, 1), done: make(chan struct{})}
		registerRuntimeYJSConn(service, receiver)
		if err := ws.WriteMessage(websocket.BinaryMessage, append([]byte{yjsFrameSubdocUpdate}, []byte("subdoc-update")...)); err != nil {
			t.Fatalf("write yjs subdoc update frame: %v", err)
		}
		if got := receiveRuntimeTestValue(t, store.appendCh, "stored yjs subdoc update"); string(got) != "subdoc-update" {
			t.Fatalf("unexpected stored yjs subdoc update: %q", string(got))
		}
		if store.appendedKind != cluster.YJSEventSubdocUpdate {
			t.Fatalf("expected subdoc update kind to be stored, got %d", store.appendedKind)
		}
		frame := receiveRuntimeTestValue(t, receiver.send, "relayed yjs subdoc update")
		if len(frame) == 0 || frame[0] != yjsFrameSubdocUpdate || string(frame[1:]) != "subdoc-update" {
			t.Fatalf("unexpected yjs subdoc update frame: %v", frame)
		}
		published := receiveRuntimeTestValue(t, store.publishYJSCh, "published yjs subdoc update")
		if published.Kind != cluster.YJSEventSubdocUpdate || string(published.Update) != "subdoc-update" {
			t.Fatalf("unexpected published yjs subdoc update: %+v", published)
		}
	})

	t.Run("store append error", func(t *testing.T) {
		service, token, cleanup := newRuntimeAuthorizedService(t, map[string]any{
			"tenant":  "tenant-a",
			"join":    []string{"tenant-a:*"},
			"publish": []string{"tenant-a:*"},
			"scope":   "join:tenant-a:* publish:tenant-a:*",
		})
		defer cleanup()
		service.store = &fakeRuntimeStore{roomRecord: runtimeWritableRoomRecord(), appendErr: errors.New("append failed")}
		ws, closeConn := dialRuntimeYJS(t, service, token)
		defer closeConn()
		if err := ws.WriteMessage(websocket.BinaryMessage, append([]byte{yjsFrameUpdate}, []byte("update")...)); err != nil {
			t.Fatalf("write append error yjs frame: %v", err)
		}
		expectWebSocketClose(t, ws, "yjs append error")
	})

	t.Run("broadcast overflow", func(t *testing.T) {
		service, token, cleanup := newRuntimeAuthorizedService(t, map[string]any{
			"tenant":  "tenant-a",
			"join":    []string{"tenant-a:*"},
			"publish": []string{"tenant-a:*"},
			"scope":   "join:tenant-a:* publish:tenant-a:*",
		})
		defer cleanup()
		service.store = &fakeRuntimeStore{roomRecord: runtimeWritableRoomRecord()}
		ws, closeConn := dialRuntimeYJS(t, service, token)
		defer closeConn()
		blocked := &yjsConn{id: "blocked-yjs", room: "tenant-a:doc-1", send: make(chan []byte, 1), done: make(chan struct{}), closed: true}
		blocked.send <- []byte("full")
		registerRuntimeYJSConn(service, blocked)
		if err := ws.WriteMessage(websocket.BinaryMessage, append([]byte{yjsFrameUpdate}, []byte("update")...)); err != nil {
			t.Fatalf("write overflow yjs frame: %v", err)
		}
		expectWebSocketClose(t, ws, "yjs broadcast overflow")
	})

	t.Run("publish error", func(t *testing.T) {
		service, token, cleanup := newRuntimeAuthorizedService(t, map[string]any{
			"tenant":  "tenant-a",
			"join":    []string{"tenant-a:*"},
			"publish": []string{"tenant-a:*"},
			"scope":   "join:tenant-a:* publish:tenant-a:*",
		})
		defer cleanup()
		service.store = &fakeRuntimeStore{roomRecord: runtimeWritableRoomRecord(), publishYJSErr: errors.New("publish failed")}
		ws, closeConn := dialRuntimeYJS(t, service, token)
		defer closeConn()
		if err := ws.WriteMessage(websocket.BinaryMessage, append([]byte{yjsFrameUpdate}, []byte("update")...)); err != nil {
			t.Fatalf("write publish error yjs frame: %v", err)
		}
		expectWebSocketClose(t, ws, "yjs publish error")
	})

	t.Run("successful update", func(t *testing.T) {
		service, token, cleanup := newRuntimeAuthorizedService(t, map[string]any{
			"tenant":  "tenant-a",
			"join":    []string{"tenant-a:*"},
			"publish": []string{"tenant-a:*"},
			"scope":   "join:tenant-a:* publish:tenant-a:*",
		})
		defer cleanup()
		store := &fakeRuntimeStore{roomRecord: runtimeWritableRoomRecord(), appendCh: make(chan []byte, 1)}
		service.store = store
		ws, closeConn := dialRuntimeYJS(t, service, token)
		defer closeConn()
		if err := ws.WriteMessage(websocket.BinaryMessage, append([]byte{yjsFrameUpdate}, []byte("client-update")...)); err != nil {
			t.Fatalf("write successful yjs frame: %v", err)
		}
		if got := receiveRuntimeTestValue(t, store.appendCh, "stored yjs update"); string(got) != "client-update" {
			t.Fatalf("unexpected stored yjs update: %q", string(got))
		}
		if store.appendedKind != cluster.YJSEventUpdate {
			t.Fatalf("expected root update kind to be stored, got %d", store.appendedKind)
		}
	})
}

func TestConnectionQueueHelpers(t *testing.T) {
	service := &Service{}
	client := &clientConn{
		service: service,
		send:    make(chan outboundMessage, 1),
		done:    make(chan struct{}),
	}
	if err := client.enqueue(outboundMessage{T: "HELLO"}); err != nil {
		t.Fatalf("enqueue client message: %v", err)
	}
	if got := <-client.send; got.T != "HELLO" {
		t.Fatalf("unexpected client message: %+v", got)
	}
	closedClient := &clientConn{done: make(chan struct{})}
	close(closedClient.done)
	if err := closedClient.enqueue(outboundMessage{T: "AFTER_CLOSE"}); err == nil {
		t.Fatalf("expected closed client enqueue to fail")
	}

	overflowService, err := NewService(runtimeTestConfig(), nil)
	if err != nil {
		t.Fatalf("new overflow service: %v", err)
	}
	defer overflowService.Close()
	overflowClient := &clientConn{
		service: overflowService,
		send:    make(chan outboundMessage, 1),
		done:    make(chan struct{}),
		closed:  true,
	}
	overflowClient.send <- outboundMessage{T: "FULL"}
	if err := overflowClient.enqueue(outboundMessage{T: "OVERFLOW"}); err == nil {
		t.Fatalf("expected full client queue to overflow")
	}
	if overflowService.stats.QueueOverflowsTotal != 1 || overflowService.metrics.QueueOverflowsTotal.Load() != 1 {
		t.Fatalf("expected overflow stats and metrics to increment")
	}

	yjs := &yjsConn{
		send: make(chan []byte, 1),
		done: make(chan struct{}),
	}
	if err := yjs.enqueueFrame(yjsFrameUpdate, []byte("update")); err != nil {
		t.Fatalf("enqueue yjs frame: %v", err)
	}
	if got := <-yjs.send; string(got) != string(append([]byte{yjsFrameUpdate}, []byte("update")...)) {
		t.Fatalf("unexpected yjs frame: %v", got)
	}
	closedYJS := &yjsConn{done: make(chan struct{})}
	close(closedYJS.done)
	if err := closedYJS.enqueueFrame(yjsFrameSnapshot, []byte("snapshot")); err == nil {
		t.Fatalf("expected closed yjs enqueue to fail")
	}
	overflowYJS := &yjsConn{
		send:   make(chan []byte, 1),
		done:   make(chan struct{}),
		closed: true,
	}
	overflowYJS.send <- []byte("full")
	if err := overflowYJS.enqueueFrame(yjsFrameUpdate, []byte("overflow")); err == nil {
		t.Fatalf("expected full yjs queue to overflow")
	}
}

func TestRuntimeAllowsRoomActionWithRoomGrants(t *testing.T) {
	cfg := runtimeTestConfig()
	service := &Service{
		cfg: cfg,
		store: &fakeRuntimeStore{roomRecord: cluster.RoomRecord{
			ID: "tenant-a:doc-1",
			UsersAccesses: map[string][]string{
				"user-1":               {cluster.PermissionRoomRead},
				"storage-reader":       {cluster.PermissionStorageRead},
				"storage-writer":       {cluster.PermissionStorageWrite},
				"normalized-room-user": {cluster.PermissionRoomWrite},
			},
			GroupsAccesses: map[string][]string{
				"team-1": {cluster.PermissionRoomPresenceWrite},
			},
		}},
	}

	scopedClaims := &auth.Claims{
		Tenant: "tenant-a",
		Join:   []string{"tenant-a:*"},
	}
	if !service.allowsRoomAction(context.Background(), scopedClaims, "join", "tenant-a:doc-1") {
		t.Fatalf("expected direct token scope to allow join")
	}

	userClaims := &auth.Claims{Tenant: "tenant-a"}
	userClaims.Subject = "user-1"
	if !service.allowsRoomAction(context.Background(), userClaims, "join", "tenant-a:doc-1") {
		t.Fatalf("expected room user grant to allow join")
	}
	if service.allowsRoomAction(context.Background(), userClaims, "publish", "tenant-a:doc-1") {
		t.Fatalf("expected room read grant to deny publish")
	}

	storageReaderClaims := &auth.Claims{Tenant: "tenant-a"}
	storageReaderClaims.Subject = "storage-reader"
	if !service.allowsRoomAction(context.Background(), storageReaderClaims, "storage:read", "tenant-a:doc-1") {
		t.Fatalf("expected storage read grant to allow storage reads")
	}
	if service.allowsRoomAction(context.Background(), storageReaderClaims, "storage:write", "tenant-a:doc-1") {
		t.Fatalf("expected storage read grant to deny storage writes")
	}

	storageWriterClaims := &auth.Claims{Tenant: "tenant-a"}
	storageWriterClaims.Subject = "storage-writer"
	if !service.allowsRoomAction(context.Background(), storageWriterClaims, "storage:read", "tenant-a:doc-1") {
		t.Fatalf("expected storage write grant to allow storage reads")
	}
	if !service.allowsRoomAction(context.Background(), storageWriterClaims, "storage:write", "tenant-a:doc-1") {
		t.Fatalf("expected storage write grant to allow storage writes")
	}

	normalizedRoomClaims := &auth.Claims{Tenant: "tenant-a"}
	normalizedRoomClaims.Subject = "normalized-room-user"
	if !service.allowsRoomAction(context.Background(), normalizedRoomClaims, "storage:write", "tenant-a:doc-1") {
		t.Fatalf("expected room write grant to preserve storage write access")
	}

	groupClaims := &auth.Claims{
		Tenant:   "tenant-a",
		GroupIDs: []string{"team-1"},
	}
	if !service.allowsRoomAction(context.Background(), groupClaims, "presence", "tenant-a:doc-1") {
		t.Fatalf("expected room group grant to allow presence")
	}

	if service.allowsRoomAction(context.Background(), &auth.Claims{Tenant: "tenant-b"}, "join", "tenant-a:doc-1") {
		t.Fatalf("expected cross-tenant room action to be rejected")
	}

	service.store = &fakeRuntimeStore{roomErr: errors.New("lookup failed")}
	if service.allowsRoomAction(context.Background(), userClaims, "join", "tenant-a:doc-1") {
		t.Fatalf("expected room lookup failure to reject action")
	}
}

func TestRuntimeHandleJoinLocalBranches(t *testing.T) {
	cfg := runtimeTestConfig()
	cfg.Limits.RoomsPerConnection = 1
	service, err := NewService(cfg, nil)
	if err != nil {
		t.Fatalf("new runtime service: %v", err)
	}
	defer service.Close()

	claims := &auth.Claims{Tenant: "tenant-a", Join: []string{"tenant-a:*"}}
	conn := runtimeTestConn(service, "conn-1", claims, 4)
	if err := service.handleJoin(conn, protocol.Message{
		ID:       "join-1",
		Room:     "tenant-a:room-1",
		JoinMeta: &protocol.JoinMeta{Limit: 1},
	}); err != nil {
		t.Fatalf("join room: %v", err)
	}
	if got := readRuntimeOutbound(t, conn); got.T != "JOINED" || got.Room != "tenant-a:room-1" {
		t.Fatalf("unexpected joined response: %+v", got)
	} else {
		payload, ok := got.Payload.(roomengine.SnapshotPage)
		if !ok {
			t.Fatalf("unexpected joined payload type: %#v", got.Payload)
		}
		if !reflect.DeepEqual(payload.Members, []string{"conn-1"}) {
			t.Fatalf("unexpected joined members: %#v", payload.Members)
		}
	}

	if err := service.handleJoin(conn, protocol.Message{ID: "join-dup", Room: "tenant-a:room-1"}); err != nil {
		t.Fatalf("duplicate join: %v", err)
	}
	if got := readRuntimeOutbound(t, conn); got.T != "JOINED" || got.ID != "join-dup" {
		t.Fatalf("unexpected duplicate join response: %+v", got)
	}

	if err := service.handleJoin(conn, protocol.Message{ID: "join-cap", Room: "tenant-a:room-2"}); err != nil {
		t.Fatalf("room cap join should enqueue an error, got %v", err)
	}
	if got := readRuntimeOutbound(t, conn); got.T != "ERROR" || got.ID != "join-cap" {
		t.Fatalf("unexpected room cap response: %+v", got)
	}

	denied := runtimeTestConn(service, "conn-denied", &auth.Claims{Tenant: "tenant-a"}, 1)
	if err := service.handleJoin(denied, protocol.Message{ID: "join-denied", Room: "tenant-a:private"}); err != nil {
		t.Fatalf("denied join should enqueue an error, got %v", err)
	}
	if got := readRuntimeOutbound(t, denied); got.T != "ERROR" || got.ID != "join-denied" {
		t.Fatalf("unexpected denied join response: %+v", got)
	}
}

func TestRuntimeAppliesRoomMembershipMutationIntents(t *testing.T) {
	service := newRuntimeUnitService(t)
	defer service.Close()

	store := &fakeRuntimeStore{
		snapshot: cluster.Snapshot{
			Members:  []string{"conn-1"},
			Presence: map[string]json.RawMessage{},
		},
	}
	service.store = store
	conn := runtimeTestConn(service, "conn-1", &auth.Claims{
		Tenant: "tenant-a",
		Join:   []string{"tenant-a:*"},
	}, 4)

	if err := service.handleJoin(conn, protocol.Message{ID: "join-1", Room: "tenant-a:room-1"}); err != nil {
		t.Fatalf("join room: %v", err)
	}
	if got := readRuntimeOutbound(t, conn); got.T != "JOINED" || got.ID != "join-1" {
		t.Fatalf("unexpected join response: %+v", got)
	}
	if !reflect.DeepEqual(store.joinedRooms, []roomengine.MembershipMutation{{Kind: roomengine.MembershipMutationJoin, ConnID: "conn-1", Room: "tenant-a:room-1"}}) {
		t.Fatalf("unexpected join membership writes: %#v", store.joinedRooms)
	}

	if err := service.handleJoin(conn, protocol.Message{ID: "join-dup", Room: "tenant-a:room-1"}); err != nil {
		t.Fatalf("duplicate join: %v", err)
	}
	if got := readRuntimeOutbound(t, conn); got.T != "JOINED" || got.ID != "join-dup" {
		t.Fatalf("unexpected duplicate join response: %+v", got)
	}
	if len(store.joinedRooms) != 1 {
		t.Fatalf("duplicate join should not write membership again: %#v", store.joinedRooms)
	}

	if err := service.handleLeave(conn, protocol.Message{ID: "leave-1", Room: "tenant-a:room-1"}); err != nil {
		t.Fatalf("leave room: %v", err)
	}
	if got := readRuntimeOutbound(t, conn); got.T != "LEFT" || got.ID != "leave-1" {
		t.Fatalf("unexpected leave response: %+v", got)
	}
	if !reflect.DeepEqual(store.leftRooms, []roomengine.MembershipMutation{{Kind: roomengine.MembershipMutationLeave, ConnID: "conn-1", Room: "tenant-a:room-1"}}) {
		t.Fatalf("unexpected leave membership writes: %#v", store.leftRooms)
	}

	if err := service.handleLeave(conn, protocol.Message{ID: "leave-dup", Room: "tenant-a:room-1"}); err != nil {
		t.Fatalf("duplicate leave: %v", err)
	}
	if got := readRuntimeOutbound(t, conn); got.T != "LEFT" || got.ID != "leave-dup" {
		t.Fatalf("unexpected duplicate leave response: %+v", got)
	}
	if len(store.leftRooms) != 1 {
		t.Fatalf("duplicate leave should not write membership again: %#v", store.leftRooms)
	}
}

func TestRuntimeJoinReplaysPublishedEventsAfterSequence(t *testing.T) {
	service := newRuntimeUnitService(t)
	defer service.Close()

	store := &fakeRuntimeStore{
		publishedEvents: []cluster.PublishedEvent{
			{Room: "tenant-a:room-1", Event: "before", Payload: json.RawMessage(`{"n":1}`), Sequence: 1, OriginNode: "node-b"},
			{Room: "tenant-a:room-1", Event: "after", Payload: json.RawMessage(`{"n":2}`), Sequence: 2, OriginNode: "node-b", TraceID: "trace-after"},
			{Room: "tenant-a:room-1", Event: storageClusterEvent, Payload: json.RawMessage(`{"kind":"set"}`), Sequence: 3, OriginNode: "node-b"},
			{Room: "tenant-a:room-2", Event: "other-room", Payload: json.RawMessage(`{"n":4}`), Sequence: 4, OriginNode: "node-b"},
		},
	}
	service.store = store
	conn := runtimeTestConn(service, "conn-catchup", &auth.Claims{
		Tenant: "tenant-a",
		Join:   []string{"tenant-a:*"},
	}, 4)

	if err := service.handleJoin(conn, protocol.Message{
		ID:       "join-catchup",
		Room:     "tenant-a:room-1",
		JoinMeta: &protocol.JoinMeta{AfterSequence: 1},
	}); err != nil {
		t.Fatalf("join catchup: %v", err)
	}
	if got := readRuntimeOutbound(t, conn); got.T != "JOINED" || got.ID != "join-catchup" {
		t.Fatalf("expected joined before catch-up events, got %+v", got)
	}
	got := readRuntimeOutbound(t, conn)
	if got.T != "EVENT" || got.Event != "after" || got.Room != "tenant-a:room-1" {
		t.Fatalf("expected replayed room event, got %+v", got)
	}
	meta, ok := got.Meta.(map[string]any)
	if !ok || meta["seq"] != uint64(2) || meta["trace_id"] != "trace-after" {
		t.Fatalf("unexpected replay event meta: %#v", got.Meta)
	}
	select {
	case got := <-conn.send:
		t.Fatalf("expected storage/internal events to be skipped, got %+v", got)
	default:
	}
}

func TestRuntimeStoreErrorBranches(t *testing.T) {
	expected := errors.New("store failed")
	claims := &auth.Claims{
		Tenant:   "tenant-a",
		Join:     []string{"tenant-a:*"},
		Publish:  []string{"tenant-a:*"},
		Presence: []string{"tenant-a:*"},
	}

	t.Run("join room write", func(t *testing.T) {
		service := newRuntimeUnitService(t)
		defer service.Close()
		store := &fakeRuntimeStore{joinErr: expected}
		service.store = store
		conn := runtimeTestConn(service, "conn-join", claims, 2)
		if err := service.handleJoin(conn, protocol.Message{ID: "join", Room: "tenant-a:room-1"}); !errors.Is(err, expected) {
			t.Fatalf("expected join store error, got %v", err)
		}
	})

	t.Run("join snapshot", func(t *testing.T) {
		service := newRuntimeUnitService(t)
		defer service.Close()
		store := &fakeRuntimeStore{snapshotErr: expected}
		service.store = store
		conn := runtimeTestConn(service, "conn-snapshot", claims, 2)
		if err := service.handleJoin(conn, protocol.Message{ID: "join", Room: "tenant-a:room-1"}); !errors.Is(err, expected) {
			t.Fatalf("expected snapshot error, got %v", err)
		}
	})

	t.Run("duplicate join snapshot", func(t *testing.T) {
		service := newRuntimeUnitService(t)
		defer service.Close()
		store := &fakeRuntimeStore{snapshotErr: expected}
		service.store = store
		conn := runtimeTestConn(service, "conn-duplicate-snapshot", claims, 2)
		joinRuntimeRoom(t, service, conn, "tenant-a:room-1")
		if err := service.handleJoin(conn, protocol.Message{ID: "join", Room: "tenant-a:room-1"}); !errors.Is(err, expected) {
			t.Fatalf("expected duplicate join snapshot error, got %v", err)
		}
	})

	t.Run("join event catchup", func(t *testing.T) {
		service := newRuntimeUnitService(t)
		defer service.Close()
		store := &fakeRuntimeStore{listPublishedEventsErr: expected}
		service.store = store
		conn := runtimeTestConn(service, "conn-join-catchup", claims, 2)
		if err := service.handleJoin(conn, protocol.Message{
			ID:       "join",
			Room:     "tenant-a:room-1",
			JoinMeta: &protocol.JoinMeta{AfterSequence: 1},
		}); !errors.Is(err, expected) {
			t.Fatalf("expected join catchup error, got %v", err)
		}
		select {
		case got := <-conn.send:
			t.Fatalf("expected no partial join response after catchup failure, got %+v", got)
		default:
		}
	})

	t.Run("leave room write", func(t *testing.T) {
		service := newRuntimeUnitService(t)
		defer service.Close()
		store := &fakeRuntimeStore{leaveErr: expected}
		service.store = store
		conn := runtimeTestConn(service, "conn-leave", claims, 2)
		joinRuntimeRoom(t, service, conn, "tenant-a:room-1")
		if err := service.handleLeave(conn, protocol.Message{ID: "leave", Room: "tenant-a:room-1"}); !errors.Is(err, expected) {
			t.Fatalf("expected leave store error, got %v", err)
		}
		if got := service.roomEngine().MemberIDs("tenant-a:room-1", ""); !reflect.DeepEqual(got, []string{conn.id}) {
			t.Fatalf("leave store error should not apply local membership, got %#v", got)
		}
	})

	t.Run("presence write", func(t *testing.T) {
		service := newRuntimeUnitService(t)
		defer service.Close()
		store := &fakeRuntimeStore{setPresenceErr: expected}
		service.store = store
		conn := runtimeTestConn(service, "conn-presence", claims, 2)
		if err := service.handlePresence(conn, protocol.Message{
			ID:      "presence",
			Room:    "tenant-a:room-1",
			Payload: json.RawMessage(`{"cursor":{"x":1}}`),
		}); !errors.Is(err, expected) {
			t.Fatalf("expected presence store error, got %v", err)
		}
	})

	t.Run("event publish", func(t *testing.T) {
		service := newRuntimeUnitService(t)
		defer service.Close()
		store := &fakeRuntimeStore{publishEventErr: expected}
		service.store = store
		conn := runtimeTestConn(service, "conn-emit", claims, 2)
		if err := service.handleEmit(conn, protocol.Message{
			ID:      "emit",
			Room:    "tenant-a:room-1",
			Event:   "doc.update",
			Payload: json.RawMessage(`{"ok":true}`),
		}); !errors.Is(err, expected) {
			t.Fatalf("expected publish store error, got %v", err)
		}
	})

	t.Run("leave presence publish", func(t *testing.T) {
		service := newRuntimeUnitService(t)
		defer service.Close()
		store := &fakeRuntimeStore{publishPresenceErr: expected}
		service.store = store
		conn := runtimeTestConn(service, "conn-leave-publish", claims, 2)
		receiver := runtimeTestConn(service, "conn-leave-publish-receiver", claims, 2)
		joinRuntimeRoom(t, service, conn, "tenant-a:room-1")
		joinRuntimeRoom(t, service, receiver, "tenant-a:room-1")
		setRuntimePresence(service, conn, "tenant-a:room-1", json.RawMessage(`{"cursor":{"x":1}}`))
		if err := service.handleLeave(conn, protocol.Message{ID: "leave", Room: "tenant-a:room-1"}); !errors.Is(err, expected) {
			t.Fatalf("expected leave presence publish error, got %v", err)
		}
		assertRuntimeNoOutbound(t, conn)
		assertRuntimeNoOutbound(t, receiver)
		if service.stats.LeavesTotal != 0 || service.metrics.LeavesTotal.Load() != 0 {
			t.Fatalf("leave stats/metrics should not increment after publish failure")
		}
		if got := service.roomEngine().MemberIDs("tenant-a:room-1", ""); !reflect.DeepEqual(got, []string{conn.id, receiver.id}) {
			t.Fatalf("leave publish error should not apply local membership, got %#v", got)
		}
		if _, ok := service.roomEngine().Snapshot("tenant-a:room-1").Presence[conn.id]; !ok {
			t.Fatalf("leave publish error should not clear local presence")
		}
	})

	t.Run("presence publish", func(t *testing.T) {
		service := newRuntimeUnitService(t)
		defer service.Close()
		store := &fakeRuntimeStore{publishPresenceErr: expected}
		service.store = store
		conn := runtimeTestConn(service, "conn-presence-publish", claims, 2)
		receiver := runtimeTestConn(service, "conn-presence-publish-receiver", claims, 2)
		joinRuntimeRoom(t, service, conn, "tenant-a:room-1")
		joinRuntimeRoom(t, service, receiver, "tenant-a:room-1")
		if err := service.handlePresence(conn, protocol.Message{
			ID:      "presence",
			Room:    "tenant-a:room-1",
			Payload: json.RawMessage(`{"cursor":{"x":1}}`),
		}); !errors.Is(err, expected) {
			t.Fatalf("expected presence publish error, got %v", err)
		}
		assertRuntimeNoOutbound(t, conn)
		assertRuntimeNoOutbound(t, receiver)
		if service.stats.PresenceUpdatesTotal != 0 || service.metrics.PresenceUpdatesTotal.Load() != 0 {
			t.Fatalf("presence stats/metrics should not increment after publish failure")
		}
		if _, ok := service.roomEngine().Snapshot("tenant-a:room-1").Presence[conn.id]; ok {
			t.Fatalf("presence publish failure should not apply local presence")
		}
	})
}

func TestRuntimeHandleClientMessageParseError(t *testing.T) {
	service := newRuntimeUnitService(t)
	defer service.Close()

	conn := runtimeTestConn(service, "conn-parse", &auth.Claims{Tenant: "tenant-a"}, 2)
	if err := service.handleClientMessage(conn, []byte(`{"t":"JOIN","id":"parse-1","room":"tenant-b:room-1"}`)); err != nil {
		t.Fatalf("parse error should be enqueued, got %v", err)
	}
	if got := readRuntimeOutbound(t, conn); got.T != "ERROR" {
		t.Fatalf("unexpected parse error response: %+v", got)
	}
}

func TestRuntimeHandleClientMessageDispatchesValidTypes(t *testing.T) {
	service := newRuntimeUnitService(t)
	defer service.Close()

	claims := &auth.Claims{
		Tenant:   "tenant-a",
		Join:     []string{"tenant-a:*"},
		Publish:  []string{"tenant-a:*"},
		Presence: []string{"tenant-a:*"},
		Scope:    "storage:tenant-a:*",
	}
	conn := runtimeTestConn(service, "conn-dispatch", claims, 8)
	if err := service.handleClientMessage(conn, []byte(`{"t":"JOIN","id":"join-1","room":"tenant-a:room-1"}`)); err != nil {
		t.Fatalf("dispatch join: %v", err)
	}
	if got := readRuntimeOutbound(t, conn); got.T != "JOINED" || got.ID != "join-1" {
		t.Fatalf("unexpected join dispatch response: %+v", got)
	}
	if err := service.handleClientMessage(conn, []byte(`{"t":"EMIT","id":"emit-1","room":"tenant-a:room-1","event":"doc.update","payload":{"ok":true}}`)); err != nil {
		t.Fatalf("dispatch emit: %v", err)
	}
	if got := readRuntimeOutbound(t, conn); got.T != "EVENT" || got.Event != "doc.update" {
		t.Fatalf("unexpected emit dispatch response: %+v", got)
	}
	if err := service.handleClientMessage(conn, []byte(`{"t":"PRESENCE_SET","id":"presence-1","room":"tenant-a:room-1","payload":{"cursor":{"x":1}}}`)); err != nil {
		t.Fatalf("dispatch presence: %v", err)
	}
	if got := readRuntimeOutbound(t, conn); got.T != "PRESENCE" {
		t.Fatalf("unexpected presence dispatch response: %+v", got)
	}
	if err := service.handleClientMessage(conn, []byte(`{"t":"STORAGE_SET","id":"storage-set-1","room":"tenant-a:room-1","payload":{"title":"Draft"},"meta":{"op_id":"op-1"}}`)); err != nil {
		t.Fatalf("dispatch storage set: %v", err)
	}
	if got := readRuntimeOutbound(t, conn); got.T != "STORAGE_ACK" || got.ID != "storage-set-1" {
		t.Fatalf("unexpected storage set dispatch response: %+v", got)
	}
	if err := service.handleClientMessage(conn, []byte(`{"t":"STORAGE_GET","id":"storage-get-1","room":"tenant-a:room-1"}`)); err != nil {
		t.Fatalf("dispatch storage get: %v", err)
	}
	if got := readRuntimeOutbound(t, conn); got.T != "STORAGE_SNAPSHOT" || got.ID != "storage-get-1" {
		t.Fatalf("unexpected storage get dispatch response: %+v", got)
	}
	if err := service.handleClientMessage(conn, []byte(`{"t":"STORAGE_PATCH","id":"storage-patch-1","room":"tenant-a:room-1","payload":[{"op":"replace","path":"/title","value":"Published"}],"meta":{"op_id":"op-2"}}`)); err != nil {
		t.Fatalf("dispatch storage patch: %v", err)
	}
	if got := readRuntimeOutbound(t, conn); got.T != "STORAGE_ACK" || got.ID != "storage-patch-1" {
		t.Fatalf("unexpected storage patch dispatch response: %+v", got)
	}
	if err := service.handleClientMessage(conn, []byte(`{"t":"LEAVE","id":"leave-1","room":"tenant-a:room-1"}`)); err != nil {
		t.Fatalf("dispatch leave: %v", err)
	}
	if got := readRuntimeOutbound(t, conn); got.T != "LEFT" || got.ID != "leave-1" {
		t.Fatalf("unexpected leave dispatch response: %+v", got)
	}
}

func TestRuntimeBroadcastAndSnapshotEdgeBranches(t *testing.T) {
	service := newRuntimeUnitService(t)
	defer service.Close()

	sender := runtimeTestConn(service, "conn-sender", &auth.Claims{Tenant: "tenant-a"}, 2)
	receiver := runtimeTestConn(service, "conn-receiver", &auth.Claims{Tenant: "tenant-a"}, 4)
	joinRuntimeRoom(t, service, sender, "tenant-a:room-1")
	joinRuntimeRoom(t, service, receiver, "tenant-a:room-1")
	setRuntimePresence(service, sender, "tenant-a:room-1", json.RawMessage(`{"cursor":{"x":1}}`))
	setRuntimePresence(service, receiver, "tenant-a:room-1", json.RawMessage(`{"cursor":{"x":2}}`))

	snapshot, err := service.snapshotRoom("tenant-a:room-1", &protocol.JoinMeta{Limit: 1})
	if err != nil {
		t.Fatalf("snapshot room: %v", err)
	}
	if len(snapshot.Members) != 1 || snapshot.NextCursor == "" || len(snapshot.Presence) != 1 {
		t.Fatalf("expected paginated snapshot with presence, members=%v presence=%v next=%q", snapshot.Members, snapshot.Presence, snapshot.NextCursor)
	}

	if err := service.broadcastEvent(cluster.PublishedEvent{
		Room:                "tenant-a:room-1",
		Event:               "doc.update",
		Payload:             json.RawMessage(`{"ok":true}`),
		ExcludeSenderConnID: sender.id,
	}, false); err != nil {
		t.Fatalf("broadcast event excluding sender: %v", err)
	}
	if got := readRuntimeOutbound(t, receiver); got.T != "EVENT" {
		t.Fatalf("unexpected receiver event: %+v", got)
	}
	select {
	case got := <-sender.send:
		t.Fatalf("sender should have been excluded, got %+v", got)
	default:
	}

	overflow := runtimeTestConn(service, "conn-overflow", &auth.Claims{Tenant: "tenant-a"}, 1)
	overflow.closed = true
	overflow.send <- outboundMessage{T: "FULL"}
	joinRuntimeRoom(t, service, overflow, "tenant-a:overflow")
	if err := service.broadcastEvent(cluster.PublishedEvent{Room: "tenant-a:overflow", Event: "doc.update"}, false); err == nil {
		t.Fatalf("expected broadcast event overflow")
	}
	if err := service.broadcastPresenceEvent(cluster.PresenceEvent{Room: "tenant-a:overflow", ConnID: "conn-other"}); err == nil {
		t.Fatalf("expected broadcast presence overflow")
	}
	sender.claims.Publish = []string{"tenant-a:*"}
	sender.claims.Presence = []string{"tenant-a:*"}
	if err := service.handleEmit(sender, protocol.Message{
		ID:      "emit-overflow",
		Room:    "tenant-a:overflow",
		Event:   "doc.update",
		Payload: json.RawMessage(`{"ok":true}`),
	}); err == nil {
		t.Fatalf("expected handle emit broadcast overflow")
	}
	if err := service.handlePresence(sender, protocol.Message{
		ID:      "presence-overflow",
		Room:    "tenant-a:overflow",
		Payload: json.RawMessage(`{"ok":true}`),
	}); err == nil {
		t.Fatalf("expected handle presence broadcast overflow")
	}
	leaver := runtimeTestConn(service, "conn-leaver", sender.claims, 1)
	blockedReceiver := runtimeTestConn(service, "conn-blocked", sender.claims, 1)
	blockedReceiver.closed = true
	blockedReceiver.send <- outboundMessage{T: "FULL"}
	joinRuntimeRoom(t, service, leaver, "tenant-a:leave-overflow")
	joinRuntimeRoom(t, service, blockedReceiver, "tenant-a:leave-overflow")
	if err := service.handleLeave(leaver, protocol.Message{ID: "leave-overflow", Room: "tenant-a:leave-overflow"}); err == nil {
		t.Fatalf("expected handle leave broadcast overflow")
	}

	yjsOverflow := &yjsConn{id: "yjs-overflow", room: "tenant-a:doc-1", send: make(chan []byte, 1), done: make(chan struct{}), closed: true}
	yjsOverflow.send <- []byte("full")
	registerRuntimeYJSConn(service, yjsOverflow)
	if err := service.broadcastYJSEvent(cluster.YJSEvent{Room: "tenant-a:doc-1", Kind: cluster.YJSEventUpdate, Update: []byte("update")}); err == nil {
		t.Fatalf("expected yjs broadcast overflow")
	}
}

func TestRuntimeStoreBackedConnectionLifecycle(t *testing.T) {
	service := newRuntimeUnitService(t)
	defer service.Close()

	store := &fakeRuntimeStore{}
	service.store = store
	conn := &clientConn{
		id:      "conn-lifecycle",
		service: service,
		claims:  &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "user-1"}, Tenant: "tenant-a"},
		send:    make(chan outboundMessage, 2),
		done:    make(chan struct{}),
		limiter: &emitLimiter{limit: service.cfg.Limits.EmitsPerSecond},
	}
	service.registerConn(conn)
	if store.touchedConnID != conn.id || store.touchedMeta.Subject != "user-1" || store.touchedMeta.Tenant != "tenant-a" {
		t.Fatalf("unexpected touched connection: id=%q meta=%+v", store.touchedConnID, store.touchedMeta)
	}
	if fanout := service.roomEngine().NotificationFanout(cluster.PublishedEvent{Event: notificationInboxCreated}, "user-1"); !reflect.DeepEqual(fanout.TargetConnIDs, []string{conn.id}) {
		t.Fatalf("expected registered connection to be a notification target, got %#v", fanout.TargetConnIDs)
	}

	receiver := runtimeTestConn(service, "conn-receiver", &auth.Claims{Tenant: "tenant-a"}, 2)
	joinRuntimeRoom(t, service, conn, "tenant-a:room-1")
	joinRuntimeRoom(t, service, receiver, "tenant-a:room-1")
	setRuntimePresence(service, conn, "tenant-a:room-1", json.RawMessage(`{"cursor":{"x":1}}`))
	conn.closed = true
	service.unregisterConn(conn)

	if store.cleanupNodeID != service.cfg.NodeID || store.cleanupConnID != conn.id {
		t.Fatalf("unexpected cleanup call: node=%q conn=%q", store.cleanupNodeID, store.cleanupConnID)
	}
	if len(store.publishedPresence) != 1 || !store.publishedPresence[0].Offline || store.publishedPresence[0].ConnID != conn.id {
		t.Fatalf("unexpected published offline presence: %+v", store.publishedPresence)
	}
	if got := readRuntimeOutbound(t, receiver); got.T != "PRESENCE" {
		t.Fatalf("expected receiver offline presence, got %+v", got)
	}
	if got := service.roomEngine().MemberIDs("tenant-a:room-1", ""); len(got) != 1 || got[0] != receiver.id {
		t.Fatalf("expected connection to be removed from room")
	}
	if fanout := service.roomEngine().NotificationFanout(cluster.PublishedEvent{Event: notificationInboxDeleted}, "user-1"); len(fanout.TargetConnIDs) != 0 {
		t.Fatalf("expected unregistered connection to be removed from notification targets, got %#v", fanout.TargetConnIDs)
	}
}

func TestRuntimeUnregisterSkipsOfflinePresenceFanoutOnPublishFailure(t *testing.T) {
	service := newRuntimeUnitService(t)
	defer service.Close()

	store := &fakeRuntimeStore{publishPresenceErr: errors.New("publish failed")}
	service.store = store
	conn := runtimeTestConn(service, "conn-disconnect-publish", &auth.Claims{Tenant: "tenant-a"}, 2)
	receiver := runtimeTestConn(service, "conn-disconnect-receiver", &auth.Claims{Tenant: "tenant-a"}, 2)
	joinRuntimeRoom(t, service, conn, "tenant-a:room-1")
	joinRuntimeRoom(t, service, receiver, "tenant-a:room-1")

	service.unregisterConn(conn)

	if store.cleanupNodeID != service.cfg.NodeID || store.cleanupConnID != conn.id {
		t.Fatalf("unexpected cleanup call: node=%q conn=%q", store.cleanupNodeID, store.cleanupConnID)
	}
	if len(store.publishedPresence) != 0 {
		t.Fatalf("presence publish failure should not record published presence: %+v", store.publishedPresence)
	}
	assertRuntimeNoOutbound(t, receiver)
}

func TestRuntimeDevConnectionsSnapshot(t *testing.T) {
	service := newRuntimeUnitService(t)
	defer service.Close()

	connB := runtimeTestConn(service, "conn-b", &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "user-b"}, Tenant: "tenant-a"}, 2)
	connA := runtimeTestConn(service, "conn-a", &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "user-a"}, Tenant: "tenant-a"}, 2)
	joinRuntimeRoom(t, service, connA, "tenant-a:room-2")
	joinRuntimeRoom(t, service, connA, "tenant-a:room-1")
	joinRuntimeRoom(t, service, connB, "tenant-a:room-1")
	registerRuntimeYJSConn(service, &yjsConn{
		id:     "yjs-a",
		claims: &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "editor-a"}, Tenant: "tenant-a"},
		room:   "tenant-a:doc-1",
		send:   make(chan []byte, 1),
		done:   make(chan struct{}),
	})

	snapshot := service.DevConnectionsSnapshot()
	if snapshot.NodeID != "node-a" {
		t.Fatalf("unexpected node id: %q", snapshot.NodeID)
	}
	if snapshot.ActiveSockets != 3 || snapshot.ActiveRoomCount != 2 {
		t.Fatalf("unexpected counts: %+v", snapshot)
	}
	if len(snapshot.Connections) != 2 || snapshot.Connections[0].ConnectionID != "conn-a" || snapshot.Connections[1].ConnectionID != "conn-b" {
		t.Fatalf("connections should be sorted by id: %+v", snapshot.Connections)
	}
	if snapshot.Connections[0].Subject != "user-a" || snapshot.Connections[0].Tenant != "tenant-a" {
		t.Fatalf("unexpected conn-a claims: %+v", snapshot.Connections[0])
	}
	if len(snapshot.Connections[0].Rooms) != 2 || snapshot.Connections[0].Rooms[0] != "tenant-a:room-1" || snapshot.Connections[0].Rooms[1] != "tenant-a:room-2" {
		t.Fatalf("unexpected conn-a rooms: %+v", snapshot.Connections[0].Rooms)
	}
	if len(snapshot.YJSConnections) != 1 || snapshot.YJSConnections[0].ConnectionID != "yjs-a" || snapshot.YJSConnections[0].Room != "tenant-a:doc-1" {
		t.Fatalf("unexpected yjs connections: %+v", snapshot.YJSConnections)
	}
	if snapshot.YJSConnections[0].Subject != "editor-a" || snapshot.YJSConnections[0].Tenant != "tenant-a" {
		t.Fatalf("unexpected yjs claims: %+v", snapshot.YJSConnections[0])
	}
}

func TestRuntimeDevStorageSnapshot(t *testing.T) {
	service := newRuntimeUnitService(t)
	defer service.Close()

	missing := service.DevStorageSnapshot("tenant-a:missing")
	if missing.NodeID != "node-a" || missing.Room != "tenant-a:missing" || missing.Found || missing.StoreBacked {
		t.Fatalf("unexpected missing storage snapshot: %+v", missing)
	}

	if _, err := service.roomEngine().SetStorage("tenant-a:room-1", json.RawMessage(`{"title":"Draft"}`), 0); err != nil {
		t.Fatalf("set storage: %v", err)
	}
	service.store = &fakeRuntimeStore{}
	snapshot := service.DevStorageSnapshot("tenant-a:room-1")
	if snapshot.NodeID != "node-a" || snapshot.Room != "tenant-a:room-1" || !snapshot.Found || !snapshot.StoreBacked {
		t.Fatalf("unexpected storage snapshot metadata: %+v", snapshot)
	}
	if snapshot.Sequence != 1 {
		t.Fatalf("unexpected storage snapshot sequence: %+v", snapshot)
	}
	if string(snapshot.Document) != `{"title":"Draft"}` {
		t.Fatalf("unexpected storage snapshot document: %s", snapshot.Document)
	}
	snapshot.Document[0] = 'X'
	again := service.DevStorageSnapshot("tenant-a:room-1")
	if string(again.Document) != `{"title":"Draft"}` {
		t.Fatalf("storage snapshot should be defensively copied, got %s", again.Document)
	}
}

func TestRuntimeDevYJSDocumentSnapshot(t *testing.T) {
	service := newRuntimeUnitService(t)
	defer service.Close()

	missing := service.DevYJSDocumentSnapshot("tenant-a:missing")
	if missing.NodeID != "node-a" || missing.Room != "tenant-a:missing" || missing.Found || missing.StoreBacked {
		t.Fatalf("unexpected missing yjs snapshot: %+v", missing)
	}

	if _, err := service.roomEngine().StoreYJSEvent(cluster.YJSEvent{
		Room:   "tenant-a:doc-1",
		Kind:   cluster.YJSEventUpdate,
		Update: []byte("update-1"),
	}); err != nil {
		t.Fatalf("store yjs update: %v", err)
	}
	if _, err := service.roomEngine().StoreYJSEvent(cluster.YJSEvent{
		Room:   "tenant-a:doc-1",
		Kind:   cluster.YJSEventSubdocUpdate,
		Update: []byte("subdoc-update"),
	}); err != nil {
		t.Fatalf("store yjs subdoc update: %v", err)
	}
	if _, err := service.roomEngine().StoreYJSEvent(cluster.YJSEvent{
		Room:   "tenant-a:doc-1",
		Kind:   cluster.YJSEventSnapshot,
		Update: []byte("snapshot-1"),
	}); err != nil {
		t.Fatalf("store yjs snapshot: %v", err)
	}
	service.store = &fakeRuntimeStore{}
	snapshot := service.DevYJSDocumentSnapshot("tenant-a:doc-1")
	if snapshot.NodeID != "node-a" || snapshot.Room != "tenant-a:doc-1" || !snapshot.Found || !snapshot.StoreBacked || !snapshot.SnapshotFound {
		t.Fatalf("unexpected yjs snapshot metadata: %+v", snapshot)
	}
	if snapshot.SnapshotBytes != len("snapshot-1") || snapshot.SnapshotHash != cluster.YJSSnapshotHash([]byte("snapshot-1")) || snapshot.SnapshotCheckpoint != 0 {
		t.Fatalf("unexpected yjs snapshot details: %+v", snapshot)
	}
	if snapshot.UpdateCount != 2 || snapshot.UpdateBytes != len("update-1")+len("subdoc-update") {
		t.Fatalf("unexpected yjs update counts: %+v", snapshot)
	}
	if !reflect.DeepEqual(snapshot.UpdateSequences, []int64{1, 2}) {
		t.Fatalf("unexpected yjs update sequences: %+v", snapshot.UpdateSequences)
	}
	if !reflect.DeepEqual(snapshot.UpdateKinds, []string{"update", "subdoc-update"}) {
		t.Fatalf("unexpected yjs update kinds: %+v", snapshot.UpdateKinds)
	}

	snapshot.UpdateSequences[0] = 99
	again := service.DevYJSDocumentSnapshot("tenant-a:doc-1")
	if !reflect.DeepEqual(again.UpdateSequences, []int64{1, 2}) {
		t.Fatalf("yjs snapshot sequences should be defensively copied, got %+v", again.UpdateSequences)
	}
}

func TestRuntimeCloseClosesActiveSockets(t *testing.T) {
	service := newRuntimeUnitService(t)

	conn := runtimeTestConn(service, "conn-close", &auth.Claims{Tenant: "tenant-a"}, 2)
	yjsConn := &yjsConn{
		id:   "yjs-close",
		room: "tenant-a:doc-1",
		send: make(chan []byte, 1),
		done: make(chan struct{}),
	}
	registerRuntimeYJSConn(service, yjsConn)

	if err := service.Close(); err != nil {
		t.Fatalf("close runtime service: %v", err)
	}
	if !conn.closed {
		t.Fatalf("expected JSON websocket connection to be closed")
	}
	if !yjsConn.closed {
		t.Fatalf("expected Yjs websocket connection to be closed")
	}
	select {
	case <-conn.done:
	default:
		t.Fatalf("expected JSON websocket done channel to be closed")
	}
	select {
	case <-yjsConn.done:
	default:
		t.Fatalf("expected Yjs websocket done channel to be closed")
	}
}

func TestRuntimeYJSStoreErrors(t *testing.T) {
	service := newRuntimeUnitService(t)
	defer service.Close()

	expected := errors.New("yjs store failed")
	store := &fakeRuntimeStore{yjsErr: expected}
	service.store = store
	if _, err := service.loadYJSDocument("tenant-a:doc-1"); !errors.Is(err, expected) {
		t.Fatalf("expected load yjs error, got %v", err)
	}

	store.yjsErr = nil
	store.appendErr = expected
	if _, err := service.storeYJSEvent(cluster.YJSEvent{
		Room:   "tenant-a:doc-1",
		Kind:   cluster.YJSEventUpdate,
		Update: []byte("update"),
	}); !errors.Is(err, expected) {
		t.Fatalf("expected append yjs error, got %v", err)
	}

	store.appendErr = nil
	store.storeSnapshotErr = expected
	if _, err := service.storeYJSEvent(cluster.YJSEvent{
		Room:   "tenant-a:doc-1",
		Kind:   cluster.YJSEventSnapshot,
		Update: []byte("snapshot"),
	}); !errors.Is(err, expected) {
		t.Fatalf("expected store yjs snapshot error, got %v", err)
	}
}

func TestRuntimeEmitPresenceAndLeaveSuccessBranches(t *testing.T) {
	service := newRuntimeUnitService(t)
	defer service.Close()

	claims := &auth.Claims{
		Tenant:   "tenant-a",
		Join:     []string{"tenant-a:*"},
		Publish:  []string{"tenant-a:*"},
		Presence: []string{"tenant-a:*"},
	}
	sender := runtimeTestConn(service, "conn-sender", claims, 8)
	receiver := runtimeTestConn(service, "conn-receiver", claims, 8)
	joinRuntimeRoom(t, service, sender, "tenant-a:room-1")
	joinRuntimeRoom(t, service, receiver, "tenant-a:room-1")

	if err := service.handleEmit(sender, protocol.Message{
		ID:       "emit-1",
		Room:     "tenant-a:room-1",
		Event:    "doc.update",
		Payload:  json.RawMessage(`{"ok":true}`),
		EmitMeta: &protocol.EmitMeta{TraceID: "trace-1"},
	}); err != nil {
		t.Fatalf("emit event: %v", err)
	}
	if got := readRuntimeOutbound(t, sender); got.T != "EVENT" || got.Event != "doc.update" {
		t.Fatalf("unexpected sender event: %+v", got)
	}
	if got := readRuntimeOutbound(t, receiver); got.T != "EVENT" || got.Event != "doc.update" {
		t.Fatalf("unexpected receiver event: %+v", got)
	}
	if service.stats.EventsTotal != 1 || service.metrics.EventsTotal.Load() != 1 {
		t.Fatalf("expected event stats/metrics to increment")
	}

	if err := service.handlePresence(sender, protocol.Message{
		ID:      "presence-1",
		Room:    "tenant-a:room-1",
		Payload: json.RawMessage(`{"cursor":{"x":1}}`),
	}); err != nil {
		t.Fatalf("presence event: %v", err)
	}
	if got := readRuntimeOutbound(t, sender); got.T != "PRESENCE" {
		t.Fatalf("unexpected sender presence: %+v", got)
	}
	if got := readRuntimeOutbound(t, receiver); got.T != "PRESENCE" {
		t.Fatalf("unexpected receiver presence: %+v", got)
	}
	if service.stats.PresenceUpdatesTotal != 1 || service.metrics.PresenceUpdatesTotal.Load() != 1 {
		t.Fatalf("expected presence stats/metrics to increment")
	}

	if err := service.handleLeave(sender, protocol.Message{ID: "leave-1", Room: "tenant-a:room-1"}); err != nil {
		t.Fatalf("leave room: %v", err)
	}
	if got := readRuntimeOutbound(t, receiver); got.T != "PRESENCE" {
		t.Fatalf("expected receiver offline presence, got %+v", got)
	}
	if got := readRuntimeOutbound(t, sender); got.T != "LEFT" || got.ID != "leave-1" {
		t.Fatalf("unexpected sender leave response: %+v", got)
	}
	if service.stats.LeavesTotal != 1 || service.metrics.LeavesTotal.Load() != 1 {
		t.Fatalf("expected leave stats/metrics to increment")
	}

	rateLimited := runtimeTestConn(service, "conn-rate", claims, 1)
	rateLimited.limiter = &emitLimiter{limit: 0}
	if err := service.handleEmit(rateLimited, protocol.Message{
		ID:      "emit-rate",
		Room:    "tenant-a:room-1",
		Event:   "doc.update",
		Payload: json.RawMessage(`{"ok":true}`),
	}); err != nil {
		t.Fatalf("rate-limited emit should enqueue error, got %v", err)
	}
	if got := readRuntimeOutbound(t, rateLimited); got.T != "ERROR" || got.ID != "emit-rate" {
		t.Fatalf("unexpected rate-limit response: %+v", got)
	}

	denied := runtimeTestConn(service, "conn-denied-presence", &auth.Claims{Tenant: "tenant-a"}, 1)
	if err := service.handlePresence(denied, protocol.Message{
		ID:      "presence-denied",
		Room:    "tenant-a:room-1",
		Payload: json.RawMessage(`{"ok":true}`),
	}); err != nil {
		t.Fatalf("denied presence should enqueue error, got %v", err)
	}
	if got := readRuntimeOutbound(t, denied); got.T != "ERROR" || got.ID != "presence-denied" {
		t.Fatalf("unexpected denied presence response: %+v", got)
	}
}

func TestRuntimeEmitUsesSequencedStoreEvent(t *testing.T) {
	service := newRuntimeUnitService(t)
	defer service.Close()

	store := &fakeRuntimeStore{}
	service.store = store
	claims := &auth.Claims{
		Tenant:  "tenant-a",
		Join:    []string{"tenant-a:*"},
		Publish: []string{"tenant-a:*"},
	}
	sender := runtimeTestConn(service, "conn-seq-sender", claims, 4)
	receiver := runtimeTestConn(service, "conn-seq-receiver", claims, 4)
	joinRuntimeRoom(t, service, sender, "tenant-a:room-1")
	joinRuntimeRoom(t, service, receiver, "tenant-a:room-1")

	if err := service.handleEmit(sender, protocol.Message{
		ID:       "emit-seq",
		Room:     "tenant-a:room-1",
		Event:    "doc.update",
		Payload:  json.RawMessage(`{"ok":true}`),
		EmitMeta: &protocol.EmitMeta{TraceID: "trace-seq"},
	}); err != nil {
		t.Fatalf("emit sequenced event: %v", err)
	}
	if len(store.publishedEvents) != 1 || store.publishedEvents[0].Sequence != 1 {
		t.Fatalf("expected sequenced store publish, got %#v", store.publishedEvents)
	}
	for _, conn := range []*clientConn{sender, receiver} {
		got := readRuntimeOutbound(t, conn)
		if got.T != "EVENT" || got.Event != "doc.update" {
			t.Fatalf("unexpected event for %s: %+v", conn.id, got)
		}
		meta, ok := got.Meta.(map[string]any)
		if !ok || meta["trace_id"] != "trace-seq" || meta["seq"] != uint64(1) {
			t.Fatalf("unexpected event metadata for %s: %#v", conn.id, got.Meta)
		}
	}
}

func TestRuntimeStorageRealtimeBranches(t *testing.T) {
	service := newRuntimeUnitService(t)
	defer service.Close()

	claims := &auth.Claims{Tenant: "tenant-a", Scope: "storage:tenant-a:*"}
	sender := runtimeTestConn(service, "conn-storage-sender", claims, 8)
	receiver := runtimeTestConn(service, "conn-storage-receiver", claims, 8)
	joinRuntimeRoom(t, service, sender, "tenant-a:room-1")
	joinRuntimeRoom(t, service, receiver, "tenant-a:room-1")

	if err := service.handleStorageSet(sender, protocol.Message{
		ID:          "storage-set",
		Room:        "tenant-a:room-1",
		Payload:     json.RawMessage(`{"liveblocksType":"LiveObject","data":{"title":"Draft"}}`),
		StorageMeta: &protocol.StorageMeta{OpID: "op-set"},
	}); err != nil {
		t.Fatalf("storage set: %v", err)
	}
	setUpdate := readRuntimeOutbound(t, receiver)
	if setUpdate.T != "STORAGE_UPDATE" || setUpdate.Room != "tenant-a:room-1" {
		t.Fatalf("unexpected storage set update: %+v", setUpdate)
	}
	if meta, ok := setUpdate.Meta.(map[string]any); !ok || meta["seq"] != uint64(1) {
		t.Fatalf("expected sequenced storage set update metadata, got %#v", setUpdate.Meta)
	}
	setPayload, ok := setUpdate.Payload.(roomengine.StorageMutation)
	if !ok || setPayload.Kind != "set" || setPayload.OpID != "op-set" || setPayload.OriginConnID != sender.id {
		t.Fatalf("unexpected storage set payload: %#v", setUpdate.Payload)
	}
	if string(setPayload.Document) != `{"liveblocksType":"LiveObject","data":{"title":"Draft"}}` {
		t.Fatalf("unexpected storage set document: %s", setPayload.Document)
	}
	setAck := readRuntimeOutbound(t, sender)
	if setAck.T != "STORAGE_ACK" || setAck.ID != "storage-set" {
		t.Fatalf("unexpected storage set ack: %+v", setAck)
	}
	if meta, ok := setAck.Meta.(map[string]any); !ok || meta["seq"] != uint64(1) {
		t.Fatalf("expected sequenced storage set ack metadata, got %#v", setAck.Meta)
	}

	if err := service.handleStorageSet(sender, protocol.Message{
		ID:          "storage-set-retry",
		Room:        "tenant-a:room-1",
		Payload:     json.RawMessage(`{"liveblocksType":"LiveObject","data":{"title":"Draft"}}`),
		StorageMeta: &protocol.StorageMeta{OpID: "op-set", ExpectedSequence: 0, ExpectedSequenceSet: true},
	}); err != nil {
		t.Fatalf("duplicate storage set should ack without fanout, got %v", err)
	}
	duplicateSetAck := readRuntimeOutbound(t, sender)
	if duplicateSetAck.T != "STORAGE_ACK" || duplicateSetAck.ID != "storage-set-retry" {
		t.Fatalf("unexpected duplicate storage set ack: %+v", duplicateSetAck)
	}
	if meta, ok := duplicateSetAck.Meta.(map[string]any); !ok || meta["seq"] != uint64(1) {
		t.Fatalf("expected duplicate set ack metadata seq=1, got %#v", duplicateSetAck.Meta)
	}
	assertRuntimeNoOutbound(t, receiver)

	if err := service.handleStorageGet(sender, protocol.Message{ID: "storage-get", Room: "tenant-a:room-1"}); err != nil {
		t.Fatalf("storage get: %v", err)
	}
	snapshot := readRuntimeOutbound(t, sender)
	if snapshot.T != "STORAGE_SNAPSHOT" || snapshot.ID != "storage-get" {
		t.Fatalf("unexpected storage snapshot: %+v", snapshot)
	}
	if meta, ok := snapshot.Meta.(map[string]any); !ok || meta["seq"] != uint64(1) {
		t.Fatalf("expected sequenced storage snapshot metadata, got %#v", snapshot.Meta)
	}

	if err := service.handleStoragePatch(sender, protocol.Message{
		ID:      "storage-patch",
		Room:    "tenant-a:room-1",
		Payload: json.RawMessage(`[{"op":"replace","path":"/data/title","value":"Published"}]`),
		StorageMeta: &protocol.StorageMeta{
			OpID: "op-patch",
		},
	}); err != nil {
		t.Fatalf("storage patch: %v", err)
	}
	patchUpdate := readRuntimeOutbound(t, receiver)
	if patchUpdate.T != "STORAGE_UPDATE" {
		t.Fatalf("unexpected storage patch update: %+v", patchUpdate)
	}
	if meta, ok := patchUpdate.Meta.(map[string]any); !ok || meta["seq"] != uint64(2) {
		t.Fatalf("expected sequenced storage patch update metadata, got %#v", patchUpdate.Meta)
	}
	patchPayload, ok := patchUpdate.Payload.(roomengine.StorageMutation)
	if !ok || patchPayload.Kind != "patch" || patchPayload.OpID != "op-patch" || len(patchPayload.Operations) != 1 {
		t.Fatalf("unexpected storage patch payload: %#v", patchUpdate.Payload)
	}
	if string(patchPayload.Document) != `{"data":{"title":"Published"},"liveblocksType":"LiveObject"}` {
		t.Fatalf("unexpected storage patch document: %s", patchPayload.Document)
	}
	patchAck := readRuntimeOutbound(t, sender)
	if patchAck.T != "STORAGE_ACK" || patchAck.ID != "storage-patch" {
		t.Fatalf("unexpected storage patch ack: %+v", patchAck)
	}
	if meta, ok := patchAck.Meta.(map[string]any); !ok || meta["seq"] != uint64(2) {
		t.Fatalf("expected sequenced storage patch ack metadata, got %#v", patchAck.Meta)
	}

	if err := service.handleStoragePatch(sender, protocol.Message{
		ID:      "storage-patch-retry",
		Room:    "tenant-a:room-1",
		Payload: json.RawMessage(`[{"op":"replace","path":"/data/title","value":"Published"}]`),
		StorageMeta: &protocol.StorageMeta{
			OpID:                "op-patch",
			ExpectedSequence:    1,
			ExpectedSequenceSet: true,
		},
	}); err != nil {
		t.Fatalf("duplicate storage patch should ack without fanout, got %v", err)
	}
	duplicatePatchAck := readRuntimeOutbound(t, sender)
	if duplicatePatchAck.T != "STORAGE_ACK" || duplicatePatchAck.ID != "storage-patch-retry" {
		t.Fatalf("unexpected duplicate storage patch ack: %+v", duplicatePatchAck)
	}
	if meta, ok := duplicatePatchAck.Meta.(map[string]any); !ok || meta["seq"] != uint64(2) {
		t.Fatalf("expected duplicate patch ack metadata seq=2, got %#v", duplicatePatchAck.Meta)
	}
	assertRuntimeNoOutbound(t, receiver)

	if err := service.handleStoragePatch(sender, protocol.Message{
		ID:      "storage-conflict",
		Room:    "tenant-a:room-1",
		Payload: json.RawMessage(`[{"op":"replace","path":"/data/title","value":"Stale"}]`),
		StorageMeta: &protocol.StorageMeta{
			OpID:                "op-conflict",
			ExpectedSequence:    1,
			ExpectedSequenceSet: true,
		},
	}); err != nil {
		t.Fatalf("conflicting storage patch should enqueue error, got %v", err)
	}
	conflict := readRuntimeOutbound(t, sender)
	if conflict.T != "ERROR" || conflict.ID != "storage-conflict" {
		t.Fatalf("unexpected storage conflict response: %+v", conflict)
	}
	if conflict.Room != "tenant-a:room-1" {
		t.Fatalf("expected storage conflict room, got %q", conflict.Room)
	}
	payload, ok := conflict.Payload.(storageConflictErrorPayload)
	if !ok ||
		payload.Code != openrtcerr.CodeStorageConflict ||
		payload.Room != "tenant-a:room-1" ||
		payload.Sequence != 2 ||
		string(payload.Document) != `{"data":{"title":"Published"},"liveblocksType":"LiveObject"}` {
		t.Fatalf("expected storage conflict error payload, got %#v", conflict.Payload)
	}
	assertRuntimeNoOutbound(t, receiver)
	current, err := service.roomEngine().GetStorage("tenant-a:room-1")
	if err != nil {
		t.Fatalf("get storage after conflict: %v", err)
	}
	if string(current) != `{"data":{"title":"Published"},"liveblocksType":"LiveObject"}` {
		t.Fatalf("conflicting patch should not mutate storage, got %s", current)
	}

	denied := runtimeTestConn(service, "conn-storage-denied", &auth.Claims{Tenant: "tenant-a"}, 1)
	if err := service.handleStorageGet(denied, protocol.Message{ID: "storage-denied", Room: "tenant-a:room-1"}); err != nil {
		t.Fatalf("denied storage should enqueue error, got %v", err)
	}
	if got := readRuntimeOutbound(t, denied); got.T != "ERROR" || got.ID != "storage-denied" {
		t.Fatalf("unexpected denied storage response: %+v", got)
	}

	if err := service.handleStoragePatch(sender, protocol.Message{
		ID:      "storage-missing",
		Room:    "tenant-a:missing",
		Payload: json.RawMessage(`[{"op":"add","path":"/title","value":"Draft"}]`),
	}); err != nil {
		t.Fatalf("missing storage patch should enqueue error, got %v", err)
	}
	if got := readRuntimeOutbound(t, sender); got.T != "ERROR" || got.ID != "storage-missing" {
		t.Fatalf("unexpected missing storage response: %+v", got)
	}
}

func TestRuntimeStorageStoreBackedBranches(t *testing.T) {
	service := newRuntimeUnitService(t)
	defer service.Close()

	store := &fakeRuntimeStore{storage: json.RawMessage(`{"title":"Draft"}`)}
	service.store = store
	claims := &auth.Claims{Tenant: "tenant-a", Scope: "storage:tenant-a:*"}
	sender := runtimeTestConn(service, "conn-storage-store", claims, 4)
	receiver := runtimeTestConn(service, "conn-storage-store-receiver", claims, 4)
	joinRuntimeRoom(t, service, sender, "tenant-a:room-1")
	joinRuntimeRoom(t, service, receiver, "tenant-a:room-1")

	if err := service.handleStoragePatch(sender, protocol.Message{
		ID:          "storage-patch-store",
		Room:        "tenant-a:room-1",
		Payload:     json.RawMessage(`[{"op":"replace","path":"/title","value":"Published"}]`),
		StorageMeta: &protocol.StorageMeta{OpID: "op-store"},
	}); err != nil {
		t.Fatalf("store backed storage patch: %v", err)
	}
	if got := readRuntimeOutbound(t, receiver); got.T != "STORAGE_UPDATE" || got.Room != "tenant-a:room-1" {
		t.Fatalf("unexpected store backed storage update: %+v", got)
	} else if meta, ok := got.Meta.(map[string]any); !ok || meta["seq"] != uint64(1) {
		t.Fatalf("expected sequenced storage update metadata, got %#v", got.Meta)
	}
	if got := readRuntimeOutbound(t, sender); got.T != "STORAGE_ACK" || got.ID != "storage-patch-store" {
		t.Fatalf("unexpected store backed storage ack: %+v", got)
	} else if meta, ok := got.Meta.(map[string]any); !ok || meta["seq"] != uint64(1) {
		t.Fatalf("expected sequenced store backed storage ack metadata, got %#v", got.Meta)
	}
	if len(store.storagePatchOperations) != 1 || store.storagePatchOperations[0].Path != "/title" {
		t.Fatalf("expected storage patch operation to reach store: %#v", store.storagePatchOperations)
	}
	if len(store.publishedEvents) != 1 || store.publishedEvents[0].Event != storageClusterEvent {
		t.Fatalf("expected storage cluster event publish, got %#v", store.publishedEvents)
	}
	stored, err := service.roomEngine().GetStorage("tenant-a:room-1")
	if err != nil {
		t.Fatalf("expected store-backed storage mutation to update room engine: %v", err)
	}
	if string(stored) != `{"title":"Published"}` {
		t.Fatalf("unexpected room engine storage after store-backed patch: %s", stored)
	}

	if err := service.handleStoragePatch(sender, protocol.Message{
		ID:      "storage-patch-store-retry",
		Room:    "tenant-a:room-1",
		Payload: json.RawMessage(`[{"op":"replace","path":"/title","value":"Published"}]`),
		StorageMeta: &protocol.StorageMeta{
			OpID:                "op-store",
			ExpectedSequence:    0,
			ExpectedSequenceSet: true,
		},
	}); err != nil {
		t.Fatalf("duplicate store backed patch should ack without publish, got %v", err)
	}
	if got := readRuntimeOutbound(t, sender); got.T != "STORAGE_ACK" || got.ID != "storage-patch-store-retry" {
		t.Fatalf("unexpected duplicate store backed storage ack: %+v", got)
	} else if meta, ok := got.Meta.(map[string]any); !ok || meta["seq"] != uint64(1) {
		t.Fatalf("expected duplicate store backed ack seq=1, got %#v", got.Meta)
	}
	assertRuntimeNoOutbound(t, receiver)
	if len(store.publishedEvents) != 1 {
		t.Fatalf("duplicate store backed patch should not publish, got %#v", store.publishedEvents)
	}

	if err := service.handleStorageGet(sender, protocol.Message{
		ID:   "storage-get-store",
		Room: "tenant-a:room-1",
	}); err != nil {
		t.Fatalf("store backed storage get: %v", err)
	}
	if got := readRuntimeOutbound(t, sender); got.T != "STORAGE_SNAPSHOT" || got.ID != "storage-get-store" {
		t.Fatalf("unexpected store backed storage snapshot: %+v", got)
	} else if meta, ok := got.Meta.(map[string]any); !ok || meta["seq"] != uint64(1) {
		t.Fatalf("expected sequenced store backed storage snapshot metadata, got %#v", got.Meta)
	}

	if err := service.handleStoragePatch(sender, protocol.Message{
		ID:      "storage-patch-store-conflict",
		Room:    "tenant-a:room-1",
		Payload: json.RawMessage(`[{"op":"replace","path":"/title","value":"Stale"}]`),
		StorageMeta: &protocol.StorageMeta{
			OpID:                "op-store-conflict",
			ExpectedSequence:    0,
			ExpectedSequenceSet: true,
		},
	}); err != nil {
		t.Fatalf("store backed conflicting storage patch should enqueue error, got %v", err)
	}
	if got := readRuntimeOutbound(t, sender); got.T != "ERROR" || got.ID != "storage-patch-store-conflict" {
		t.Fatalf("unexpected store backed conflict response: %+v", got)
	} else if got.Room != "tenant-a:room-1" {
		t.Fatalf("expected store backed storage conflict room, got %q", got.Room)
	} else {
		payload, ok := got.Payload.(storageConflictErrorPayload)
		if !ok ||
			payload.Code != openrtcerr.CodeStorageConflict ||
			payload.Room != "tenant-a:room-1" ||
			payload.Sequence != 1 ||
			string(payload.Document) != `{"title":"Published"}` {
			t.Fatalf("expected store backed storage conflict error payload, got %#v", got.Payload)
		}
	}
	assertRuntimeNoOutbound(t, receiver)
	if len(store.publishedEvents) != 1 {
		t.Fatalf("conflicting store backed patch should not publish event, got %#v", store.publishedEvents)
	}
	stored, err = service.roomEngine().GetStorage("tenant-a:room-1")
	if err != nil {
		t.Fatalf("expected store-backed storage after conflict: %v", err)
	}
	if string(stored) != `{"title":"Published"}` {
		t.Fatalf("store-backed conflict should not mutate room engine storage: %s", stored)
	}

	store.publishEventErr = errors.New("publish failed")
	if err := service.handleStoragePatch(sender, protocol.Message{
		ID:      "storage-patch-publish-failed",
		Room:    "tenant-a:room-1",
		Payload: json.RawMessage(`[{"op":"replace","path":"/title","value":"Unpublished"}]`),
	}); err != nil {
		t.Fatalf("publish-failed storage patch should enqueue error, got %v", err)
	}
	if got := readRuntimeOutbound(t, sender); got.T != "ERROR" || got.ID != "storage-patch-publish-failed" {
		t.Fatalf("unexpected publish-failed storage response: %+v", got)
	}
	assertRuntimeNoOutbound(t, receiver)
	store.publishEventErr = nil

	clusterReceiver := runtimeTestConn(service, "conn-storage-cluster", claims, 4)
	joinRuntimeRoom(t, service, clusterReceiver, "tenant-a:room-1")
	remoteMutation := roomengine.StorageMutation{
		Kind:         roomengine.StorageMutationSet,
		OpID:         "op-remote",
		OriginConnID: "conn-remote",
		Document:     json.RawMessage(`{"title":"Remote"}`),
	}
	remotePayload, err := json.Marshal(remoteMutation)
	if err != nil {
		t.Fatalf("marshal remote storage mutation: %v", err)
	}
	clusterEvent := cluster.PublishedEvent{
		Room:                "tenant-a:room-1",
		Event:               storageClusterEvent,
		Payload:             remotePayload,
		ExcludeSenderConnID: sender.id,
		OriginNode:          "node-b",
		Sequence:            42,
	}
	service.handleClusterEvent(clusterEvent)
	if got := readRuntimeOutbound(t, clusterReceiver); got.T != "STORAGE_UPDATE" {
		t.Fatalf("expected storage update from cluster event, got %+v", got)
	} else if meta, ok := got.Meta.(map[string]any); !ok || meta["seq"] != uint64(42) {
		t.Fatalf("expected sequenced cluster storage update metadata, got %#v", got.Meta)
	}
	stored, err = service.roomEngine().GetStorage("tenant-a:room-1")
	if err != nil {
		t.Fatalf("expected remote storage mutation to update room engine: %v", err)
	}
	if string(stored) != `{"title":"Remote"}` {
		t.Fatalf("unexpected room engine storage after remote mutation: %s", stored)
	}

	remoteDeletePayload, err := json.Marshal(roomengine.StorageMutation{
		Kind:     roomengine.StorageMutationDelete,
		Sequence: 43,
	})
	if err != nil {
		t.Fatalf("marshal remote storage delete mutation: %v", err)
	}
	service.handleClusterEvent(cluster.PublishedEvent{
		Room:                "tenant-a:room-1",
		Event:               storageClusterEvent,
		Payload:             remoteDeletePayload,
		ExcludeSenderConnID: sender.id,
		OriginNode:          "node-b",
		Sequence:            43,
	})
	if got := readRuntimeOutbound(t, clusterReceiver); got.T != "STORAGE_UPDATE" {
		t.Fatalf("expected storage delete update from cluster event, got %+v", got)
	} else {
		if meta, ok := got.Meta.(map[string]any); !ok || meta["seq"] != uint64(43) {
			t.Fatalf("expected sequenced cluster storage delete metadata, got %#v", got.Meta)
		}
		update, ok := got.Payload.(roomengine.StorageMutation)
		if !ok || update.Kind != roomengine.StorageMutationDelete || len(update.Document) != 0 {
			t.Fatalf("unexpected storage delete update payload: %#v", got.Payload)
		}
	}
	if _, err := service.roomEngine().GetStorage("tenant-a:room-1"); !errors.Is(err, cluster.ErrStorageNotFound) {
		t.Fatalf("expected remote storage delete to clear room engine storage, got %v", err)
	}
}

func TestRuntimeNotificationDeltaTargetsSubjectConnections(t *testing.T) {
	service := newRuntimeUnitService(t)
	defer service.Close()

	targetA := runtimeTestConn(service, "conn-target-a", &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "user-1"}, Tenant: "tenant-a"}, 2)
	targetB := runtimeTestConn(service, "conn-target-b", &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "user-1"}, Tenant: "tenant-a"}, 2)
	other := runtimeTestConn(service, "conn-other", &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "user-2"}, Tenant: "tenant-a"}, 2)
	anonymous := runtimeTestConn(service, "conn-anonymous", &auth.Claims{Tenant: "tenant-a"}, 2)

	payload := json.RawMessage(`{"type":"created","userId":"user-1","notificationId":"in_1","notification":{"id":"in_1","userId":"user-1","kind":"thread","notifiedAt":"2026-07-03T00:00:00Z"}}`)
	service.handleClusterEvent(cluster.PublishedEvent{
		Room:       "notifications:user-1",
		Event:      notificationInboxCreated,
		Payload:    payload,
		OriginNode: "admin:node-b",
	})

	for _, conn := range []*clientConn{targetA, targetB} {
		got := readRuntimeOutbound(t, conn)
		if got.T != "NOTIFICATION" || got.Event != notificationInboxCreated {
			t.Fatalf("unexpected notification delta for %s: %+v", conn.id, got)
		}
		delta, ok := got.Payload.(notificationDeltaPayload)
		if !ok || delta.UserID != "user-1" || delta.NotificationID != "in_1" || delta.Notification == nil || delta.Notification.ID != "in_1" {
			t.Fatalf("unexpected notification delta payload for %s: %#v", conn.id, got.Payload)
		}
	}
	for _, conn := range []*clientConn{other, anonymous} {
		select {
		case got := <-conn.send:
			t.Fatalf("unexpected notification delta for %s: %+v", conn.id, got)
		default:
		}
	}

	service.handleClusterEvent(cluster.PublishedEvent{
		Room:       "notifications:user-1",
		Event:      notificationInboxRead,
		Payload:    json.RawMessage(`{"type":"read","userId":"user-1","notificationId":"in_1"}`),
		OriginNode: service.cfg.NodeID,
	})
	select {
	case got := <-targetA.send:
		t.Fatalf("same-node notification event should be ignored, got %+v", got)
	default:
	}
}

func TestRuntimeYJSDocumentAndBroadcastBranches(t *testing.T) {
	service := newRuntimeUnitService(t)
	defer service.Close()

	updateEvent, err := service.storeYJSEvent(cluster.YJSEvent{
		Room:   "tenant-a:doc-1",
		Kind:   cluster.YJSEventUpdate,
		Update: []byte("update-1"),
	})
	if err != nil {
		t.Fatalf("store local yjs update: %v", err)
	}
	if updateEvent.Sequence != 1 {
		t.Fatalf("expected first local update sequence to be 1, got %d", updateEvent.Sequence)
	}
	if _, err := service.storeYJSEvent(cluster.YJSEvent{
		Room:   "tenant-a:doc-1",
		Kind:   cluster.YJSEventSnapshot,
		Update: []byte("snapshot-1"),
	}); err != nil {
		t.Fatalf("store local yjs snapshot: %v", err)
	}

	doc, err := service.loadYJSDocument("tenant-a:doc-1")
	if err != nil {
		t.Fatalf("load local yjs document: %v", err)
	}
	if string(doc.Snapshot) != "snapshot-1" || len(doc.Updates) != 1 || string(doc.Updates[0]) != "update-1" || doc.UpdateSequences[0] != 1 {
		t.Fatalf("unexpected local yjs document: %+v", doc)
	}
	if doc.SnapshotHash != cluster.YJSSnapshotHash([]byte("snapshot-1")) {
		t.Fatalf("unexpected local yjs snapshot hash: %q", doc.SnapshotHash)
	}
	if len(doc.UpdateKinds) != 1 || doc.UpdateKinds[0] != cluster.YJSEventUpdate {
		t.Fatalf("unexpected local yjs update kinds: %+v", doc.UpdateKinds)
	}
	doc.Snapshot[0] = 'X'
	doc.Updates[0][0] = 'X'
	doc.UpdateKinds[0] = cluster.YJSEventSnapshot
	reloaded, err := service.loadYJSDocument("tenant-a:doc-1")
	if err != nil {
		t.Fatalf("reload local yjs document: %v", err)
	}
	if string(reloaded.Snapshot) != "snapshot-1" || string(reloaded.Updates[0]) != "update-1" || reloaded.UpdateKinds[0] != cluster.YJSEventUpdate {
		t.Fatalf("loadYJSDocument should return defensive copies, got %+v", reloaded)
	}
	if reloaded.SnapshotHash != cluster.YJSSnapshotHash([]byte("snapshot-1")) {
		t.Fatalf("unexpected reloaded local yjs snapshot hash: %q", reloaded.SnapshotHash)
	}

	sender := &yjsConn{id: "sender", room: "tenant-a:doc-1", send: make(chan []byte, 1), done: make(chan struct{})}
	receiver := &yjsConn{id: "receiver", room: "tenant-a:doc-1", send: make(chan []byte, 1), done: make(chan struct{})}
	service.registerYJSConn(sender)
	service.registerYJSConn(receiver)
	if err := service.broadcastYJSEvent(cluster.YJSEvent{
		Room:         "tenant-a:doc-1",
		Kind:         cluster.YJSEventUpdate,
		Update:       []byte("broadcast"),
		OriginConnID: sender.id,
	}); err != nil {
		t.Fatalf("broadcast yjs event: %v", err)
	}
	select {
	case frame := <-receiver.send:
		if string(frame) != string(append([]byte{yjsFrameUpdate}, []byte("broadcast")...)) {
			t.Fatalf("unexpected receiver frame: %v", frame)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for yjs broadcast")
	}
	select {
	case frame := <-sender.send:
		t.Fatalf("sender should be excluded, got frame %v", frame)
	default:
	}

	store := &fakeRuntimeStore{appendSeq: 42}
	service.store = store
	stored, err := service.storeYJSEvent(cluster.YJSEvent{
		Room:   "tenant-a:doc-redis",
		Kind:   cluster.YJSEventUpdate,
		Update: []byte("redis-update"),
	})
	if err != nil {
		t.Fatalf("store redis-backed yjs update: %v", err)
	}
	if stored.Sequence != 42 || string(store.appendedUpdate) != "redis-update" || store.appendedKind != cluster.YJSEventUpdate {
		t.Fatalf("unexpected redis-backed yjs store: event=%+v update=%q", stored, string(store.appendedUpdate))
	}
	if _, err := service.storeYJSEvent(cluster.YJSEvent{
		Room:   "tenant-a:doc-redis",
		Kind:   cluster.YJSEventSnapshot,
		Update: []byte("redis-snapshot"),
	}); err != nil {
		t.Fatalf("store redis-backed yjs snapshot: %v", err)
	}
	if string(store.storedSnapshot) != "redis-snapshot" {
		t.Fatalf("unexpected redis-backed yjs snapshot: %q", string(store.storedSnapshot))
	}
	store.yjsDocument = cluster.YJSDocument{Snapshot: []byte("loaded"), SnapshotHash: cluster.YJSSnapshotHash([]byte("loaded"))}
	loaded, err := service.loadYJSDocument("tenant-a:doc-redis")
	if err != nil {
		t.Fatalf("load redis-backed yjs document: %v", err)
	}
	if string(loaded.Snapshot) != "loaded" {
		t.Fatalf("unexpected redis-backed yjs document: %+v", loaded)
	}
	if loaded.SnapshotHash != cluster.YJSSnapshotHash([]byte("loaded")) {
		t.Fatalf("unexpected redis-backed yjs snapshot hash: %q", loaded.SnapshotHash)
	}
}

func TestSendYJSDocument(t *testing.T) {
	conn := &yjsConn{
		send: make(chan []byte, 3),
		done: make(chan struct{}),
	}
	if err := sendYJSDocument(conn, cluster.YJSDocument{
		Snapshot:    []byte("snapshot"),
		Updates:     [][]byte{[]byte("update"), []byte("subdoc-update")},
		UpdateKinds: []cluster.YJSEventKind{cluster.YJSEventUpdate, cluster.YJSEventSubdocUpdate},
	}); err != nil {
		t.Fatalf("send yjs document: %v", err)
	}
	if frame := <-conn.send; len(frame) == 0 || frame[0] != yjsFrameSnapshot || string(frame[1:]) != "snapshot" {
		t.Fatalf("unexpected snapshot frame: %v", frame)
	}
	if frame := <-conn.send; len(frame) == 0 || frame[0] != yjsFrameUpdate || string(frame[1:]) != "update" {
		t.Fatalf("unexpected update frame: %v", frame)
	}
	if frame := <-conn.send; len(frame) == 0 || frame[0] != yjsFrameSubdocUpdate || string(frame[1:]) != "subdoc-update" {
		t.Fatalf("unexpected subdoc update frame: %v", frame)
	}

	closedSnapshot := &yjsConn{done: make(chan struct{})}
	close(closedSnapshot.done)
	if err := sendYJSDocument(closedSnapshot, cluster.YJSDocument{Snapshot: []byte("snapshot")}); err == nil {
		t.Fatalf("expected closed snapshot enqueue to fail")
	}

	closedUpdate := &yjsConn{done: make(chan struct{})}
	close(closedUpdate.done)
	if err := sendYJSDocument(closedUpdate, cluster.YJSDocument{Updates: [][]byte{[]byte("update")}}); err == nil {
		t.Fatalf("expected closed update enqueue to fail")
	}
}

func TestRuntimeBackgroundLoopsTickAndStop(t *testing.T) {
	oldHeartbeatInterval := heartbeatInterval
	oldReconcileInterval := reconcileInterval
	heartbeatInterval = time.Millisecond
	reconcileInterval = time.Millisecond
	defer func() {
		heartbeatInterval = oldHeartbeatInterval
		reconcileInterval = oldReconcileInterval
	}()

	service := newRuntimeUnitService(t)
	defer service.Close()
	store := &fakeRuntimeStore{
		touchCh:     make(chan string, 1),
		reconcileCh: make(chan string, 1),
	}
	service.store = store

	serverWS, clientWS, cleanup := runtimeTestWebSocketPair(t)
	defer cleanup()
	conn := &clientConn{
		id:      "conn-heartbeat",
		ws:      serverWS,
		service: service,
		claims:  &auth.Claims{Tenant: "tenant-a"},
		done:    make(chan struct{}),
	}
	conn.claims.Subject = "user-1"
	service.roomEngine().RegisterSession(sessionInfoFromConn(conn))
	go service.heartbeatLoop(conn)
	if got := receiveRuntimeTestValue(t, store.touchCh, "touch connection"); got != conn.id {
		t.Fatalf("unexpected touched connection: %s", got)
	}
	if store.touchedMeta.Subject != "user-1" || store.touchedMeta.Tenant != "tenant-a" {
		t.Fatalf("unexpected touched heartbeat metadata: %+v", store.touchedMeta)
	}
	close(conn.done)
	_ = clientWS.Close()

	serverYJS, clientYJS, yjsCleanup := runtimeTestWebSocketPair(t)
	defer yjsCleanup()
	yjs := &yjsConn{
		id:   "yjs-heartbeat",
		ws:   serverYJS,
		done: make(chan struct{}),
	}
	go service.yjsHeartbeatLoop(yjs)
	time.Sleep(5 * time.Millisecond)
	close(yjs.done)
	_ = clientYJS.Close()

	go service.reconcileLoop()
	if got := receiveRuntimeTestValue(t, store.reconcileCh, "reconcile node"); got != service.cfg.NodeID {
		t.Fatalf("unexpected reconciled node: %s", got)
	}

	cancelService := newRuntimeUnitService(t)
	defer cancelService.Close()
	serverYJSCancel, clientYJSCancel, yjsCancelCleanup := runtimeTestWebSocketPair(t)
	defer yjsCancelCleanup()
	yjsCancel := &yjsConn{
		id:   "yjs-cancel",
		ws:   serverYJSCancel,
		done: make(chan struct{}),
	}
	yjsExited := make(chan struct{})
	go func() {
		cancelService.yjsHeartbeatLoop(yjsCancel)
		close(yjsExited)
	}()
	cancelService.cancel()
	receiveRuntimeTestSignal(t, yjsExited, "yjs heartbeat service cancel")
	_ = clientYJSCancel.Close()
}

func TestRuntimeWriteLoopsSendAndExit(t *testing.T) {
	serverWS, clientWS, cleanup := runtimeTestWebSocketPair(t)
	defer cleanup()
	conn := &clientConn{
		ws:   serverWS,
		send: make(chan outboundMessage, 1),
		done: make(chan struct{}),
	}
	exited := make(chan struct{})
	go func() {
		conn.writeLoop()
		close(exited)
	}()
	conn.send <- outboundMessage{T: "HELLO", ID: "req-1"}
	var message outboundMessage
	if err := clientWS.ReadJSON(&message); err != nil {
		t.Fatalf("read runtime websocket message: %v", err)
	}
	if message.T != "HELLO" || message.ID != "req-1" {
		t.Fatalf("unexpected runtime websocket message: %+v", message)
	}
	close(conn.done)
	receiveRuntimeTestSignal(t, exited, "runtime write loop exit")

	serverYJS, clientYJS, yjsCleanup := runtimeTestWebSocketPair(t)
	defer yjsCleanup()
	yjs := &yjsConn{
		ws:   serverYJS,
		send: make(chan []byte, 1),
		done: make(chan struct{}),
	}
	yjsExited := make(chan struct{})
	go func() {
		yjs.writeLoop()
		close(yjsExited)
	}()
	yjs.send <- append([]byte{yjsFrameUpdate}, []byte("update")...)
	messageType, frame, err := clientYJS.ReadMessage()
	if err != nil {
		t.Fatalf("read yjs websocket message: %v", err)
	}
	if messageType != websocket.BinaryMessage || string(frame) != string(append([]byte{yjsFrameUpdate}, []byte("update")...)) {
		t.Fatalf("unexpected yjs websocket frame: type=%d frame=%v", messageType, frame)
	}
	close(yjs.done)
	receiveRuntimeTestSignal(t, yjsExited, "yjs write loop exit")
}

func TestRuntimeWriteLoopsExitOnWriteError(t *testing.T) {
	serverWS, _, cleanup := runtimeTestWebSocketPair(t)
	defer cleanup()
	conn := &clientConn{
		ws:   serverWS,
		send: make(chan outboundMessage, 1),
		done: make(chan struct{}),
	}
	exited := make(chan struct{})
	go func() {
		conn.writeLoop()
		close(exited)
	}()
	_ = serverWS.Close()
	conn.send <- outboundMessage{T: "HELLO"}
	receiveRuntimeTestSignal(t, exited, "runtime write loop write error")

	serverYJS, _, yjsCleanup := runtimeTestWebSocketPair(t)
	defer yjsCleanup()
	yjs := &yjsConn{
		ws:   serverYJS,
		send: make(chan []byte, 1),
		done: make(chan struct{}),
	}
	yjsExited := make(chan struct{})
	go func() {
		yjs.writeLoop()
		close(yjsExited)
	}()
	_ = serverYJS.Close()
	yjs.send <- append([]byte{yjsFrameUpdate}, []byte("update")...)
	receiveRuntimeTestSignal(t, yjsExited, "yjs write loop write error")
}

func TestEmitLimiter(t *testing.T) {
	limiter := &emitLimiter{limit: 1}
	if !limiter.Allow() {
		t.Fatalf("expected first emit to be allowed")
	}
	if limiter.Allow() {
		t.Fatalf("expected second emit in same window to be rejected")
	}

	disabled := &emitLimiter{limit: 0}
	if disabled.Allow() {
		t.Fatalf("expected zero-limit limiter to reject emits")
	}
}

func TestNewConnIDShape(t *testing.T) {
	id := newConnID()
	if len(id) != 24 {
		t.Fatalf("unexpected conn id length: %d", len(id))
	}
}

func TestNewConnIDPanicsWhenRandomReadFails(t *testing.T) {
	oldRandomRead := randomRead
	randomRead = func([]byte) (int, error) {
		return 0, errors.New("random failed")
	}
	defer func() {
		randomRead = oldRandomRead
		if recovered := recover(); recovered == nil {
			t.Fatalf("expected newConnID to panic")
		}
	}()

	_ = newConnID()
}

func newRuntimeAuthorizedService(t *testing.T, extraClaims map[string]any) (*Service, string, func()) {
	t.Helper()
	jwks, signToken := newRuntimeJWKS(t)
	cfg := runtimeTestConfig()
	cfg.Auth.JWKSURL = jwks.URL
	service, err := NewService(cfg, nil)
	if err != nil {
		jwks.Close()
		t.Fatalf("new authorized runtime service: %v", err)
	}
	cleanup := func() {
		_ = service.Close()
		jwks.Close()
	}
	return service, signToken(t, extraClaims), cleanup
}

func newRuntimeJWKS(t *testing.T) (*httptest.Server, func(*testing.T, map[string]any) string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate runtime jwks key: %v", err)
	}
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	signToken := func(t *testing.T, extraClaims map[string]any) string {
		t.Helper()
		claims := jwt.MapClaims{
			"iss": "https://issuer.example.com",
			"aud": "openrtc-clients",
			"exp": time.Now().Add(time.Hour).Unix(),
			"sub": "user-1",
		}
		for key, value := range extraClaims {
			claims[key] = value
		}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		token.Header["kid"] = "runtime-key"
		raw, err := token.SignedString(privateKey)
		if err != nil {
			t.Fatalf("sign runtime token: %v", err)
		}
		return raw
	}
	return jwks, signToken
}

func runtimeWritableRoomRecord() cluster.RoomRecord {
	return cluster.RoomRecord{
		ID:              "tenant-a:doc-1",
		DefaultAccesses: []string{cluster.PermissionRoomWrite},
	}
}

func runtimeTestConfig() config.RuntimeConfig {
	cfg := config.RuntimeConfig{}
	cfg.Auth.Issuer = "https://issuer.example.com"
	cfg.Auth.Audience = "openrtc-clients"
	cfg.Auth.JWKSURL = "https://issuer.example.com/jwks.json"
	cfg.NodeID = "node-a"
	cfg.Server.WSPath = "/ws"
	cfg.Limits.OutboundQueueDepth = 1
	cfg.Limits.EmitsPerSecond = 100
	cfg.Limits.YJSMaxBytes = 1024
	cfg.Tenant.EnforcePrefix = true
	cfg.Tenant.Separator = ":"
	return cfg
}

func newRuntimeUnitService(t *testing.T) *Service {
	t.Helper()
	cfg := runtimeTestConfig()
	cfg.Limits.RoomsPerConnection = 4
	service, err := NewService(cfg, nil)
	if err != nil {
		t.Fatalf("new runtime service: %v", err)
	}
	return service
}

func runtimeTestConn(service *Service, id string, claims *auth.Claims, depth int) *clientConn {
	if depth <= 0 {
		depth = 4
	}
	conn := &clientConn{
		id:      id,
		service: service,
		claims:  claims,
		send:    make(chan outboundMessage, depth),
		done:    make(chan struct{}),
		limiter: &emitLimiter{limit: service.cfg.Limits.EmitsPerSecond},
	}
	service.mu.Lock()
	service.conns[id] = conn
	service.mu.Unlock()
	service.roomEngine().RegisterSession(sessionInfoFromConn(conn))
	return conn
}

func joinRuntimeRoom(t *testing.T, service *Service, conn *clientConn, room string) {
	t.Helper()
	if _, err := service.roomEngine().Join(conn.id, room, 0); err != nil {
		t.Fatalf("join test room: %v", err)
	}
}

func setRuntimePresence(service *Service, conn *clientConn, room string, payload json.RawMessage) {
	service.roomEngine().SetPresence(conn.id, room, payload)
}

func registerRuntimeYJSConn(service *Service, conn *yjsConn) {
	service.mu.Lock()
	service.yjsConns[conn.id] = conn
	service.mu.Unlock()
	service.roomEngine().RegisterYJSSession(yjsSessionInfoFromConn(conn))
}

func compactRuntimeTestJSON(raw json.RawMessage) (json.RawMessage, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return nil, err
	}
	return append(json.RawMessage(nil), buf.Bytes()...), nil
}

func readRuntimeOutbound(t *testing.T, conn *clientConn) outboundMessage {
	t.Helper()
	select {
	case message := <-conn.send:
		return message
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for outbound runtime message")
		return outboundMessage{}
	}
}

func assertRuntimeNoOutbound(t *testing.T, conn *clientConn) {
	t.Helper()
	select {
	case message := <-conn.send:
		t.Fatalf("unexpected outbound runtime message: %+v", message)
	case <-time.After(50 * time.Millisecond):
	}
}

func receiveRuntimeTestValue[T any](t *testing.T, ch <-chan T, label string) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		var zero T
		t.Fatalf("timed out waiting for %s", label)
		return zero
	}
}

func receiveRuntimeTestSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func dialRuntimeYJS(t *testing.T, service *Service, token string) (*websocket.Conn, func()) {
	t.Helper()
	server := httptest.NewServer(service.Handler())
	ws, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[len("http"):]+"/yjs/tenant-a%3Adoc-1?token="+url.QueryEscape(token), nil)
	if err != nil {
		server.Close()
		t.Fatalf("dial runtime yjs websocket: %v", err)
	}
	cleanup := func() {
		_ = ws.Close()
		server.Close()
	}
	return ws, cleanup
}

func expectWebSocketClose(t *testing.T, ws *websocket.Conn, label string) {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := ws.ReadMessage(); err == nil {
		t.Fatalf("expected websocket close for %s", label)
	}
}

func runtimeTestWebSocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn, func()) {
	t.Helper()
	done := make(chan struct{})
	serverConn := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		serverConn <- ws
		<-done
	}))
	client, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[len("http"):], nil)
	if err != nil {
		close(done)
		server.Close()
		t.Fatalf("dial websocket: %v", err)
	}
	serverWS := receiveRuntimeTestValue(t, serverConn, "server websocket")
	cleanup := func() {
		close(done)
		_ = serverWS.Close()
		_ = client.Close()
		server.Close()
	}
	return serverWS, client, cleanup
}

type fakeRuntimeStore struct {
	healthyErr error
	roomRecord cluster.RoomRecord
	roomErr    error
	synced     []stats.Snapshot

	touchedConnID string
	touchedMeta   cluster.ConnectionMeta
	touchCh       chan string
	cleanupNodeID string
	cleanupConnID string

	publishEventErr        error
	publishedEvents        []cluster.PublishedEvent
	nextPublishedSequence  uint64
	listPublishedEventsErr error
	publishPresenceErr     error
	publishedPresence      []cluster.PresenceEvent
	joinedRooms            []roomengine.MembershipMutation
	leftRooms              []roomengine.MembershipMutation
	joinErr                error
	leaveErr               error
	setPresenceErr         error
	snapshot               cluster.Snapshot
	snapshotErr            error
	yjsDocument            cluster.YJSDocument
	yjsErr                 error
	appendSeq              int64
	appendedUpdate         []byte
	appendedKind           cluster.YJSEventKind
	appendCh               chan []byte
	appendErr              error
	storedSnapshot         []byte
	storeSnapshotErr       error
	publishYJSErr          error
	publishedYJSEvents     []cluster.YJSEvent
	publishYJSCh           chan cluster.YJSEvent
	reconcileCh            chan string
	subscribeErr           error
	subscribePresenceErr   error
	subscribeYJSErr        error
	storage                json.RawMessage
	storageSequence        uint64
	storageOpResults       map[string]cluster.StorageWriteResult
	storageOpFingerprints  map[string]string
	getStorageErr          error
	setStorageErr          error
	patchStorageErr        error
	storagePatchOperations []cluster.JSONPatchOperation
	closed                 bool
}

func (s *fakeRuntimeStore) Healthy(context.Context) error {
	return s.healthyErr
}

func (s *fakeRuntimeStore) PublishEvent(_ context.Context, event cluster.PublishedEvent) (cluster.PublishedEvent, error) {
	if s.publishEventErr != nil {
		return cluster.PublishedEvent{}, s.publishEventErr
	}
	if event.Sequence == 0 {
		s.nextPublishedSequence++
		event.Sequence = s.nextPublishedSequence
	}
	s.publishedEvents = append(s.publishedEvents, event)
	return event, nil
}

func (s *fakeRuntimeStore) ListPublishedEvents(_ context.Context, room string, afterSequence uint64, limit int) (cluster.PublishedEventList, error) {
	if s.listPublishedEventsErr != nil {
		return cluster.PublishedEventList{}, s.listPublishedEventsErr
	}
	events := make([]cluster.PublishedEvent, 0, len(s.publishedEvents))
	for _, event := range s.publishedEvents {
		if event.Room != room || event.Sequence <= afterSequence {
			continue
		}
		events = append(events, event)
		if limit > 0 && len(events) >= limit {
			break
		}
	}
	return cluster.PublishedEventList{Events: events}, nil
}

func (s *fakeRuntimeStore) Subscribe(context.Context, func(cluster.PublishedEvent)) error {
	return s.subscribeErr
}

func (s *fakeRuntimeStore) PublishPresence(_ context.Context, event cluster.PresenceEvent) error {
	if s.publishPresenceErr == nil {
		s.publishedPresence = append(s.publishedPresence, event)
	}
	return s.publishPresenceErr
}

func (s *fakeRuntimeStore) SubscribePresence(context.Context, func(cluster.PresenceEvent)) error {
	return s.subscribePresenceErr
}

func (s *fakeRuntimeStore) PublishYJSEvent(_ context.Context, event cluster.YJSEvent) error {
	if s.publishYJSErr == nil {
		s.publishedYJSEvents = append(s.publishedYJSEvents, event)
		if s.publishYJSCh != nil {
			s.publishYJSCh <- event
		}
	}
	return s.publishYJSErr
}

func (s *fakeRuntimeStore) SubscribeYJSEvents(context.Context, func(cluster.YJSEvent)) error {
	return s.subscribeYJSErr
}

func (s *fakeRuntimeStore) TouchConnection(_ context.Context, connID string, meta cluster.ConnectionMeta) error {
	s.touchedConnID = connID
	s.touchedMeta = meta
	if s.touchCh != nil {
		s.touchCh <- connID
	}
	return nil
}

func (s *fakeRuntimeStore) JoinRoom(_ context.Context, connID string, room string) error {
	if s.joinErr == nil {
		s.joinedRooms = append(s.joinedRooms, roomengine.MembershipMutation{Kind: roomengine.MembershipMutationJoin, ConnID: connID, Room: room})
	}
	return s.joinErr
}

func (s *fakeRuntimeStore) LeaveRoom(_ context.Context, connID string, room string) error {
	if s.leaveErr == nil {
		s.leftRooms = append(s.leftRooms, roomengine.MembershipMutation{Kind: roomengine.MembershipMutationLeave, ConnID: connID, Room: room})
	}
	return s.leaveErr
}

func (s *fakeRuntimeStore) SetPresence(context.Context, string, string, json.RawMessage) error {
	return s.setPresenceErr
}

func (s *fakeRuntimeStore) SetEphemeralPresence(context.Context, string, string, json.RawMessage, time.Duration) error {
	return nil
}

func (s *fakeRuntimeStore) ClearPresence(context.Context, string, string) error {
	return nil
}

func (s *fakeRuntimeStore) SnapshotRoom(context.Context, string) (cluster.Snapshot, error) {
	if s.snapshotErr != nil {
		return cluster.Snapshot{}, s.snapshotErr
	}
	return s.snapshot, nil
}

func (s *fakeRuntimeStore) ActiveUsers(context.Context, string) ([]cluster.ActiveUser, error) {
	return nil, nil
}

func (s *fakeRuntimeStore) CreateRoom(context.Context, cluster.RoomRecord) (cluster.RoomRecord, error) {
	return cluster.RoomRecord{}, nil
}

func (s *fakeRuntimeStore) GetRoom(context.Context, string) (cluster.RoomRecord, error) {
	if s.roomErr != nil {
		return cluster.RoomRecord{}, s.roomErr
	}
	if s.roomRecord.ID != "" {
		return s.roomRecord, nil
	}
	return cluster.RoomRecord{}, cluster.ErrRoomNotFound
}

func (s *fakeRuntimeStore) UpdateRoom(context.Context, string, cluster.RoomUpdate) (cluster.RoomRecord, error) {
	return cluster.RoomRecord{}, nil
}

func (s *fakeRuntimeStore) DeleteRoom(context.Context, string) error {
	return nil
}

func (s *fakeRuntimeStore) ListRooms(context.Context, string, uint64, int) (cluster.RoomList, error) {
	return cluster.RoomList{}, nil
}

func (s *fakeRuntimeStore) CreateThread(context.Context, string, cluster.ThreadRecord) (cluster.ThreadRecord, error) {
	return cluster.ThreadRecord{}, nil
}

func (s *fakeRuntimeStore) GetThread(context.Context, string, string) (cluster.ThreadRecord, error) {
	return cluster.ThreadRecord{}, nil
}

func (s *fakeRuntimeStore) ListThreads(context.Context, string) ([]cluster.ThreadRecord, error) {
	return nil, nil
}

func (s *fakeRuntimeStore) UpdateThread(context.Context, string, string, cluster.ThreadUpdate) (cluster.ThreadRecord, error) {
	return cluster.ThreadRecord{}, nil
}

func (s *fakeRuntimeStore) DeleteThread(context.Context, string, string) (cluster.ThreadRecord, error) {
	return cluster.ThreadRecord{}, nil
}

func (s *fakeRuntimeStore) GetThreadReadState(context.Context, string, string, string) (cluster.ThreadReadState, error) {
	return cluster.ThreadReadState{}, cluster.ErrThreadNotFound
}

func (s *fakeRuntimeStore) MarkThreadRead(context.Context, string, string, string) (cluster.ThreadReadState, error) {
	return cluster.ThreadReadState{}, cluster.ErrThreadNotFound
}

func (s *fakeRuntimeStore) MarkThreadUnread(context.Context, string, string, string) (cluster.ThreadReadState, error) {
	return cluster.ThreadReadState{}, cluster.ErrThreadNotFound
}

func (s *fakeRuntimeStore) AddComment(context.Context, string, string, cluster.CommentRecord) (cluster.ThreadRecord, error) {
	return cluster.ThreadRecord{}, nil
}

func (s *fakeRuntimeStore) UpdateComment(context.Context, string, string, string, cluster.CommentUpdate) (cluster.ThreadRecord, error) {
	return cluster.ThreadRecord{}, nil
}

func (s *fakeRuntimeStore) CreateInboxNotification(context.Context, cluster.InboxNotificationRecord) (cluster.InboxNotificationRecord, error) {
	return cluster.InboxNotificationRecord{}, cluster.ErrInboxAlreadyExists
}

func (s *fakeRuntimeStore) ListInboxNotifications(context.Context, string, cluster.InboxNotificationListFilter) (cluster.InboxNotificationList, error) {
	return cluster.InboxNotificationList{}, nil
}

func (s *fakeRuntimeStore) GetInboxNotification(context.Context, string, string) (cluster.InboxNotificationRecord, error) {
	return cluster.InboxNotificationRecord{}, cluster.ErrInboxNotFound
}

func (s *fakeRuntimeStore) MarkInboxNotificationRead(context.Context, string) (cluster.InboxNotificationRecord, error) {
	return cluster.InboxNotificationRecord{}, cluster.ErrInboxNotFound
}

func (s *fakeRuntimeStore) DeleteInboxNotification(context.Context, string, string) error {
	return cluster.ErrInboxNotFound
}

func (s *fakeRuntimeStore) DeleteAllInboxNotifications(context.Context, string) error {
	return nil
}

func (s *fakeRuntimeStore) GetNotificationSettings(context.Context, string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (s *fakeRuntimeStore) SetNotificationSettings(context.Context, string, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (s *fakeRuntimeStore) DeleteNotificationSettings(context.Context, string) error {
	return nil
}

func (s *fakeRuntimeStore) GetRoomSubscriptionSettings(context.Context, string, string) (cluster.RoomSubscriptionSettings, error) {
	return cluster.RoomSubscriptionSettings{}, nil
}

func (s *fakeRuntimeStore) SetRoomSubscriptionSettings(context.Context, cluster.RoomSubscriptionSettings) (cluster.RoomSubscriptionSettings, error) {
	return cluster.RoomSubscriptionSettings{}, nil
}

func (s *fakeRuntimeStore) DeleteRoomSubscriptionSettings(context.Context, string, string) error {
	return nil
}

func (s *fakeRuntimeStore) ListRoomSubscriptionSettings(context.Context, string, uint64, int) (cluster.RoomSubscriptionSettingsList, error) {
	return cluster.RoomSubscriptionSettingsList{}, nil
}

func (s *fakeRuntimeStore) GetStorage(context.Context, string) (json.RawMessage, error) {
	document, _, err := s.GetStorageWithSequence(context.Background(), "")
	return document, err
}

func (s *fakeRuntimeStore) GetStorageWithSequence(context.Context, string) (json.RawMessage, uint64, error) {
	if s.getStorageErr != nil {
		return nil, 0, s.getStorageErr
	}
	if s.storage == nil {
		return nil, 0, cluster.ErrStorageNotFound
	}
	return append(json.RawMessage(nil), s.storage...), s.storageSequence, nil
}

func (s *fakeRuntimeStore) SetStorage(_ context.Context, _ string, document json.RawMessage) (json.RawMessage, error) {
	stored, _, err := s.SetStorageWithOptions(context.Background(), "", document, cluster.StorageWriteOptions{})
	return stored, err
}

func (s *fakeRuntimeStore) SetStorageWithOptions(_ context.Context, _ string, document json.RawMessage, options cluster.StorageWriteOptions) (json.RawMessage, uint64, error) {
	result, err := s.SetStorageWithResult(context.Background(), "", document, options)
	if err != nil {
		return nil, 0, err
	}
	return result.Document, result.Sequence, nil
}

func (s *fakeRuntimeStore) SetStorageWithResult(_ context.Context, _ string, document json.RawMessage, options cluster.StorageWriteOptions) (cluster.StorageWriteResult, error) {
	if s.setStorageErr != nil {
		return cluster.StorageWriteResult{}, s.setStorageErr
	}
	stored, err := compactRuntimeTestJSON(document)
	if err != nil {
		return cluster.StorageWriteResult{}, err
	}
	if options.MaxBytes > 0 && len(stored) > options.MaxBytes {
		return cluster.StorageWriteResult{}, cluster.ErrStoragePatch
	}
	if err := cluster.ValidateStorageDocument(stored); err != nil {
		return cluster.StorageWriteResult{}, err
	}
	fingerprint := fakeStorageOpFingerprint("set", stored)
	if result, duplicate, err := s.fakeStorageOpResult(options.OpID, fingerprint); duplicate || err != nil {
		return result, err
	}
	if options.ExpectedSequenceSet && s.storageSequence != options.ExpectedSequence {
		return cluster.StorageWriteResult{}, cluster.ErrStorageConflict
	}
	s.storage = append(json.RawMessage(nil), stored...)
	s.storageSequence++
	result := cluster.StorageWriteResult{
		Kind:     "set",
		Document: append(json.RawMessage(nil), stored...),
		Sequence: s.storageSequence,
	}
	s.rememberFakeStorageOp(options.OpID, fingerprint, result)
	return result, nil
}

func (s *fakeRuntimeStore) DeleteStorage(context.Context, string) error {
	return cluster.ErrStorageNotFound
}

func (s *fakeRuntimeStore) ApplyStoragePatch(_ context.Context, _ string, operations []cluster.JSONPatchOperation, _ int) (json.RawMessage, error) {
	patched, _, err := s.ApplyStoragePatchWithOptions(context.Background(), "", operations, cluster.StorageWriteOptions{})
	return patched, err
}

func (s *fakeRuntimeStore) ApplyStoragePatchWithOptions(_ context.Context, _ string, operations []cluster.JSONPatchOperation, options cluster.StorageWriteOptions) (json.RawMessage, uint64, error) {
	result, err := s.ApplyStoragePatchWithResult(context.Background(), "", operations, options)
	if err != nil {
		return nil, 0, err
	}
	return result.Document, result.Sequence, nil
}

func (s *fakeRuntimeStore) ApplyStoragePatchWithResult(_ context.Context, _ string, operations []cluster.JSONPatchOperation, options cluster.StorageWriteOptions) (cluster.StorageWriteResult, error) {
	if s.patchStorageErr != nil {
		return cluster.StorageWriteResult{}, s.patchStorageErr
	}
	if s.storage == nil {
		return cluster.StorageWriteResult{}, cluster.ErrStorageNotFound
	}
	fingerprint := fakeStoragePatchFingerprint(operations)
	if result, duplicate, err := s.fakeStorageOpResult(options.OpID, fingerprint); duplicate || err != nil {
		return result, err
	}
	if options.ExpectedSequenceSet && s.storageSequence != options.ExpectedSequence {
		return cluster.StorageWriteResult{}, cluster.ErrStorageConflict
	}
	patched, err := cluster.ApplyJSONPatch(s.storage, operations)
	if err != nil {
		return cluster.StorageWriteResult{}, err
	}
	if options.MaxBytes > 0 && len(patched) > options.MaxBytes {
		return cluster.StorageWriteResult{}, cluster.ErrStoragePatch
	}
	if err := cluster.ValidateStorageDocument(patched); err != nil {
		return cluster.StorageWriteResult{}, err
	}
	s.storagePatchOperations = append([]cluster.JSONPatchOperation(nil), operations...)
	s.storage = append(json.RawMessage(nil), patched...)
	s.storageSequence++
	result := cluster.StorageWriteResult{
		Kind:     "patch",
		Document: append(json.RawMessage(nil), patched...),
		Sequence: s.storageSequence,
	}
	s.rememberFakeStorageOp(options.OpID, fingerprint, result)
	return result, nil
}

func (s *fakeRuntimeStore) fakeStorageOpResult(opID string, fingerprint string) (cluster.StorageWriteResult, bool, error) {
	if opID == "" {
		return cluster.StorageWriteResult{}, false, nil
	}
	result, ok := s.storageOpResults[opID]
	if !ok {
		return cluster.StorageWriteResult{}, false, nil
	}
	if s.storageOpFingerprints[opID] != fingerprint {
		return cluster.StorageWriteResult{}, false, cluster.ErrStorageConflict
	}
	result.Document = append(json.RawMessage(nil), result.Document...)
	result.Duplicate = true
	return result, true, nil
}

func (s *fakeRuntimeStore) rememberFakeStorageOp(opID string, fingerprint string, result cluster.StorageWriteResult) {
	if opID == "" {
		return
	}
	if s.storageOpResults == nil {
		s.storageOpResults = make(map[string]cluster.StorageWriteResult)
		s.storageOpFingerprints = make(map[string]string)
	}
	result.Document = append(json.RawMessage(nil), result.Document...)
	s.storageOpResults[opID] = result
	s.storageOpFingerprints[opID] = fingerprint
}

func fakeStorageOpFingerprint(kind string, document json.RawMessage) string {
	return kind + "\x00" + string(document)
}

func fakeStoragePatchFingerprint(operations []cluster.JSONPatchOperation) string {
	raw, _ := json.Marshal(operations)
	return "patch\x00" + string(raw)
}

func (s *fakeRuntimeStore) LoadYJSDocument(context.Context, string) (cluster.YJSDocument, error) {
	if s.yjsErr != nil {
		return cluster.YJSDocument{}, s.yjsErr
	}
	return s.yjsDocument, nil
}

func (s *fakeRuntimeStore) AppendYJSUpdate(_ context.Context, _ string, kind cluster.YJSEventKind, update []byte) (int64, error) {
	if s.appendErr != nil {
		return 0, s.appendErr
	}
	s.appendedKind = kind
	s.appendedUpdate = append([]byte(nil), update...)
	if s.appendCh != nil {
		s.appendCh <- append([]byte(nil), update...)
	}
	if s.appendSeq == 0 {
		s.appendSeq = 1
	}
	return s.appendSeq, nil
}

func (s *fakeRuntimeStore) StoreYJSSnapshot(_ context.Context, _ string, snapshot []byte) error {
	if s.storeSnapshotErr != nil {
		return s.storeSnapshotErr
	}
	s.storedSnapshot = append([]byte(nil), snapshot...)
	return nil
}

func (s *fakeRuntimeStore) CleanupConnection(_ context.Context, nodeID string, connID string) error {
	s.cleanupNodeID = nodeID
	s.cleanupConnID = connID
	return nil
}

func (s *fakeRuntimeStore) ReconcileNode(_ context.Context, nodeID string) error {
	if s.reconcileCh != nil {
		s.reconcileCh <- nodeID
	}
	return nil
}

func (s *fakeRuntimeStore) SyncStats(_ context.Context, _ string, snapshot stats.Snapshot) error {
	s.synced = append(s.synced, snapshot)
	return nil
}

func (s *fakeRuntimeStore) AggregateStats(context.Context) (stats.Snapshot, error) {
	return stats.Snapshot{}, nil
}

func (s *fakeRuntimeStore) Close() error {
	s.closed = true
	return nil
}
