package devserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"

	"github.com/openrtc/openrtc/server/internal/cluster"
	"github.com/openrtc/openrtc/server/internal/config"
	runtimeapp "github.com/openrtc/openrtc/server/internal/runtime"
)

func TestMainHelpReturnsSuccess(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Main([]string{"--help"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "Usage of openrtc dev") {
		t.Fatalf("expected help on stdout, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestMainRejectsUnexpectedArgs(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Main([]string{"extra"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "unexpected arguments: extra") {
		t.Fatalf("expected unexpected argument error, got %q", stderr.String())
	}
}

func TestHandleTokenAllowsLocalDevAnonymousAuth(t *testing.T) {
	privateKey = testPrivateKey(t)
	req := httptest.NewRequest(http.MethodGet, "/dev/token?pubkey=pk_localdev&tenant=acme&groups=editors,reviewers", nil)
	rec := httptest.NewRecorder()

	handleToken(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Token    string   `json:"token"`
		Kind     string   `json:"kind"`
		Username string   `json:"username"`
		Tenant   string   `json:"tenant"`
		Groups   []string `json:"groups"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if body.Token == "" {
		t.Fatalf("expected signed token")
	}
	if body.Kind != "client" {
		t.Fatalf("expected client token, got %q", body.Kind)
	}
	if !strings.HasPrefix(body.Username, "anon-") {
		t.Fatalf("expected anonymous username, got %q", body.Username)
	}
	if body.Tenant != "acme" {
		t.Fatalf("expected tenant acme, got %q", body.Tenant)
	}
	if len(body.Groups) != 2 || body.Groups[0] != "editors" || body.Groups[1] != "reviewers" {
		t.Fatalf("unexpected groups: %#v", body.Groups)
	}

	claims := parseTokenClaims(t, body.Token)
	if claims["iss"] != devIssuer {
		t.Fatalf("unexpected issuer: %v", claims["iss"])
	}
	if !hasAudience(claims["aud"], clientAudience) {
		t.Fatalf("expected audience %q, got %v", clientAudience, claims["aud"])
	}
	if claims["tenant"] != "acme" {
		t.Fatalf("unexpected tenant claim: %v", claims["tenant"])
	}
	if got := stringSliceClaim(claims["join"]); len(got) != 1 || got[0] != "*" {
		t.Fatalf("expected wildcard join grant, got %#v", got)
	}
	if got := stringSliceClaim(claims["presence"]); len(got) != 1 || got[0] != "*" {
		t.Fatalf("expected wildcard presence grant, got %#v", got)
	}
}

func TestHandleTokenRejectsUnknownPubkey(t *testing.T) {
	privateKey = testPrivateKey(t)
	req := httptest.NewRequest(http.MethodGet, "/dev/token?pubkey=pk_bad", nil)
	rec := httptest.NewRecorder()

	handleToken(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", rec.Code)
	}
}

func TestHandleTokenIssuesAdminScope(t *testing.T) {
	privateKey = testPrivateKey(t)
	req := httptest.NewRequest(http.MethodGet, "/dev/token?username=ops&kind=admin&scope=rooms:*", nil)
	rec := httptest.NewRecorder()

	handleToken(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Token string `json:"token"`
		Kind  string `json:"kind"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if body.Kind != "admin" {
		t.Fatalf("expected admin token, got %q", body.Kind)
	}
	claims := parseTokenClaims(t, body.Token)
	if !hasAudience(claims["aud"], adminAudience) {
		t.Fatalf("expected audience %q, got %v", adminAudience, claims["aud"])
	}
	if claims["scope"] != "rooms:*" {
		t.Fatalf("expected custom scope, got %v", claims["scope"])
	}
	if _, ok := claims["join"]; ok {
		t.Fatalf("admin token should not include client join grants")
	}
}

func TestDevConfigBuildsClusterRuntimeConfig(t *testing.T) {
	cfg, err := devConfig("dev-node", "127.0.0.1", 18080, "/ws", "http://127.0.0.1:3000", "redis://localhost:6379/0", "http://127.0.0.1:3000/jwks")
	if err != nil {
		t.Fatalf("devConfig: %v", err)
	}
	if cfg.Mode != config.ModeCluster {
		t.Fatalf("expected cluster mode, got %s", cfg.Mode)
	}
	if cfg.NodeID != "dev-node" || cfg.Server.Host != "127.0.0.1" || cfg.Server.Port != 18080 || cfg.Server.WSPath != "/ws" {
		t.Fatalf("unexpected server config: %+v node=%s", cfg.Server, cfg.NodeID)
	}
	if len(cfg.Server.AllowedOrigins) != 1 || cfg.Server.AllowedOrigins[0] != "http://127.0.0.1:3000" {
		t.Fatalf("unexpected origins: %#v", cfg.Server.AllowedOrigins)
	}
	if cfg.Redis == nil || cfg.Redis.URL != "redis://localhost:6379/0" || cfg.Redis.ChannelPrefix != "room:" {
		t.Fatalf("unexpected redis config: %+v", cfg.Redis)
	}
	if cfg.Auth.Issuer != devIssuer || cfg.Auth.Audience != clientAudience || cfg.Auth.JWKSURL != "http://127.0.0.1:3000/jwks" {
		t.Fatalf("unexpected auth config: %+v", cfg.Auth)
	}
	if cfg.AdminAuth == nil || cfg.AdminAuth.Issuer != devIssuer || cfg.AdminAuth.Audience != adminAudience {
		t.Fatalf("unexpected admin auth config: %+v", cfg.AdminAuth)
	}
	if cfg.Tenant.EnforcePrefix {
		t.Fatalf("expected tenant prefix enforcement disabled for local dev")
	}
}

func TestHandleSocketsReportsRuntimeSnapshot(t *testing.T) {
	service := &runtimeapp.Service{}
	handler := handleSockets(func() *runtimeapp.Service { return service })

	req := httptest.NewRequest(http.MethodGet, "/dev/sockets", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body runtimeapp.DevConnectionsSnapshot
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode sockets response: %v", err)
	}
	if body.ActiveSockets != 0 || len(body.Connections) != 0 || len(body.YJSConnections) != 0 {
		t.Fatalf("expected empty socket snapshot, got %+v", body)
	}

	rec = httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodPost, "/dev/sockets", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected POST 405, got %d", rec.Code)
	}

	handler = handleSockets(func() *runtimeapp.Service { return nil })
	rec = httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/dev/sockets", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected missing runtime 503, got %d", rec.Code)
	}
}

