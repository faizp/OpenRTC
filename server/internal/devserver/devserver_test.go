package devserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

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

func TestMainProbeHelpReturnsSuccess(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Main([]string{"probe", "--help"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "Usage of openrtc dev probe") {
		t.Fatalf("expected probe help on stdout, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestParseOptionsDefaultsToMemoryStorage(t *testing.T) {
	clearDevStorageEnv(t)
	var output bytes.Buffer

	opts, err := parseOptions(nil, &output)

	if err != nil {
		t.Fatalf("parse options: %v", err)
	}
	if opts.storage != devStorageMemory {
		t.Fatalf("expected memory storage by default, got %q", opts.storage)
	}
}

func TestParseOptionsUsesRedisStorageWhenConfigured(t *testing.T) {
	clearDevStorageEnv(t)
	var output bytes.Buffer

	opts, err := parseOptions([]string{"--redis-url", "redis://redis.local:6379/2"}, &output)

	if err != nil {
		t.Fatalf("parse options: %v", err)
	}
	if opts.storage != devStorageRedis {
		t.Fatalf("expected redis storage when redis-url is set, got %q", opts.storage)
	}
	if opts.redisURL != "redis://redis.local:6379/2" {
		t.Fatalf("unexpected redis URL: %q", opts.redisURL)
	}
}

func TestParseOptionsRejectsUnknownStorage(t *testing.T) {
	clearDevStorageEnv(t)
	var output bytes.Buffer

	_, err := parseOptions([]string{"--storage", "sqlite"}, &output)

	if err == nil || !strings.Contains(err.Error(), "storage must be memory or redis") {
		t.Fatalf("expected storage validation error, got %v", err)
	}
}

func TestParseProbeOptionsRejectsBadRestart(t *testing.T) {
	var output bytes.Buffer

	_, err := parseProbeOptions([]string{"--restart", "everything"}, &output)

	if err == nil || !strings.Contains(err.Error(), "restart must be none, runtime, admin, or both") {
		t.Fatalf("expected restart validation error, got %v", err)
	}
}

func TestRunProbeReportsHealthyDevStack(t *testing.T) {
	server := newProbeTestServer(t, probeTestServerOptions{healthy: true})
	defer server.Close()

	result, err := runProbe(context.Background(), probeOptions{
		baseURL:           server.URL,
		room:              "demo:room-1",
		restart:           probeRestartBoth,
		timeout:           time.Second,
		afterSequence:     4,
		limit:             5,
		expectSeedRoom:    true,
		expectSeedStorage: true,
	})

	if err != nil {
		t.Fatalf("run probe: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected healthy probe, got %+v", result.Checks)
	}
	if result.Room != "demo:room-1" {
		t.Fatalf("unexpected room: %q", result.Room)
	}
	if got := probeCheckNames(result.Checks); strings.Join(got, ",") != "config,seed-room,status,connections,sockets,storage,yjs,events,restart-runtime,restart-admin" {
		t.Fatalf("unexpected checks: %v", got)
	}
	if result.Snapshots.Events == nil || result.Snapshots.Events.AfterSequence != 4 || result.Snapshots.Events.Limit != 5 {
		t.Fatalf("expected event probe options to be forwarded, got %+v", result.Snapshots.Events)
	}
	if result.Snapshots.RuntimeRestart == nil || result.Snapshots.RuntimeRestart.Service != "runtime" {
		t.Fatalf("expected runtime restart snapshot, got %+v", result.Snapshots.RuntimeRestart)
	}
	if result.Snapshots.AdminRestart == nil || result.Snapshots.AdminRestart.Service != "admin" {
		t.Fatalf("expected admin restart snapshot, got %+v", result.Snapshots.AdminRestart)
	}
}

func TestProbeMainWritesDegradedJSONAndFails(t *testing.T) {
	server := newProbeTestServer(t, probeTestServerOptions{healthy: false})
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := probeMain([]string{"--base-url", server.URL, "--json", "--timeout", "1s"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr for structured degraded report, got %q", stderr.String())
	}
	var result devProbeResult
	if err := json.NewDecoder(&stdout).Decode(&result); err != nil {
		t.Fatalf("decode probe JSON: %v\n%s", err, stdout.String())
	}
	if result.OK {
		t.Fatalf("expected degraded probe result")
	}
	if probeCheckByName(result.Checks, "status").OK {
		t.Fatalf("expected status check to fail: %+v", result.Checks)
	}
	if probeCheckByName(result.Checks, "storage").OK {
		t.Fatalf("expected storage check to fail: %+v", result.Checks)
	}
	if !probeCheckByName(result.Checks, "events").OK {
		t.Fatalf("expected events check to pass: %+v", result.Checks)
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

func TestHandleTokenWithOptionsReturnsIntegrationConfig(t *testing.T) {
	privateKey = testPrivateKey(t)
	opts := options{
		host:        "127.0.0.1",
		appPort:     3000,
		runtimePort: 8080,
		adminPort:   8090,
		seedRooms:   []string{"demo:room-1", "demo:canvas-1"},
	}
	req := httptest.NewRequest(http.MethodGet, "/dev/token?pubkey=pk_localdev&room=demo:canvas-1", nil)
	rec := httptest.NewRecorder()

	handleTokenWithOptions(opts)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Token  string                  `json:"token"`
		Room   string                  `json:"room"`
		Config devClientConfigSnapshot `json:"config"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if body.Token == "" {
		t.Fatalf("expected signed token")
	}
	if body.Room != "demo:canvas-1" {
		t.Fatalf("expected requested room, got %q", body.Room)
	}
	if body.Config.PublicKey != localPublicKey ||
		body.Config.TokenURL != "/dev/token" ||
		body.Config.WSURL != "ws://127.0.0.1:8080/ws" ||
		body.Config.YJSURL != "ws://127.0.0.1:8080/yjs" ||
		body.Config.AdminProxyURL != "/admin" ||
		body.Config.RuntimeProxyURL != "/runtime" ||
		body.Config.StatusURL != "http://127.0.0.1:3000/dev/status" ||
		body.Config.ConnectionsURL != "http://127.0.0.1:3000/dev/connections?room=demo:room-1" ||
		body.Config.SocketsURL != "http://127.0.0.1:3000/dev/sockets" ||
		body.Config.StorageURL != "http://127.0.0.1:3000/dev/storage?room=demo:room-1" ||
		body.Config.YJSInspectionURL != "http://127.0.0.1:3000/dev/yjs?room=demo:room-1" ||
		body.Config.EventsURL != "http://127.0.0.1:3000/dev/events?room=demo:room-1" ||
		body.Config.CrashRuntimeURL != "http://127.0.0.1:3000/dev/crash/runtime" ||
		body.Config.CrashAdminURL != "http://127.0.0.1:3000/dev/crash/admin" {
		t.Fatalf("unexpected integration config: %+v", body.Config)
	}
	if len(body.Config.SeedRooms) != 2 || body.Config.SeedRooms[0] != "demo:room-1" || body.Config.SeedRooms[1] != "demo:canvas-1" {
		t.Fatalf("unexpected seed rooms: %#v", body.Config.SeedRooms)
	}
}

func TestHandleDevConfigReturnsIntegrationConfig(t *testing.T) {
	opts := options{
		host:        "127.0.0.1",
		appPort:     3000,
		runtimePort: 8080,
		adminPort:   8090,
		seedRooms:   []string{"demo:room-1", "demo:canvas-1"},
	}
	rec := httptest.NewRecorder()

	handleDevConfig(opts)(rec, httptest.NewRequest(http.MethodGet, "/dev/config", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body devClientConfigSnapshot
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode config response: %v", err)
	}
	if body.PublicKey != localPublicKey ||
		body.TokenURL != "/dev/token" ||
		body.WSURL != "ws://127.0.0.1:8080/ws" ||
		body.AdminProxyURL != "/admin" ||
		body.StatusURL != "http://127.0.0.1:3000/dev/status" ||
		body.StorageURL != "http://127.0.0.1:3000/dev/storage?room=demo:room-1" {
		t.Fatalf("unexpected integration config: %+v", body)
	}
	if len(body.SeedRooms) != 2 || body.SeedRooms[0] != "demo:room-1" || body.SeedRooms[1] != "demo:canvas-1" {
		t.Fatalf("unexpected seed rooms: %#v", body.SeedRooms)
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

func TestStartDevStoreUsesEmbeddedRedis(t *testing.T) {
	handle, err := startDevStore(context.Background(), options{storage: devStorageMemory})
	if err != nil {
		t.Fatalf("start dev store: %v", err)
	}
	defer handle.close()

	if !strings.HasPrefix(handle.redisURL, "redis://") || handle.redisURL == "redis://localhost:6379/0" {
		t.Fatalf("expected embedded redis URL, got %q", handle.redisURL)
	}
	if err := handle.store.Healthy(context.Background()); err != nil {
		t.Fatalf("expected embedded redis to be healthy: %v", err)
	}
	if err := seedRooms(context.Background(), handle.store, []string{"demo:room-1"}); err != nil {
		t.Fatalf("seed rooms: %v", err)
	}
	if _, err := handle.store.GetStorage(context.Background(), "demo:room-1"); err != nil {
		t.Fatalf("expected seeded storage from embedded redis: %v", err)
	}
}

func TestSeedRoomsCreatesRoomsAndDefaultStorage(t *testing.T) {
	store := newFakeSeedStore()

	if err := seedRooms(context.Background(), store, []string{"demo:room-1", "", "demo:room-2"}); err != nil {
		t.Fatalf("seed rooms: %v", err)
	}
	if len(store.rooms) != 2 {
		t.Fatalf("expected two seeded rooms, got %+v", store.rooms)
	}
	for _, room := range []string{"demo:room-1", "demo:room-2"} {
		document := store.storage[room]
		if len(document) == 0 {
			t.Fatalf("expected seeded storage for %s", room)
		}
		if err := cluster.ValidateStorageDocument(document); err != nil {
			t.Fatalf("seeded storage should be valid for %s: %v", room, err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(document, &decoded); err != nil {
			t.Fatalf("decode seeded storage: %v", err)
		}
		data, ok := decoded["data"].(map[string]any)
		if !ok || data["room"] != room {
			t.Fatalf("expected seeded room data for %s, got %+v", room, decoded)
		}
	}
}

func TestSeedRoomsPreservesExistingStorage(t *testing.T) {
	store := newFakeSeedStore()
	store.rooms["demo:room-1"] = cluster.RoomRecord{ID: "demo:room-1"}
	store.storage["demo:room-1"] = json.RawMessage(`{"title":"Existing"}`)

	if err := seedRooms(context.Background(), store, []string{"demo:room-1"}); err != nil {
		t.Fatalf("seed rooms: %v", err)
	}
	if string(store.storage["demo:room-1"]) != `{"title":"Existing"}` {
		t.Fatalf("expected existing storage to be preserved, got %s", store.storage["demo:room-1"])
	}
}

func TestSeedRoomsReportsStorageErrors(t *testing.T) {
	store := newFakeSeedStore()
	store.getStorageErr = context.Canceled

	if err := seedRooms(context.Background(), store, []string{"demo:room-1"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected storage error, got %v", err)
	}
}

func TestHandleStatusReportsHealthyDevStack(t *testing.T) {
	store := newFakeSeedStore()
	if err := seedRooms(context.Background(), store, []string{"demo:room-1"}); err != nil {
		t.Fatalf("seed rooms: %v", err)
	}
	opts := options{
		host:        "127.0.0.1",
		appPort:     3000,
		runtimePort: 8080,
		adminPort:   8090,
		storage:     devStorageMemory,
		seedRooms:   []string{"demo:room-1"},
	}
	handler := handleStatus(opts, store, runningManagedService("runtime"), runningManagedService("admin"))

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/dev/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body devStatusSnapshot
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if body.Status != "ok" || body.StorageBackend != devStorageMemory || !body.Redis.Healthy || !body.Runtime.Running || !body.Admin.Running {
		t.Fatalf("unexpected healthy status: %+v", body)
	}
	if body.Runtime.Generation == 0 || body.Runtime.StartedAt == "" {
		t.Fatalf("expected runtime generation metadata: %+v", body.Runtime)
	}
	if len(body.SeedRooms) != 1 || body.SeedRooms[0].Room != "demo:room-1" || !body.SeedRooms[0].Exists || !body.SeedRooms[0].StorageFound {
		t.Fatalf("unexpected seed room status: %+v", body.SeedRooms)
	}
	if body.Endpoints.Token != "http://127.0.0.1:3000/dev/token?pubkey=pk_localdev" ||
		body.Endpoints.RuntimeWS != "ws://127.0.0.1:8080/ws" ||
		body.Endpoints.Storage != "http://127.0.0.1:3000/dev/storage?room=demo:room-1" ||
		body.Endpoints.YJS != "http://127.0.0.1:3000/dev/yjs?room=demo:room-1" {
		t.Fatalf("unexpected endpoints: %+v", body.Endpoints)
	}
}

func TestHandleStatusReportsDegradedDevStack(t *testing.T) {
	store := newFakeSeedStore()
	store.healthyErr = context.Canceled
	store.rooms["demo:room-1"] = cluster.RoomRecord{ID: "demo:room-1"}
	opts := options{
		host:        "127.0.0.1",
		appPort:     3000,
		runtimePort: 8080,
		adminPort:   8090,
		seedRooms:   []string{"demo:room-1", "demo:missing"},
	}
	handler := handleStatus(opts, store, &managedService{name: "runtime"}, nil)

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/dev/status", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d: %s", rec.Code, rec.Body.String())
	}
	var body devStatusSnapshot
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if body.Status != "degraded" || body.Redis.Healthy || body.Runtime.Running || body.Admin.Running {
		t.Fatalf("unexpected degraded status: %+v", body)
	}
	if body.Redis.Error == "" {
		t.Fatalf("expected redis error in degraded status")
	}
	if len(body.SeedRooms) != 2 || !body.SeedRooms[0].Exists || body.SeedRooms[0].StorageFound || body.SeedRooms[1].Exists {
		t.Fatalf("unexpected degraded seed room status: %+v", body.SeedRooms)
	}

	rec = httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodPost, "/dev/status", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected POST 405, got %d", rec.Code)
	}
}

func TestHandleRestartReportsServiceStatus(t *testing.T) {
	starts := 0
	service := &managedService{
		name:   "runtime",
		addr:   "127.0.0.1:0",
		logger: log.New(io.Discard, "", 0),
		startFn: func() (http.Handler, func() error, error) {
			starts++
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}), func() error { return nil }, nil
		},
	}
	defer service.stop(context.Background())
	handler := handleRestart(service)

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodPost, "/dev/crash/runtime", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if starts != 1 {
		t.Fatalf("expected one start, got %d", starts)
	}
	var body devRestartSnapshot
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode restart response: %v", err)
	}
	if body.Status != "restarted" || body.Service != "runtime" {
		t.Fatalf("unexpected restart response: %+v", body)
	}
	if !body.ServiceStatus.Running || body.ServiceStatus.Generation != 1 || body.ServiceStatus.StartedAt == "" {
		t.Fatalf("expected running generation status: %+v", body.ServiceStatus)
	}
	if body.ServiceStatus.URL != "http://127.0.0.1:0" || body.ServiceStatus.Ready != "http://127.0.0.1:0/readyz" {
		t.Fatalf("unexpected service URLs: %+v", body.ServiceStatus)
	}

	rec = httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/dev/crash/runtime", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected GET 405, got %d", rec.Code)
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

func TestHandleYJSDocumentReportsDurableAndRuntimeSnapshots(t *testing.T) {
	service := &runtimeapp.Service{}
	document := cluster.YJSDocument{
		Snapshot:           []byte("snapshot-1"),
		SnapshotHash:       cluster.YJSSnapshotHash([]byte("snapshot-1")),
		SnapshotCheckpoint: 7,
		Updates:            [][]byte{[]byte("update-8"), []byte("subdoc-update")},
		UpdateSequences:    []int64{8, 9},
		UpdateKinds:        []cluster.YJSEventKind{cluster.YJSEventUpdate, cluster.YJSEventSubdocUpdate},
	}
	handler := handleYJSDocument(&fakeDevStorageStore{yjsDocument: document}, func() *runtimeapp.Service {
		return service
	})

	req := httptest.NewRequest(http.MethodGet, "/dev/yjs?room=tenant-a:doc-1", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body devYJSSnapshot
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode yjs response: %v", err)
	}
	if body.Room != "tenant-a:doc-1" || !body.Durable.Found || !body.Durable.SnapshotFound {
		t.Fatalf("unexpected durable yjs snapshot metadata: %+v", body)
	}
	if body.Durable.SnapshotBytes != len("snapshot-1") || body.Durable.SnapshotHash != cluster.YJSSnapshotHash([]byte("snapshot-1")) || body.Durable.SnapshotCheckpoint != 7 {
		t.Fatalf("unexpected durable yjs snapshot details: %+v", body.Durable)
	}
	if body.Durable.UpdateCount != 2 || body.Durable.UpdateBytes != len("update-8")+len("subdoc-update") {
		t.Fatalf("unexpected durable yjs update counts: %+v", body.Durable)
	}
	if len(body.Durable.UpdateSequences) != 2 || body.Durable.UpdateSequences[0] != 8 || body.Durable.UpdateSequences[1] != 9 {
		t.Fatalf("unexpected durable yjs update sequences: %+v", body.Durable.UpdateSequences)
	}
	if len(body.Durable.UpdateKinds) != 2 || body.Durable.UpdateKinds[0] != "update" || body.Durable.UpdateKinds[1] != "subdoc-update" {
		t.Fatalf("unexpected durable yjs update kinds: %+v", body.Durable.UpdateKinds)
	}
	if body.Runtime == nil || body.Runtime.Room != "tenant-a:doc-1" || body.Runtime.Found {
		t.Fatalf("unexpected runtime yjs snapshot: %+v", body.Runtime)
	}

	rec = httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/dev/yjs", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing room 400, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodPost, "/dev/yjs?room=tenant-a:doc-1", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected POST 405, got %d", rec.Code)
	}
}

func TestHandleYJSDocumentReportsStoreErrors(t *testing.T) {
	handler := handleYJSDocument(&fakeDevStorageStore{yjsErr: context.Canceled}, func() *runtimeapp.Service {
		return nil
	})

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/dev/yjs?room=tenant-a:doc-1", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected store failure 500, got %d", rec.Code)
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

type fakeSeedStore struct {
	rooms         map[string]cluster.RoomRecord
	storage       map[string]json.RawMessage
	healthyErr    error
	createRoomErr error
	getRoomErr    error
	getStorageErr error
	setStorageErr error
}

func newFakeSeedStore() *fakeSeedStore {
	return &fakeSeedStore{
		rooms:   make(map[string]cluster.RoomRecord),
		storage: make(map[string]json.RawMessage),
	}
}

func (s *fakeSeedStore) CreateRoom(_ context.Context, room cluster.RoomRecord) (cluster.RoomRecord, error) {
	if s.createRoomErr != nil {
		return cluster.RoomRecord{}, s.createRoomErr
	}
	if _, ok := s.rooms[room.ID]; ok {
		return cluster.RoomRecord{}, cluster.ErrRoomAlreadyExists
	}
	s.rooms[room.ID] = room
	return room, nil
}

func (s *fakeSeedStore) Healthy(context.Context) error {
	return s.healthyErr
}

func (s *fakeSeedStore) GetRoom(_ context.Context, room string) (cluster.RoomRecord, error) {
	if s.getRoomErr != nil {
		return cluster.RoomRecord{}, s.getRoomErr
	}
	record, ok := s.rooms[room]
	if !ok {
		return cluster.RoomRecord{}, cluster.ErrRoomNotFound
	}
	return record, nil
}

func (s *fakeSeedStore) GetStorage(_ context.Context, room string) (json.RawMessage, error) {
	if s.getStorageErr != nil {
		return nil, s.getStorageErr
	}
	if s.storage == nil {
		return nil, cluster.ErrStorageNotFound
	}
	document, ok := s.storage[room]
	if ok {
		return append(json.RawMessage(nil), document...), nil
	}
	return nil, cluster.ErrStorageNotFound
}

func (s *fakeSeedStore) SetStorage(_ context.Context, room string, document json.RawMessage) (json.RawMessage, error) {
	if s.setStorageErr != nil {
		return nil, s.setStorageErr
	}
	if s.storage == nil {
		s.storage = make(map[string]json.RawMessage)
	}
	s.storage[room] = append(json.RawMessage(nil), document...)
	return document, nil
}

func runningManagedService(name string) *managedService {
	return &managedService{
		name:       name,
		server:     &http.Server{},
		generation: 1,
		startedAt:  time.Unix(1, 0).UTC(),
	}
}

func clearDevStorageEnv(t *testing.T) {
	t.Helper()
	t.Setenv("OPENRTC_DEV_STORAGE", "")
	t.Setenv("OPENRTC_DEV_REDIS_URL", "")
	t.Setenv("REDIS_URL", "")
}

type fakeDevStorageStore struct {
	document    json.RawMessage
	err         error
	yjsDocument cluster.YJSDocument
	yjsErr      error
	events      []cluster.PublishedEvent
	eventsErr   error
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

func (s *fakeDevStorageStore) LoadYJSDocument(context.Context, string) (cluster.YJSDocument, error) {
	if s.yjsErr != nil {
		return cluster.YJSDocument{}, s.yjsErr
	}
	document := s.yjsDocument
	document.Snapshot = append([]byte(nil), document.Snapshot...)
	document.Updates = make([][]byte, 0, len(s.yjsDocument.Updates))
	for _, update := range s.yjsDocument.Updates {
		document.Updates = append(document.Updates, append([]byte(nil), update...))
	}
	document.UpdateSequences = append([]int64(nil), s.yjsDocument.UpdateSequences...)
	document.UpdateKinds = append([]cluster.YJSEventKind(nil), s.yjsDocument.UpdateKinds...)
	return document, nil
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

type probeTestServerOptions struct {
	healthy bool
}

type probeTestServer struct {
	URL string
}

func (s *probeTestServer) Close() {}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newProbeTestServer(t *testing.T, opts probeTestServerOptions) *probeTestServer {
	t.Helper()
	mux := http.NewServeMux()
	baseURL := "http://openrtc-dev.test"
	config := func() devClientConfigSnapshot {
		return devClientConfigSnapshot{
			PublicKey:        localPublicKey,
			TokenURL:         "/dev/token",
			JWKSURL:          baseURL + "/jwks",
			WSURL:            "ws://127.0.0.1:8080/ws",
			YJSURL:           "ws://127.0.0.1:8080/yjs",
			AdminURL:         "http://127.0.0.1:8090",
			AdminProxyURL:    "/admin",
			RuntimeURL:       "http://127.0.0.1:8080",
			RuntimeProxyURL:  "/runtime",
			StatusURL:        baseURL + "/dev/status",
			ConnectionsURL:   baseURL + "/dev/connections?room=demo:room-1",
			SocketsURL:       baseURL + "/dev/sockets",
			StorageURL:       baseURL + "/dev/storage?room=demo:room-1",
			YJSInspectionURL: baseURL + "/dev/yjs?room=demo:room-1",
			EventsURL:        baseURL + "/dev/events?room=demo:room-1",
			CrashRuntimeURL:  baseURL + "/dev/crash/runtime",
			CrashAdminURL:    baseURL + "/dev/crash/admin",
			SeedRooms:        []string{"demo:room-1", "demo:canvas-1"},
		}
	}
	writeJSON := func(w http.ResponseWriter, status int, value any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(value)
	}

	mux.HandleFunc("/dev/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method must be GET", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, config())
	})
	mux.HandleFunc("/dev/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method must be GET", http.StatusMethodNotAllowed)
			return
		}
		statusCode := http.StatusOK
		body := devStatusSnapshot{
			Status:         "ok",
			StorageBackend: devStorageMemory,
			Redis:          devDependencyStatus{Healthy: true},
			Runtime:        devManagedServiceStatus{Running: true, URL: "http://127.0.0.1:8080", Health: "http://127.0.0.1:8080/healthz", Ready: "http://127.0.0.1:8080/readyz", Generation: 2},
			Admin:          devManagedServiceStatus{Running: true, URL: "http://127.0.0.1:8090", Health: "http://127.0.0.1:8090/healthz", Ready: "http://127.0.0.1:8090/readyz", Generation: 1},
			SeedRooms:      []devSeedRoomStatus{{Room: "demo:room-1", Exists: true, StorageFound: true}},
			Endpoints:      devEndpointSnapshot{Sockets: baseURL + "/dev/sockets"},
		}
		if !opts.healthy {
			statusCode = http.StatusServiceUnavailable
			body.Status = "degraded"
			body.Redis = devDependencyStatus{Healthy: false, Error: "redis ping failed"}
			body.Runtime.Running = false
			body.SeedRooms = []devSeedRoomStatus{{Room: "demo:room-1", Exists: true, StorageFound: false}}
		}
		writeJSON(w, statusCode, body)
	})
	mux.HandleFunc("/dev/connections", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method must be GET", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"room":        r.URL.Query().Get("room"),
			"connections": []map[string]any{{"type": "json", "connection_id": "conn-1", "id": "ada", "tenant": "demo"}},
		})
	})
	mux.HandleFunc("/dev/sockets", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method must be GET", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"node_id":           "dev-runtime",
			"connections":       []map[string]any{{"connection_id": "conn-1", "subject": "ada", "tenant": "demo", "rooms": []string{"demo:room-1"}}},
			"yjs_connections":   []map[string]any{{"connection_id": "yjs-1", "subject": "ada", "tenant": "demo", "room": "demo:room-1"}},
			"active_sockets":    2,
			"active_room_count": 1,
		})
	})
	mux.HandleFunc("/dev/storage", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method must be GET", http.StatusMethodNotAllowed)
			return
		}
		found := opts.healthy
		var document json.RawMessage
		if found {
			document = json.RawMessage(`{"title":"OpenRTC dev room"}`)
		}
		writeJSON(w, http.StatusOK, devStorageSnapshot{
			Room:    r.URL.Query().Get("room"),
			Durable: devStorageDocumentSnapshot{Found: found, Document: document},
		})
	})
	mux.HandleFunc("/dev/yjs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method must be GET", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, devYJSSnapshot{
			Room: r.URL.Query().Get("room"),
			Durable: devYJSDocumentSnapshot{
				Found:              opts.healthy,
				SnapshotFound:      opts.healthy,
				SnapshotBytes:      10,
				SnapshotHash:       "fnv1a64:abc",
				SnapshotCheckpoint: 3,
				UpdateCount:        1,
				UpdateBytes:        10,
			},
		})
	})
	mux.HandleFunc("/dev/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method must be GET", http.StatusMethodNotAllowed)
			return
		}
		after, _ := strconv.ParseUint(r.URL.Query().Get("after_seq"), 10, 64)
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		writeJSON(w, http.StatusOK, devEventsSnapshot{
			Room:          r.URL.Query().Get("room"),
			AfterSequence: after,
			Limit:         limit,
			Events:        []cluster.PublishedEvent{{Room: "demo:room-1", Event: "dev.probe", Sequence: after + 1}},
		})
	})
	mux.HandleFunc("/dev/crash/runtime", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method must be POST", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, devRestartSnapshot{Status: "restarted", Service: "runtime", ServiceStatus: devManagedServiceStatus{Running: true, Generation: 3}})
	})
	mux.HandleFunc("/dev/crash/admin", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method must be POST", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, devRestartSnapshot{Status: "restarted", Service: "admin", ServiceStatus: devManagedServiceStatus{Running: true, Generation: 2}})
	})

	previousClient := probeHTTPClient
	probeHTTPClient = func() *http.Client {
		return &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
				return rec.Result(), nil
			}),
		}
	}
	t.Cleanup(func() {
		probeHTTPClient = previousClient
	})
	return &probeTestServer{URL: baseURL}
}

func probeCheckNames(checks []devProbeCheck) []string {
	names := make([]string, 0, len(checks))
	for _, check := range checks {
		names = append(names, check.Name)
	}
	return names
}

func probeCheckByName(checks []devProbeCheck, name string) devProbeCheck {
	for _, check := range checks {
		if check.Name == name {
			return check
		}
	}
	return devProbeCheck{}
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
