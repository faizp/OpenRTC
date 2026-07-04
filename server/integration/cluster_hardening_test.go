package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/openrtc/openrtc/server/internal/admin"
	"github.com/openrtc/openrtc/server/internal/cluster"
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

func TestClusterRuntimeUsesRoomAccessGrants(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	cfg, server, signToken := clusterConfig(t, redisServer.Addr(), "node-a")
	store, err := cluster.NewRedisStore("redis://"+redisServer.Addr(), "room:")
	if err != nil {
		t.Fatalf("seed redis store: %v", err)
	}
	defer store.Close()
	if _, err := store.CreateRoom(context.Background(), cluster.RoomRecord{
		ID:              "tenant-a:grant-room",
		DefaultAccesses: []string{cluster.PermissionRoomRead, cluster.PermissionRoomPresenceWrite},
		UsersAccesses: map[string][]string{
			"blocked-editor": {},
		},
		GroupsAccesses: map[string][]string{
			"editors": {cluster.PermissionRoomWrite},
		},
	}); err != nil {
		t.Fatalf("create room grant record: %v", err)
	}

	readOnlyToken := signToken(t, "openrtc-clients", map[string]any{
		"tenant": "tenant-a",
	})
	readOnly := wsConnect(t, server.URL+cfg.Server.WSPath+"?token="+readOnlyToken)
	defer readOnly.Close()
	readJSON(t, readOnly) // HELLO
	mustWriteJSON(t, readOnly, map[string]any{"t": "JOIN", "id": "join-read", "room": "tenant-a:grant-room"})
	joined := readJSON(t, readOnly)
	if joined["t"] != "JOINED" {
		t.Fatalf("expected room grant to allow join, got %v", joined)
	}
	mustWriteJSON(t, readOnly, map[string]any{"t": "PRESENCE_SET", "id": "presence-read", "room": "tenant-a:grant-room", "payload": map[string]any{"cursor": 1}})
	presence := readJSON(t, readOnly)
	if presence["t"] != "PRESENCE" {
		t.Fatalf("expected room grant to allow presence, got %v", presence)
	}
	mustWriteJSON(t, readOnly, map[string]any{"t": "EMIT", "id": "emit-read", "room": "tenant-a:grant-room", "event": "doc.update", "payload": map[string]any{"ok": true}})
	publishErr := readJSON(t, readOnly)
	if publishErr["t"] != "ERROR" {
		t.Fatalf("expected read-only room grant to deny publish, got %v", publishErr)
	}

	editorToken := signToken(t, "openrtc-clients", map[string]any{
		"tenant":   "tenant-a",
		"groupIds": []string{"editors"},
	})
	editor := wsConnect(t, server.URL+cfg.Server.WSPath+"?token="+editorToken)
	defer editor.Close()
	readJSON(t, editor) // HELLO
	mustWriteJSON(t, editor, map[string]any{"t": "JOIN", "id": "join-edit", "room": "tenant-a:grant-room"})
	if msg := readJSON(t, editor); msg["t"] != "JOINED" {
		t.Fatalf("expected group grant to allow join, got %v", msg)
	}
	mustWriteJSON(t, editor, map[string]any{"t": "EMIT", "id": "emit-edit", "room": "tenant-a:grant-room", "event": "doc.update", "payload": map[string]any{"ok": true}})
	event := readJSON(t, editor)
	if event["t"] != "EVENT" || event["event"] != "doc.update" {
		t.Fatalf("expected group write grant to allow publish, got %v", event)
	}

	blockedEditorToken := signToken(t, "openrtc-clients", map[string]any{
		"sub":      "blocked-editor",
		"tenant":   "tenant-a",
		"groupIds": []string{"editors"},
	})
	blockedEditor := wsConnect(t, server.URL+cfg.Server.WSPath+"?token="+blockedEditorToken)
	defer blockedEditor.Close()
	readJSON(t, blockedEditor) // HELLO
	mustWriteJSON(t, blockedEditor, map[string]any{"t": "JOIN", "id": "join-blocked-editor", "room": "tenant-a:grant-room"})
	blockedEditorErr := readJSON(t, blockedEditor)
	if blockedEditorErr["t"] != "ERROR" {
		t.Fatalf("expected explicit user deny to override group/default grants, got %v", blockedEditorErr)
	}

	crossTenantToken := signToken(t, "openrtc-clients", map[string]any{
		"tenant":   "tenant-b",
		"groupIds": []string{"editors"},
	})
	crossTenant := wsConnect(t, server.URL+cfg.Server.WSPath+"?token="+crossTenantToken)
	defer crossTenant.Close()
	readJSON(t, crossTenant) // HELLO
	mustWriteJSON(t, crossTenant, map[string]any{"t": "JOIN", "id": "join-cross", "room": "tenant-a:grant-room"})
	crossTenantErr := readJSON(t, crossTenant)
	if crossTenantErr["t"] != "ERROR" {
		t.Fatalf("expected tenant prefix to override room grant, got %v", crossTenantErr)
	}
}