func TestHandleStorageReportsDurableAndRuntimeSnapshots(t *testing.T) {
	service := &runtimeapp.Service{}
	handler := handleStorage(&fakeDevStorageStore{document: json.RawMessage(`{"title":"Durable"}`)}, func() *runtimeapp.Service {
		return service
	})

	req := httptest.NewRequest(http.MethodGet, "/dev/storage?room=tenant-a:room-1", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body devStorageSnapshot
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode storage response: %v", err)
	}
	if body.Room != "tenant-a:room-1" || !body.Durable.Found || string(body.Durable.Document) != `{"title":"Durable"}` {
		t.Fatalf("unexpected durable storage snapshot: %+v", body)
	}
	if body.Runtime == nil || body.Runtime.Room != "tenant-a:room-1" || body.Runtime.Found {
		t.Fatalf("unexpected runtime storage snapshot: %+v", body.Runtime)
	}

	rec = httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/dev/storage", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing room 400, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodPost, "/dev/storage?room=tenant-a:room-1", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected POST 405, got %d", rec.Code)
	}
}

func TestHandleStorageReportsMissingDurableSnapshot(t *testing.T) {
	handler := handleStorage(&fakeDevStorageStore{err: cluster.ErrStorageNotFound}, func() *runtimeapp.Service {
		return nil
	})

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/dev/storage?room=tenant-a:missing", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected missing durable storage to return 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body devStorageSnapshot
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode storage response: %v", err)
	}
	if body.Room != "tenant-a:missing" || body.Durable.Found || body.Runtime != nil {
		t.Fatalf("unexpected missing durable storage snapshot: %+v", body)
	}
}

