package runtimeapp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/openrtc/openrtc/server/internal/auth"
	"github.com/openrtc/openrtc/server/internal/cluster"
	"github.com/openrtc/openrtc/server/internal/config"
	"github.com/openrtc/openrtc/server/internal/protocol"
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
				"user-1": {cluster.PermissionRoomRead},
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

	t.Run("leave room write", func(t *testing.T) {
		service := newRuntimeUnitService(t)
		defer service.Close()
		store := &fakeRuntimeStore{leaveErr: expected}
		service.store = store
		conn := runtimeTestConn(service, "conn-leave", claims, 2)
		conn.rooms["tenant-a:room-1"] = struct{}{}
		service.rooms["tenant-a:room-1"] = map[string]*clientConn{conn.id: conn}
		if err := service.handleLeave(conn, protocol.Message{ID: "leave", Room: "tenant-a:room-1"}); !errors.Is(err, expected) {
			t.Fatalf("expected leave store error, got %v", err)
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
		conn.rooms["tenant-a:room-1"] = struct{}{}
		service.rooms["tenant-a:room-1"] = map[string]*clientConn{conn.id: conn}
		if err := service.handleLeave(conn, protocol.Message{ID: "leave", Room: "tenant-a:room-1"}); !errors.Is(err, expected) {
			t.Fatalf("expected leave presence publish error, got %v", err)
		}
	})

	t.Run("presence publish", func(t *testing.T) {
		service := newRuntimeUnitService(t)
		defer service.Close()
		store := &fakeRuntimeStore{publishPresenceErr: expected}
		service.store = store
		conn := runtimeTestConn(service, "conn-presence-publish", claims, 2)
		if err := service.handlePresence(conn, protocol.Message{
			ID:      "presence",
			Room:    "tenant-a:room-1",
			Payload: json.RawMessage(`{"cursor":{"x":1}}`),
		}); !errors.Is(err, expected) {
			t.Fatalf("expected presence publish error, got %v", err)
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

func TestRuntimeBroadcastAndSnapshotEdgeBranches(t *testing.T) {
	service := newRuntimeUnitService(t)
	defer service.Close()

	sender := runtimeTestConn(service, "conn-sender", &auth.Claims{Tenant: "tenant-a"}, 2)
	receiver := runtimeTestConn(service, "conn-receiver", &auth.Claims{Tenant: "tenant-a"}, 4)
	service.rooms["tenant-a:room-1"] = map[string]*clientConn{
		sender.id:   sender,
		receiver.id: receiver,
	}
	service.presence["tenant-a:room-1"] = map[string]json.RawMessage{
		sender.id:   json.RawMessage(`{"cursor":{"x":1}}`),
		receiver.id: json.RawMessage(`{"cursor":{"x":2}}`),
	}

	members, presence, nextCursor, err := service.snapshotRoom("tenant-a:room-1", &protocol.JoinMeta{Limit: 1})
	if err != nil {
		t.Fatalf("snapshot room: %v", err)
	}
	if len(members) != 1 || nextCursor == "" || len(presence) != 1 {
		t.Fatalf("expected paginated snapshot with presence, members=%v presence=%v next=%q", members, presence, nextCursor)
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
	service.rooms["tenant-a:overflow"] = map[string]*clientConn{overflow.id: overflow}
	if err := service.broadcastEvent(cluster.PublishedEvent{Room: "tenant-a:overflow", Event: "doc.update"}, false); err == nil {
		t.Fatalf("expected broadcast event overflow")
	}
	if err := service.broadcastPresenceEvent(cluster.PresenceEvent{Room: "tenant-a:overflow", ConnID: "conn-other"}); err == nil {
		t.Fatalf("expected broadcast presence overflow")
	}

	yjsOverflow := &yjsConn{id: "yjs-overflow", room: "tenant-a:doc-1", send: make(chan []byte, 1), done: make(chan struct{}), closed: true}
	yjsOverflow.send <- []byte("full")
	service.yjsRooms["tenant-a:doc-1"] = map[string]*yjsConn{yjsOverflow.id: yjsOverflow}
	if err := service.broadcastYJSEvent(cluster.YJSEvent{Room: "tenant-a:doc-1", Kind: cluster.YJSEventUpdate, Update: []byte("update")}); err == nil {
		t.Fatalf("expected yjs broadcast overflow")
	}
}

func TestRuntimeStoreBackedConnectionLifecycle(t *testing.T) {
	service := newRuntimeUnitService(t)
	defer service.Close()

	store := &fakeRuntimeStore{}
	service.store = store
	conn := runtimeTestConn(service, "conn-lifecycle", &auth.Claims{Tenant: "tenant-a"}, 2)
	conn.claims.Subject = "user-1"
	service.registerConn(conn)
	if store.touchedConnID != conn.id || store.touchedMeta.Subject != "user-1" || store.touchedMeta.Tenant != "tenant-a" {
		t.Fatalf("unexpected touched connection: id=%q meta=%+v", store.touchedConnID, store.touchedMeta)
	}

	receiver := runtimeTestConn(service, "conn-receiver", &auth.Claims{Tenant: "tenant-a"}, 2)
	conn.rooms["tenant-a:room-1"] = struct{}{}
	service.rooms["tenant-a:room-1"] = map[string]*clientConn{
		conn.id:     conn,
		receiver.id: receiver,
	}
	service.presence["tenant-a:room-1"] = map[string]json.RawMessage{
		conn.id: json.RawMessage(`{"cursor":{"x":1}}`),
	}
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
	if _, ok := service.rooms["tenant-a:room-1"][conn.id]; ok {
		t.Fatalf("expected connection to be removed from room")
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
	sender.rooms["tenant-a:room-1"] = struct{}{}
	receiver.rooms["tenant-a:room-1"] = struct{}{}
	service.rooms["tenant-a:room-1"] = map[string]*clientConn{
		sender.id:   sender,
		receiver.id: receiver,
	}

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
	doc.Snapshot[0] = 'X'
	doc.Updates[0][0] = 'X'
	reloaded, err := service.loadYJSDocument("tenant-a:doc-1")
	if err != nil {
		t.Fatalf("reload local yjs document: %v", err)
	}
	if string(reloaded.Snapshot) != "snapshot-1" || string(reloaded.Updates[0]) != "update-1" {
		t.Fatalf("loadYJSDocument should return defensive copies, got %+v", reloaded)
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
	if stored.Sequence != 42 || string(store.appendedUpdate) != "redis-update" {
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
	store.yjsDocument = cluster.YJSDocument{Snapshot: []byte("loaded")}
	loaded, err := service.loadYJSDocument("tenant-a:doc-redis")
	if err != nil {
		t.Fatalf("load redis-backed yjs document: %v", err)
	}
	if string(loaded.Snapshot) != "loaded" {
		t.Fatalf("unexpected redis-backed yjs document: %+v", loaded)
	}
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

func runtimeTestConfig() config.RuntimeConfig {
	cfg := config.RuntimeConfig{}
	cfg.Auth.Issuer = "https://issuer.example.com"
	cfg.Auth.Audience = "openrtc-clients"
	cfg.Auth.JWKSURL = "https://issuer.example.com/jwks.json"
	cfg.NodeID = "node-a"
	cfg.Server.WSPath = "/ws"
	cfg.Limits.OutboundQueueDepth = 1
	cfg.Limits.EmitsPerSecond = 100
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
	return &clientConn{
		id:      id,
		service: service,
		claims:  claims,
		rooms:   make(map[string]struct{}),
		send:    make(chan outboundMessage, depth),
		done:    make(chan struct{}),
		limiter: &emitLimiter{limit: service.cfg.Limits.EmitsPerSecond},
	}
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

type fakeRuntimeStore struct {
	healthyErr error
	roomRecord cluster.RoomRecord
	roomErr    error
	synced     []stats.Snapshot

	touchedConnID string
	touchedMeta   cluster.ConnectionMeta
	cleanupNodeID string
	cleanupConnID string

	publishEventErr    error
	publishPresenceErr error
	publishedPresence  []cluster.PresenceEvent
	joinErr            error
	leaveErr           error
	setPresenceErr     error
	snapshot           cluster.Snapshot
	snapshotErr        error
	yjsDocument        cluster.YJSDocument
	yjsErr             error
	appendSeq          int64
	appendedUpdate     []byte
	appendErr          error
	storedSnapshot     []byte
	storeSnapshotErr   error
	publishYJSErr      error
}

func (s *fakeRuntimeStore) Healthy(context.Context) error {
	return s.healthyErr
}

func (s *fakeRuntimeStore) PublishEvent(context.Context, cluster.PublishedEvent) error {
	return s.publishEventErr
}

func (s *fakeRuntimeStore) Subscribe(context.Context, func(cluster.PublishedEvent)) error {
	return nil
}

func (s *fakeRuntimeStore) PublishPresence(_ context.Context, event cluster.PresenceEvent) error {
	if s.publishPresenceErr == nil {
		s.publishedPresence = append(s.publishedPresence, event)
	}
	return s.publishPresenceErr
}

func (s *fakeRuntimeStore) SubscribePresence(context.Context, func(cluster.PresenceEvent)) error {
	return nil
}

func (s *fakeRuntimeStore) PublishYJSEvent(context.Context, cluster.YJSEvent) error {
	return s.publishYJSErr
}

func (s *fakeRuntimeStore) SubscribeYJSEvents(context.Context, func(cluster.YJSEvent)) error {
	return nil
}

func (s *fakeRuntimeStore) TouchConnection(_ context.Context, connID string, meta cluster.ConnectionMeta) error {
	s.touchedConnID = connID
	s.touchedMeta = meta
	return nil
}

func (s *fakeRuntimeStore) JoinRoom(context.Context, string, string) error {
	return s.joinErr
}

func (s *fakeRuntimeStore) LeaveRoom(context.Context, string, string) error {
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

func (s *fakeRuntimeStore) ListThreads(context.Context, string) ([]cluster.ThreadRecord, error) {
	return nil, nil
}

func (s *fakeRuntimeStore) AddComment(context.Context, string, string, cluster.CommentRecord) (cluster.ThreadRecord, error) {
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
	return nil, cluster.ErrStorageNotFound
}

func (s *fakeRuntimeStore) SetStorage(context.Context, string, json.RawMessage) (json.RawMessage, error) {
	return nil, nil
}

func (s *fakeRuntimeStore) DeleteStorage(context.Context, string) error {
	return cluster.ErrStorageNotFound
}

func (s *fakeRuntimeStore) ApplyStoragePatch(context.Context, string, []cluster.JSONPatchOperation, int) (json.RawMessage, error) {
	return nil, cluster.ErrStorageNotFound
}

func (s *fakeRuntimeStore) LoadYJSDocument(context.Context, string) (cluster.YJSDocument, error) {
	if s.yjsErr != nil {
		return cluster.YJSDocument{}, s.yjsErr
	}
	return s.yjsDocument, nil
}

func (s *fakeRuntimeStore) AppendYJSUpdate(_ context.Context, _ string, update []byte) (int64, error) {
	if s.appendErr != nil {
		return 0, s.appendErr
	}
	s.appendedUpdate = append([]byte(nil), update...)
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

func (s *fakeRuntimeStore) ReconcileNode(context.Context, string) error {
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
	return nil
}