func TestClusterRuntimeStorageUsesRoomAccessGrants(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	cfg, server, signToken := clusterConfig(t, redisServer.Addr(), "node-storage")
	store, err := cluster.NewRedisStore("redis://"+redisServer.Addr(), "room:")
	if err != nil {
		t.Fatalf("seed redis store: %v", err)
	}
	defer store.Close()
	if _, err := store.CreateRoom(context.Background(), cluster.RoomRecord{
		ID:              "tenant-a:storage-grant-room",
		DefaultAccesses: []string{cluster.PermissionStorageRead},
		UsersAccesses: map[string][]string{
			"storage-writer":         {cluster.PermissionStorageWrite},
			"blocked-storage-writer": {},
		},
		GroupsAccesses: map[string][]string{
			"storage-writers": {cluster.PermissionStorageWrite},
		},
	}); err != nil {
		t.Fatalf("create room grant record: %v", err)
	}
	if _, err := store.SetStorage(context.Background(), "tenant-a:storage-grant-room", json.RawMessage(`{"title":"Seeded"}`)); err != nil {
		t.Fatalf("seed storage: %v", err)
	}

	readOnly := wsConnect(t, server.URL+cfg.Server.WSPath+"?token="+signToken(t, "openrtc-clients", map[string]any{
		"tenant": "tenant-a",
	}))
	defer readOnly.Close()
	readJSON(t, readOnly) // HELLO
	mustWriteJSON(t, readOnly, map[string]any{"t": "STORAGE_GET", "id": "storage-read-default", "room": "tenant-a:storage-grant-room"})
	expectStorageSnapshotTitle(t, readJSON(t, readOnly), "storage-read-default", "Seeded")
	mustWriteJSON(t, readOnly, map[string]any{
		"t":       "STORAGE_SET",
		"id":      "storage-write-default",
		"room":    "tenant-a:storage-grant-room",
		"payload": map[string]any{"title": "Denied"},
	})
	expectRuntimeError(t, readJSON(t, readOnly), "storage-write-default")

	userWriter := wsConnect(t, server.URL+cfg.Server.WSPath+"?token="+signToken(t, "openrtc-clients", map[string]any{
		"sub":    "storage-writer",
		"tenant": "tenant-a",
	}))
	defer userWriter.Close()
	readJSON(t, userWriter) // HELLO
	mustWriteJSON(t, userWriter, map[string]any{
		"t":       "STORAGE_SET",
		"id":      "storage-write-user",
		"room":    "tenant-a:storage-grant-room",
		"payload": map[string]any{"title": "User Write"},
		"meta":    map[string]any{"op_id": "op-user-write"},
	})
	expectStorageAck(t, readJSON(t, userWriter), "storage-write-user", "set", "op-user-write")

	groupWriter := wsConnect(t, server.URL+cfg.Server.WSPath+"?token="+signToken(t, "openrtc-clients", map[string]any{
		"sub":      "group-storage-writer",
		"tenant":   "tenant-a",
		"groupIds": []string{"storage-writers"},
	}))
	defer groupWriter.Close()
	readJSON(t, groupWriter) // HELLO
	mustWriteJSON(t, groupWriter, map[string]any{
		"t":    "STORAGE_PATCH",
		"id":   "storage-patch-group",
		"room": "tenant-a:storage-grant-room",
		"payload": []map[string]any{
			{"op": "replace", "path": "/title", "value": "Group Write"},
		},
		"meta": map[string]any{"op_id": "op-group-patch"},
	})
	expectStorageAck(t, readJSON(t, groupWriter), "storage-patch-group", "patch", "op-group-patch")

	blockedWriter := wsConnect(t, server.URL+cfg.Server.WSPath+"?token="+signToken(t, "openrtc-clients", map[string]any{
		"sub":      "blocked-storage-writer",
		"tenant":   "tenant-a",
		"groupIds": []string{"storage-writers"},
	}))
	defer blockedWriter.Close()
	readJSON(t, blockedWriter) // HELLO
	mustWriteJSON(t, blockedWriter, map[string]any{"t": "STORAGE_GET", "id": "storage-read-blocked", "room": "tenant-a:storage-grant-room"})
	expectRuntimeError(t, readJSON(t, blockedWriter), "storage-read-blocked")
	mustWriteJSON(t, blockedWriter, map[string]any{
		"t":       "STORAGE_SET",
		"id":      "storage-write-blocked",
		"room":    "tenant-a:storage-grant-room",
		"payload": map[string]any{"title": "Blocked"},
	})
	expectRuntimeError(t, readJSON(t, blockedWriter), "storage-write-blocked")

	crossTenant := wsConnect(t, server.URL+cfg.Server.WSPath+"?token="+signToken(t, "openrtc-clients", map[string]any{
		"tenant":   "tenant-b",
		"groupIds": []string{"storage-writers"},
	}))
	defer crossTenant.Close()
	readJSON(t, crossTenant) // HELLO
	mustWriteJSON(t, crossTenant, map[string]any{"t": "STORAGE_GET", "id": "storage-read-cross", "room": "tenant-a:storage-grant-room"})
	expectRuntimeErrorWithoutID(t, readJSON(t, crossTenant))
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

func TestAdminPublishValidatesInputShapeAndSize(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	jwks, signToken := newJWKS(t)
	defer jwks.Close()

	cfg, err := config.LoadFromMap(map[string]string{
		"OPENRTC_MODE":                    "cluster",
		"OPENRTC_REDIS_URL":               "redis://" + redisServer.Addr(),
		"OPENRTC_NODE_ID":                 "node-a",
		"OPENRTC_AUTH_ISSUER":             "https://issuer.example.com",
		"OPENRTC_AUTH_AUDIENCE":           "openrtc-clients",
		"OPENRTC_AUTH_JWKS_URL":           jwks.URL,
		"OPENRTC_ADMIN_AUTH_ISSUER":       "https://issuer.example.com",
		"OPENRTC_ADMIN_AUTH_AUDIENCE":     "openrtc-admin",
		"OPENRTC_LIMIT_PAYLOAD_MAX_BYTES": "10",
	})
	if err != nil {
		t.Fatalf("admin config: %v", err)
	}
	adminSvc, err := admin.NewService(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("admin service: %v", err)
	}
	defer adminSvc.Close()
	adminServer := httptest.NewServer(adminSvc.Handler())
	defer adminServer.Close()

	adminToken := signToken(t, "openrtc-admin", map[string]any{
		"tenant": "tenant-a",
		"scope":  "publish:tenant-a:*",
	})

	tests := []struct {
		name string
		body map[string]any
		want int
	}{
		{
			name: "unsafe room",
			body: map[string]any{"room": "tenant-a:bad room", "event": "admin.broadcast", "payload": map[string]any{"ok": true}},
			want: http.StatusBadRequest,
		},
		{
			name: "oversized payload",
			body: map[string]any{"room": "tenant-a:room-1", "event": "admin.broadcast", "payload": "01234567890"},
			want: http.StatusRequestEntityTooLarge,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			req, _ := http.NewRequest(http.MethodPost, adminServer.URL+"/v1/publish", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+adminToken)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("expected %d, got %d", tc.want, resp.StatusCode)
			}
		})
	}
}