func TestHandleEventsReportsPublishedEventLog(t *testing.T) {
	handler := handleEvents(&fakeDevStorageStore{
		events: []cluster.PublishedEvent{
			{Room: "tenant-a:room-1", Event: "before", Payload: json.RawMessage(`{"n":1}`), Sequence: 1, OriginNode: "node-a"},
			{Room: "tenant-a:room-1", Event: "after", Payload: json.RawMessage(`{"n":2}`), Sequence: 2, OriginNode: "node-a"},
			{Room: "tenant-a:room-2", Event: "other-room", Payload: json.RawMessage(`{"n":3}`), Sequence: 3, OriginNode: "node-a"},
		},
	})

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/dev/events?room=tenant-a:room-1&after_seq=1&limit=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body devEventsSnapshot
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode events response: %v", err)
	}
	if body.Room != "tenant-a:room-1" || body.AfterSequence != 1 || body.Limit != 1 {
		t.Fatalf("unexpected event snapshot metadata: %+v", body)
	}
	if len(body.Events) != 1 || body.Events[0].Event != "after" || body.Events[0].Sequence != 2 {
		t.Fatalf("unexpected events: %+v", body.Events)
	}

	rec = httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/dev/events", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing room 400, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/dev/events?room=tenant-a:room-1&after_seq=-1", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid after_seq 400, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/dev/events?room=tenant-a:room-1&limit=1001", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid limit 400, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodPost, "/dev/events?room=tenant-a:room-1", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected POST 405, got %d", rec.Code)
	}
}

func TestHandleEventsReportsStoreErrors(t *testing.T) {
	handler := handleEvents(&fakeDevStorageStore{eventsErr: context.Canceled})

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/dev/events?room=tenant-a:room-1", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected store failure 500, got %d", rec.Code)
	}
}

func TestFilterSocketSnapshotByRoom(t *testing.T) {
	snapshot := runtimeapp.DevConnectionsSnapshot{
		NodeID:          "node-a",
		ActiveRoomCount: 3,
		Connections: []runtimeapp.DevConnectionSnapshot{
			{ConnectionID: "conn-a", Rooms: []string{"room-a", "room-b"}},
			{ConnectionID: "conn-b", Rooms: []string{"room-c"}},
		},
		YJSConnections: []runtimeapp.DevYJSConnectionSnapshot{
			{ConnectionID: "yjs-a", Room: "room-b"},
			{ConnectionID: "yjs-b", Room: "room-c"},
		},
	}

	filtered := filterSocketSnapshot(snapshot, "room-b")
	if filtered.NodeID != "node-a" || filtered.ActiveRoomCount != 3 {
		t.Fatalf("unexpected filtered metadata: %+v", filtered)
	}
	if filtered.ActiveSockets != 2 {
		t.Fatalf("expected two sockets after filter, got %+v", filtered)
	}
	if len(filtered.Connections) != 1 || filtered.Connections[0].ConnectionID != "conn-a" {
		t.Fatalf("unexpected filtered connections: %+v", filtered.Connections)
	}
	if len(filtered.YJSConnections) != 1 || filtered.YJSConnections[0].ConnectionID != "yjs-a" {
		t.Fatalf("unexpected filtered yjs connections: %+v", filtered.YJSConnections)
	}
}

func testPrivateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate private key: %v", err)
	}
	return key
}

func parseTokenClaims(t *testing.T, tokenString string) jwt.MapClaims {
	t.Helper()
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		return &privateKey.PublicKey, nil
	})
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	if !token.Valid {
		t.Fatalf("expected valid token")
	}
	return claims
}

func stringSliceClaim(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if value, ok := item.(string); ok {
			out = append(out, value)
		}
	}
	return out
}

type fakeDevStorageStore struct {
	document  json.RawMessage
	err       error
	events    []cluster.PublishedEvent
	eventsErr error
}

func (s *fakeDevStorageStore) GetStorage(context.Context, string) (json.RawMessage, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.document == nil {
		return nil, cluster.ErrStorageNotFound
	}
	return append(json.RawMessage(nil), s.document...), nil
}

func (s *fakeDevStorageStore) ListPublishedEvents(_ context.Context, room string, afterSequence uint64, limit int) (cluster.PublishedEventList, error) {
	if s.eventsErr != nil {
		return cluster.PublishedEventList{}, s.eventsErr
	}
	events := make([]cluster.PublishedEvent, 0, len(s.events))
	for _, event := range s.events {
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

func hasAudience(value any, audience string) bool {
	switch typed := value.(type) {
	case string:
		return typed == audience
	case []any:
		for _, item := range typed {
			if item == audience {
				return true
			}
		}
	case []string:
		for _, item := range typed {
			if item == audience {
				return true
			}
		}
	}
	return false
}