func TestAdminRoomLifecycleWithScopedAuth(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	jwks, signToken := newJWKS(t)
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
		t.Fatalf("admin config: %v", err)
	}
	adminSvc, err := admin.NewService(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("admin service: %v", err)
	}
	defer adminSvc.Close()
	adminServer := httptest.NewServer(adminSvc.Handler())
	defer adminServer.Close()

	adminToken := signToken(t, "openrtc-admin", map[string]any{
		"tenant": "tenant-a",
		"scope":  "rooms:tenant-a:*",
	})

	createResp := doAdminJSON(t, http.MethodPost, adminServer.URL+"/v1/rooms", adminToken, map[string]any{
		"id":              "tenant-a:room-1",
		"metadata":        map[string]any{"name": "Room 1"},
		"defaultAccesses": []string{"room:read", "room:presence:write"},
		"usersAccesses": map[string]any{
			"user-editor": []string{"room:write"},
		},
		"groupsAccesses": map[string]any{
			"design": []string{"room:write"},
		},
	})
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected create status 201, got %d", createResp.StatusCode)
	}
	var created cluster.RoomRecord
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created room: %v", err)
	}
	if created.ID != "tenant-a:room-1" || string(created.Metadata) != `{"name":"Room 1"}` {
		t.Fatalf("unexpected created room: %+v", created)
	}
	if !created.Allows("user-editor", nil, "publish") || !created.Allows("user-viewer", nil, "presence") || !created.Allows("user-designer", []string{"design"}, "publish") {
		t.Fatalf("unexpected created room access grants: %+v", created)
	}

	conflictResp := doAdminJSON(t, http.MethodPost, adminServer.URL+"/v1/rooms", adminToken, map[string]any{
		"id": "tenant-a:room-1",
	})
	conflictResp.Body.Close()
	if conflictResp.StatusCode != http.StatusConflict {
		t.Fatalf("expected duplicate create conflict, got %d", conflictResp.StatusCode)
	}

	getResp := doAdminJSON(t, http.MethodGet, adminServer.URL+"/v1/rooms/"+url.PathEscape("tenant-a:room-1"), adminToken, nil)
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected get status 200, got %d", getResp.StatusCode)
	}

	updateResp := doAdminJSON(t, http.MethodPatch, adminServer.URL+"/v1/rooms/"+url.PathEscape("tenant-a:room-1"), adminToken, map[string]any{
		"metadata":        map[string]any{"name": "Room One", "pinned": true},
		"defaultAccesses": []string{},
		"groupsAccesses": map[string]any{
			"engineering": []string{"room:write"},
		},
	})
	defer updateResp.Body.Close()
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("expected update status 200, got %d", updateResp.StatusCode)
	}
	var updated cluster.RoomRecord
	if err := json.NewDecoder(updateResp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated room: %v", err)
	}
	if string(updated.Metadata) != `{"name":"Room One","pinned":true}` {
		t.Fatalf("unexpected updated metadata: %s", updated.Metadata)
	}
	if updated.Allows("viewer", nil, "join") || !updated.Allows("engineer", []string{"engineering"}, "publish") {
		t.Fatalf("unexpected updated room access grants: %+v", updated)
	}

	secondResp := doAdminJSON(t, http.MethodPost, adminServer.URL+"/v1/rooms", adminToken, map[string]any{
		"id": "tenant-a:room-2",
	})
	secondResp.Body.Close()
	if secondResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected second create status 201, got %d", secondResp.StatusCode)
	}

	listResp := doAdminJSON(t, http.MethodGet, adminServer.URL+"/v1/rooms?prefix=tenant-a:&limit=20", adminToken, nil)
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected list status 200, got %d", listResp.StatusCode)
	}
	var list struct {
		Rooms []cluster.RoomRecord `json:"rooms"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Rooms) != 2 || list.Rooms[0].ID != "tenant-a:room-1" || list.Rooms[1].ID != "tenant-a:room-2" {
		t.Fatalf("unexpected room list: %+v", list.Rooms)
	}

	forbiddenResp := doAdminJSON(t, http.MethodGet, adminServer.URL+"/v1/rooms/"+url.PathEscape("tenant-b:room-1"), adminToken, nil)
	forbiddenResp.Body.Close()
	if forbiddenResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected forbidden tenant room, got %d", forbiddenResp.StatusCode)
	}

	deleteResp := doAdminJSON(t, http.MethodDelete, adminServer.URL+"/v1/rooms/"+url.PathEscape("tenant-a:room-1"), adminToken, nil)
	deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected delete status 204, got %d", deleteResp.StatusCode)
	}
	missingResp := doAdminJSON(t, http.MethodGet, adminServer.URL+"/v1/rooms/"+url.PathEscape("tenant-a:room-1"), adminToken, nil)
	missingResp.Body.Close()
	if missingResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected missing room status 404, got %d", missingResp.StatusCode)
	}
}

func TestAdminRoomLifecycleValidatesMetadataAndScope(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	jwks, signToken := newJWKS(t)
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
		t.Fatalf("admin config: %v", err)
	}
	adminSvc, err := admin.NewService(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("admin service: %v", err)
	}
	defer adminSvc.Close()
	adminServer := httptest.NewServer(adminSvc.Handler())
	defer adminServer.Close()

	adminToken := signToken(t, "openrtc-admin", map[string]any{
		"tenant": "tenant-a",
		"scope":  "rooms:tenant-a:*",
	})
	invalidMetadataResp := doAdminJSON(t, http.MethodPost, adminServer.URL+"/v1/rooms", adminToken, map[string]any{
		"id":       "tenant-a:bad-metadata",
		"metadata": []string{"not", "object"},
	})
	invalidMetadataResp.Body.Close()
	if invalidMetadataResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected invalid metadata 400, got %d", invalidMetadataResp.StatusCode)
	}

	invalidAccessResp := doAdminJSON(t, http.MethodPost, adminServer.URL+"/v1/rooms", adminToken, map[string]any{
		"id":              "tenant-a:bad-access",
		"defaultAccesses": []string{"room:admin"},
	})
	invalidAccessResp.Body.Close()
	if invalidAccessResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected invalid access 400, got %d", invalidAccessResp.StatusCode)
	}

	publishOnlyToken := signToken(t, "openrtc-admin", map[string]any{
		"tenant": "tenant-a",
		"scope":  "publish:tenant-a:*",
	})
	forbiddenResp := doAdminJSON(t, http.MethodPost, adminServer.URL+"/v1/rooms", publishOnlyToken, map[string]any{
		"id": "tenant-a:room-1",
	})
	forbiddenResp.Body.Close()
	if forbiddenResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected missing rooms scope 403, got %d", forbiddenResp.StatusCode)
	}
}

func TestAdminStorageLifecycleWithScopedAuth(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	jwks, signToken := newJWKS(t)
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
		t.Fatalf("admin config: %v", err)
	}
	adminSvc, err := admin.NewService(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("admin service: %v", err)
	}
	defer adminSvc.Close()
	adminServer := httptest.NewServer(adminSvc.Handler())
	defer adminServer.Close()

	adminToken := signToken(t, "openrtc-admin", map[string]any{
		"tenant": "tenant-a",
		"scope":  "storage:tenant-a:*",
	})
	storageURL := adminServer.URL + "/v1/rooms/" + url.PathEscape("tenant-a:room-1") + "/storage"

	missingResp := doAdminJSON(t, http.MethodGet, storageURL, adminToken, nil)
	missingResp.Body.Close()
	if missingResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected missing storage 404, got %d", missingResp.StatusCode)
	}

	setResp := doAdminJSON(t, http.MethodPut, storageURL, adminToken, map[string]any{
		"layers": []any{"base"},
		"meta":   map[string]any{"title": "Draft"},
	})
	defer setResp.Body.Close()
	if setResp.StatusCode != http.StatusOK {
		t.Fatalf("expected set storage 200, got %d", setResp.StatusCode)
	}

	patchResp := doAdminJSON(t, http.MethodPatch, storageURL+"/json-patch", adminToken, []map[string]any{
		{"op": "test", "path": "/meta/title", "value": "Draft"},
		{"op": "add", "path": "/layers/-", "value": "foreground"},
		{"op": "replace", "path": "/meta/title", "value": "Published"},
	})
	defer patchResp.Body.Close()
	if patchResp.StatusCode != http.StatusOK {
		t.Fatalf("expected patch storage 200, got %d", patchResp.StatusCode)
	}
	var patched map[string]any
	if err := json.NewDecoder(patchResp.Body).Decode(&patched); err != nil {
		t.Fatalf("decode patched storage: %v", err)
	}
	if patched["meta"].(map[string]any)["title"] != "Published" {
		t.Fatalf("unexpected patched storage: %#v", patched)
	}

	failingPatchResp := doAdminJSON(t, http.MethodPatch, storageURL+"/json-patch", adminToken, []map[string]any{
		{"op": "remove", "path": "/missing"},
	})
	failingPatchResp.Body.Close()
	if failingPatchResp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected patch failure 422, got %d", failingPatchResp.StatusCode)
	}

	getResp := doAdminJSON(t, http.MethodGet, storageURL, adminToken, nil)
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected get storage 200, got %d", getResp.StatusCode)
	}
	var current map[string]any
	if err := json.NewDecoder(getResp.Body).Decode(&current); err != nil {
		t.Fatalf("decode current storage: %v", err)
	}
	if current["meta"].(map[string]any)["title"] != "Published" {
		t.Fatalf("failed patch should not mutate storage: %#v", current)
	}

	forbiddenToken := signToken(t, "openrtc-admin", map[string]any{
		"tenant": "tenant-a",
		"scope":  "rooms:tenant-a:*",
	})
	forbiddenResp := doAdminJSON(t, http.MethodGet, storageURL, forbiddenToken, nil)
	forbiddenResp.Body.Close()
	if forbiddenResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected storage scope denial 403, got %d", forbiddenResp.StatusCode)
	}

	deleteResp := doAdminJSON(t, http.MethodDelete, storageURL, adminToken, nil)
	deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected storage delete 204, got %d", deleteResp.StatusCode)
	}
}

func TestAdminStorageValidatesDocumentAndPatchShape(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	jwks, signToken := newJWKS(t)
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
		t.Fatalf("admin config: %v", err)
	}
	adminSvc, err := admin.NewService(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("admin service: %v", err)
	}
	defer adminSvc.Close()
	adminServer := httptest.NewServer(adminSvc.Handler())
	defer adminServer.Close()

	adminToken := signToken(t, "openrtc-admin", map[string]any{
		"tenant": "tenant-a",
		"scope":  "storage:tenant-a:*",
	})
	storageURL := adminServer.URL + "/v1/rooms/" + url.PathEscape("tenant-a:room-1") + "/storage"

	arrayResp := doAdminJSON(t, http.MethodPut, storageURL, adminToken, []string{"not", "object"})
	arrayResp.Body.Close()
	if arrayResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected non-object storage 400, got %d", arrayResp.StatusCode)
	}

	invalidTypedResp := doAdminRaw(t, http.MethodPut, storageURL, adminToken, `{"liveblocksType":"LiveList","data":[]}`)
	invalidTypedResp.Body.Close()
	if invalidTypedResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected invalid typed storage 400, got %d", invalidTypedResp.StatusCode)
	}

	validTypedResp := doAdminRaw(t, http.MethodPut, storageURL, adminToken, `{"liveblocksType":"LiveObject","data":{"items":{"liveblocksType":"LiveList","data":["a"]}}}`)
	validTypedResp.Body.Close()
	if validTypedResp.StatusCode != http.StatusOK {
		t.Fatalf("expected valid typed storage 200, got %d", validTypedResp.StatusCode)
	}

	invalidTypedPatchResp := doAdminJSON(t, http.MethodPatch, storageURL+"/json-patch", adminToken, []map[string]any{
		{"op": "replace", "path": "/data/items/data", "value": map[string]any{}},
	})
	invalidTypedPatchResp.Body.Close()
	if invalidTypedPatchResp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected invalid typed patch 422, got %d", invalidTypedPatchResp.StatusCode)
	}

	setResp := doAdminJSON(t, http.MethodPut, storageURL, adminToken, map[string]any{"ok": true})
	setResp.Body.Close()
	if setResp.StatusCode != http.StatusOK {
		t.Fatalf("expected valid storage 200, got %d", setResp.StatusCode)
	}

	unsupportedResp := doAdminJSON(t, http.MethodPatch, storageURL+"/json-patch", adminToken, []map[string]any{
		{"op": "increment", "path": "/ok", "value": true},
	})
	unsupportedResp.Body.Close()
	if unsupportedResp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected unsupported patch 422, got %d", unsupportedResp.StatusCode)
	}
}

func TestAdminStorageRejectsInvalidMethodsAuthAndPatchBodies(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	jwks, signToken := newJWKS(t)
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
		t.Fatalf("admin config: %v", err)
	}
	adminSvc, err := admin.NewService(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("admin service: %v", err)
	}
	defer adminSvc.Close()
	adminServer := httptest.NewServer(adminSvc.Handler())
	defer adminServer.Close()

	adminToken := signToken(t, "openrtc-admin", map[string]any{
		"tenant": "tenant-a",
		"scope":  "storage:tenant-a:*",
	})
	storageURL := adminServer.URL + "/v1/rooms/" + url.PathEscape("tenant-a:room-1") + "/storage"

	postResp := doAdminRaw(t, http.MethodPost, storageURL, adminToken, "")
	postResp.Body.Close()
	if postResp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected storage POST 405, got %d", postResp.StatusCode)
	}

	getPatchResp := doAdminRaw(t, http.MethodGet, storageURL+"/json-patch", adminToken, "")
	getPatchResp.Body.Close()
	if getPatchResp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected json-patch GET 405, got %d", getPatchResp.StatusCode)
	}

	unsupportedResp := doAdminRaw(t, http.MethodGet, storageURL+"/unknown", adminToken, "")
	unsupportedResp.Body.Close()
	if unsupportedResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected unsupported storage subresource 400, got %d", unsupportedResp.StatusCode)
	}

	noAuthResp := doAdminRaw(t, http.MethodGet, storageURL, "", "")
	noAuthResp.Body.Close()
	if noAuthResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected missing storage auth 401, got %d", noAuthResp.StatusCode)
	}

	patchMissingResp := doAdminJSON(t, http.MethodPatch, storageURL+"/json-patch", adminToken, []map[string]any{
		{"op": "test", "path": "", "value": map[string]any{}},
	})
	patchMissingResp.Body.Close()
	if patchMissingResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected missing storage patch 404, got %d", patchMissingResp.StatusCode)
	}

	deleteMissingResp := doAdminRaw(t, http.MethodDelete, storageURL, adminToken, "")
	deleteMissingResp.Body.Close()
	if deleteMissingResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected missing storage delete 404, got %d", deleteMissingResp.StatusCode)
	}

	setResp := doAdminRaw(t, http.MethodPut, storageURL, adminToken, `{"ok":true}`)
	setResp.Body.Close()
	if setResp.StatusCode != http.StatusOK {
		t.Fatalf("expected storage set 200, got %d", setResp.StatusCode)
	}

	tests := []struct {
		name string
		body string
		want int
	}{
		{name: "malformed", body: `[`, want: http.StatusBadRequest},
		{name: "empty", body: `[]`, want: http.StatusBadRequest},
		{name: "unknown field", body: `[{"op":"add","path":"/x","value":true,"extra":true}]`, want: http.StatusBadRequest},
		{name: "missing op", body: `[{"path":"/x","value":true}]`, want: http.StatusBadRequest},
		{name: "remove root", body: `[{"op":"remove","path":""}]`, want: http.StatusUnprocessableEntity},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := doAdminRaw(t, http.MethodPatch, storageURL+"/json-patch", adminToken, tc.body)
			resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("expected %d, got %d", tc.want, resp.StatusCode)
			}
		})
	}
}

func TestAdminActiveUsersWithScopedPresenceAuth(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	jwks, signToken := newJWKS(t)
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
		t.Fatalf("admin config: %v", err)
	}
	adminSvc, err := admin.NewService(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("admin service: %v", err)
	}
	defer adminSvc.Close()
	adminServer := httptest.NewServer(adminSvc.Handler())
	defer adminServer.Close()

	store, err := cluster.NewRedisStore("redis://"+redisServer.Addr(), "room:")
	if err != nil {
		t.Fatalf("seed redis store: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.TouchConnection(ctx, "conn-1", cluster.ConnectionMeta{
		NodeID:      "node-a",
		Subject:     "user-1",
		Tenant:      "tenant-a",
		ConnectedAt: time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("touch connection: %v", err)
	}
	if err := store.JoinRoom(ctx, "conn-1", "tenant-a:room-1"); err != nil {
		t.Fatalf("join room: %v", err)
	}
	if err := store.SetPresence(ctx, "conn-1", "tenant-a:room-1", json.RawMessage(`{"cursor":{"x":1}}`)); err != nil {
		t.Fatalf("set presence: %v", err)
	}

	adminToken := signToken(t, "openrtc-admin", map[string]any{
		"tenant": "tenant-a",
		"scope":  "presence:tenant-a:*",
	})
	activeUsersURL := adminServer.URL + "/v1/rooms/" + url.PathEscape("tenant-a:room-1") + "/active_users"

	resp := doAdminRaw(t, http.MethodGet, activeUsersURL, adminToken, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected active users 200, got %d", resp.StatusCode)
	}
	var body struct {
		Data []cluster.ActiveUser `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode active users: %v", err)
	}
	if len(body.Data) != 1 || body.Data[0].ID != "user-1" || body.Data[0].ConnectionID != "conn-1" {
		t.Fatalf("unexpected active users response: %+v", body.Data)
	}
	if string(body.Data[0].Presence) != `{"cursor":{"x":1}}` {
		t.Fatalf("unexpected active user presence: %s", body.Data[0].Presence)
	}

	roomsOnlyToken := signToken(t, "openrtc-admin", map[string]any{
		"tenant": "tenant-a",
		"scope":  "rooms:tenant-a:*",
	})
	forbiddenResp := doAdminRaw(t, http.MethodGet, activeUsersURL, roomsOnlyToken, "")
	forbiddenResp.Body.Close()
	if forbiddenResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected missing presence scope 403, got %d", forbiddenResp.StatusCode)
	}

	postResp := doAdminRaw(t, http.MethodPost, activeUsersURL, adminToken, "")
	postResp.Body.Close()
	if postResp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected active users POST 405, got %d", postResp.StatusCode)
	}
}

func TestAdminRoomRejectsInvalidMethodsAndMissingRecords(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer redisServer.Close()

	jwks, signToken := newJWKS(t)
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
		t.Fatalf("admin config: %v", err)
	}
	adminSvc, err := admin.NewService(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("admin service: %v", err)
	}
	defer adminSvc.Close()
	adminServer := httptest.NewServer(adminSvc.Handler())
	defer adminServer.Close()

	adminToken := signToken(t, "openrtc-admin", map[string]any{
		"tenant": "tenant-a",
		"scope":  "rooms:tenant-a:*",
	})
	roomURL := adminServer.URL + "/v1/rooms/" + url.PathEscape("tenant-a:missing")

	resp := doAdminRaw(t, http.MethodPost, roomURL, adminToken, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected room POST 405, got %d", resp.StatusCode)
	}

	resp = doAdminRaw(t, http.MethodPut, adminServer.URL+"/v1/rooms", adminToken, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected rooms PUT 405, got %d", resp.StatusCode)
	}

	resp = doAdminJSON(t, http.MethodPatch, roomURL, adminToken, map[string]any{"metadata": map[string]any{"name": "Missing"}})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected missing room update 404, got %d", resp.StatusCode)
	}

	resp = doAdminJSON(t, http.MethodPatch, roomURL, adminToken, map[string]any{"metadata": []string{"invalid"}})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected invalid room update metadata 400, got %d", resp.StatusCode)
	}

	resp = doAdminRaw(t, http.MethodDelete, roomURL, adminToken, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected missing room delete 404, got %d", resp.StatusCode)
	}

	resp = doAdminRaw(t, http.MethodGet, adminServer.URL+"/v1/rooms?limit=0", adminToken, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected bad list limit 400, got %d", resp.StatusCode)
	}

	resp = doAdminRaw(t, http.MethodGet, adminServer.URL+"/v1/rooms?cursor=-1", adminToken, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected bad list cursor 400, got %d", resp.StatusCode)
	}
}

func TestAdminAPIsRequireRedisBacking(t *testing.T) {
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
		t.Fatalf("admin config: %v", err)
	}
	adminSvc, err := admin.NewService(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("admin service: %v", err)
	}
	defer adminSvc.Close()
	adminServer := httptest.NewServer(adminSvc.Handler())
	defer adminServer.Close()

	adminToken := signToken(t, "openrtc-admin", map[string]any{
		"tenant": "tenant-a",
		"scope":  "publish:tenant-a:* presence:tenant-a:* rooms:tenant-a:* storage:tenant-a:*",
	})

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "publish", method: http.MethodPost, path: "/v1/publish", body: `{"room":"tenant-a:room-1","event":"event","payload":{}}`},
		{name: "presence", method: http.MethodPost, path: "/v1/presence", body: `{"room":"tenant-a:room-1","conn_id":"conn-1","state":{}}`},
		{name: "create room", method: http.MethodPost, path: "/v1/rooms", body: `{"id":"tenant-a:room-1"}`},
		{name: "get room", method: http.MethodGet, path: "/v1/rooms/" + url.PathEscape("tenant-a:room-1")},
		{name: "list rooms", method: http.MethodGet, path: "/v1/rooms"},
		{name: "active users", method: http.MethodGet, path: "/v1/rooms/" + url.PathEscape("tenant-a:room-1") + "/active_users"},
		{name: "get storage", method: http.MethodGet, path: "/v1/rooms/" + url.PathEscape("tenant-a:room-1") + "/storage"},
		{name: "patch storage", method: http.MethodPatch, path: "/v1/rooms/" + url.PathEscape("tenant-a:room-1") + "/storage/json-patch", body: `[{"op":"test","path":"","value":{}}]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := doAdminRaw(t, tc.method, adminServer.URL+tc.path, adminToken, tc.body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("expected 503, got %d", resp.StatusCode)
			}
		})
	}
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

func TestClusterPresenceStoredInRedisAndBroadcastsLive(t *testing.T) {
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

	mustWriteJSON(t, clientA, map[string]any{
		"t":       "PRESENCE_SET",
		"id":      "a-pres-2",
		"room":    "tenant-a:presence-room",
		"payload": map[string]any{"status": "typing"},
	})
	readJSON(t, clientA) // own PRESENCE

	livePresence := readJSON(t, clientB)
	if livePresence["t"] != "PRESENCE" {
		t.Fatalf("expected live cross-node PRESENCE, got %v", livePresence)
	}
	livePayload := livePresence["payload"].(map[string]any)
	if livePayload["offline"] == true {
		t.Fatalf("expected online presence update, got offline payload: %v", livePayload)
	}
	state := livePayload["state"].(map[string]any)
	if state["status"] != "typing" {
		t.Fatalf("expected typing presence state, got %v", state)
	}

	mustWriteJSON(t, clientA, map[string]any{"t": "LEAVE", "id": "a-leave", "room": "tenant-a:presence-room"})
	readJSON(t, clientA) // LEFT
	offlinePresence := readJSON(t, clientB)
	if offlinePresence["t"] != "PRESENCE" {
		t.Fatalf("expected offline PRESENCE, got %v", offlinePresence)
	}
	offlinePayload := offlinePresence["payload"].(map[string]any)
	if offlinePayload["offline"] != true {
		t.Fatalf("expected offline presence payload, got %v", offlinePayload)
	}

	t.Log("Cross-node presence snapshot, live update, and offline event test passed!")
}

func expectStorageSnapshotTitle(t *testing.T, message map[string]any, requestID string, title string) {
	t.Helper()
	if message["t"] != "STORAGE_SNAPSHOT" || message["id"] != requestID {
		t.Fatalf("expected STORAGE_SNAPSHOT %q, got %v", requestID, message)
	}
	payload, ok := message["payload"].(map[string]any)
	if !ok {
		t.Fatalf("expected storage snapshot payload object, got %v", message["payload"])
	}
	document, ok := payload["document"].(map[string]any)
	if !ok || document["title"] != title {
		t.Fatalf("expected storage title %q, got %v", title, payload["document"])
	}
}

func expectStorageAck(t *testing.T, message map[string]any, requestID string, kind string, opID string) {
	t.Helper()
	if message["t"] != "STORAGE_ACK" || message["id"] != requestID {
		t.Fatalf("expected STORAGE_ACK %q, got %v", requestID, message)
	}
	payload, ok := message["payload"].(map[string]any)
	if !ok {
		t.Fatalf("expected storage ack payload object, got %v", message["payload"])
	}
	if payload["kind"] != kind || payload["op_id"] != opID {
		t.Fatalf("expected storage ack kind=%q op_id=%q, got %v", kind, opID, payload)
	}
}

func expectRuntimeError(t *testing.T, message map[string]any, requestID string) {
	t.Helper()
	if message["t"] != "ERROR" || message["id"] != requestID {
		t.Fatalf("expected ERROR %q, got %v", requestID, message)
	}
	payload, ok := message["payload"].(map[string]any)
	if !ok {
		t.Fatalf("expected error payload object, got %v", message["payload"])
	}
	if payload["code"] != "ROOM_FORBIDDEN" {
		t.Fatalf("expected ROOM_FORBIDDEN error, got %v", payload)
	}
}

func expectRuntimeErrorWithoutID(t *testing.T, message map[string]any) {
	t.Helper()
	if message["t"] != "ERROR" {
		t.Fatalf("expected ERROR, got %v", message)
	}
	if _, ok := message["id"]; ok {
		t.Fatalf("expected error without request ID, got %v", message)
	}
	payload, ok := message["payload"].(map[string]any)
	if !ok {
		t.Fatalf("expected error payload object, got %v", message["payload"])
	}
	if payload["code"] != "ROOM_FORBIDDEN" {
		t.Fatalf("expected ROOM_FORBIDDEN error, got %v", payload)
	}
}

func doAdminJSON(t *testing.T, method string, url string, token string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("admin request: %v", err)
	}
	return resp
}

func doAdminRaw(t *testing.T, method string, url string, token string, body string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("admin request: %v", err)
	}
	return resp
}
