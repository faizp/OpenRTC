package devserver

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"

	"github.com/openrtc/openrtc/server/internal/admin"
	"github.com/openrtc/openrtc/server/internal/cluster"
	"github.com/openrtc/openrtc/server/internal/config"
	runtimeapp "github.com/openrtc/openrtc/server/internal/runtime"
)

const (
	devIssuer        = "openrtc-dev"
	clientAudience   = "openrtc-clients"
	adminAudience    = "openrtc-admin"
	localPublicKey   = "pk_localdev"
	defaultSeedRooms = "demo:room-1,demo:canvas-1"
	devEventsLimit   = 100
	devEventsMax     = 1000
	devStorageMemory = "memory"
	devStorageRedis  = "redis"
)

var privateKey *rsa.PrivateKey

type options struct {
	host        string
	appPort     int
	runtimePort int
	adminPort   int
	redisURL    string
	storage     string
	staticDir   string
	seedRooms   []string
}

type devStoreHandle struct {
	store    cluster.Store
	redisURL string
	closeFn  func() error
}

type managedService struct {
	name    string
	addr    string
	startFn func() (http.Handler, func() error, error)
	logger  *log.Logger

	mu         sync.Mutex
	server     *http.Server
	closeFn    func() error
	generation uint64
	startedAt  time.Time
}

type seedStore interface {
	CreateRoom(ctx context.Context, room cluster.RoomRecord) (cluster.RoomRecord, error)
	GetStorage(ctx context.Context, room string) (json.RawMessage, error)
	SetStorage(ctx context.Context, room string, document json.RawMessage) (json.RawMessage, error)
}

type devStatusStore interface {
	Healthy(ctx context.Context) error
	GetRoom(ctx context.Context, room string) (cluster.RoomRecord, error)
	GetStorage(ctx context.Context, room string) (json.RawMessage, error)
}

type storageGetter interface {
	GetStorage(ctx context.Context, room string) (json.RawMessage, error)
}

type yjsDocumentLoader interface {
	LoadYJSDocument(ctx context.Context, room string) (cluster.YJSDocument, error)
}

type publishedEventLister interface {
	ListPublishedEvents(ctx context.Context, room string, afterSequence uint64, limit int) (cluster.PublishedEventList, error)
}

type devStorageSnapshot struct {
	Room    string                         `json:"room"`
	Durable devStorageDocumentSnapshot     `json:"durable"`
	Runtime *runtimeapp.DevStorageSnapshot `json:"runtime,omitempty"`
}

type devYJSSnapshot struct {
	Room    string                             `json:"room"`
	Durable devYJSDocumentSnapshot             `json:"durable"`
	Runtime *runtimeapp.DevYJSDocumentSnapshot `json:"runtime,omitempty"`
}

type devStorageDocumentSnapshot struct {
	Found    bool            `json:"found"`
	Sequence uint64          `json:"sequence,omitempty"`
	Document json.RawMessage `json:"document,omitempty"`
}

type devYJSDocumentSnapshot struct {
	Found              bool     `json:"found"`
	SnapshotFound      bool     `json:"snapshot_found"`
	SnapshotBytes      int      `json:"snapshot_bytes"`
	SnapshotHash       string   `json:"snapshot_hash,omitempty"`
	SnapshotCheckpoint int64    `json:"snapshot_checkpoint"`
	UpdateCount        int      `json:"update_count"`
	UpdateBytes        int      `json:"update_bytes"`
	UpdateSequences    []int64  `json:"update_sequences,omitempty"`
	UpdateKinds        []string `json:"update_kinds,omitempty"`
}

type devEventsSnapshot struct {
	Room          string                   `json:"room"`
	AfterSequence uint64                   `json:"after_seq"`
	Limit         int                      `json:"limit"`
	Events        []cluster.PublishedEvent `json:"events"`
}

type devClientConfigSnapshot struct {
	PublicKey        string   `json:"publicKey"`
	TokenURL         string   `json:"tokenURL"`
	JWKSURL          string   `json:"jwksURL"`
	WSURL            string   `json:"wsURL"`
	YJSURL           string   `json:"yjsURL"`
	AdminURL         string   `json:"adminURL"`
	AdminProxyURL    string   `json:"adminProxyURL"`
	RuntimeURL       string   `json:"runtimeURL"`
	RuntimeProxyURL  string   `json:"runtimeProxyURL"`
	StatusURL        string   `json:"statusURL"`
	ConnectionsURL   string   `json:"connectionsURL"`
	SocketsURL       string   `json:"socketsURL"`
	StorageURL       string   `json:"storageURL"`
	YJSInspectionURL string   `json:"yjsInspectionURL"`
	EventsURL        string   `json:"eventsURL"`
	CrashRuntimeURL  string   `json:"crashRuntimeURL"`
	CrashAdminURL    string   `json:"crashAdminURL"`
	SeedRooms        []string `json:"seedRooms"`
}

type devStatusSnapshot struct {
	Status         string                  `json:"status"`
	StorageBackend string                  `json:"storage_backend"`
	Redis          devDependencyStatus     `json:"redis"`
	Runtime        devManagedServiceStatus `json:"runtime"`
	Admin          devManagedServiceStatus `json:"admin"`
	SeedRooms      []devSeedRoomStatus     `json:"seed_rooms"`
	Endpoints      devEndpointSnapshot     `json:"endpoints"`
}

type devDependencyStatus struct {
	Healthy bool   `json:"healthy"`
	Error   string `json:"error,omitempty"`
}

type devManagedServiceStatus struct {
	Running    bool   `json:"running"`
	URL        string `json:"url"`
	Health     string `json:"healthz"`
	Ready      string `json:"readyz"`
	Generation uint64 `json:"generation"`
	StartedAt  string `json:"started_at,omitempty"`
}

type devRestartSnapshot struct {
	Status        string                  `json:"status"`
	Service       string                  `json:"service"`
	ServiceStatus devManagedServiceStatus `json:"service_status"`
}

type devSeedRoomStatus struct {
	Room         string `json:"room"`
	Exists       bool   `json:"exists"`
	StorageFound bool   `json:"storage_found"`
	Error        string `json:"error,omitempty"`
}

type devEndpointSnapshot struct {
	App          string `json:"app"`
	Config       string `json:"config"`
	JWKS         string `json:"jwks"`
	Token        string `json:"token"`
	RuntimeWS    string `json:"runtime_ws"`
	RuntimeYJS   string `json:"runtime_yjs"`
	RuntimeProxy string `json:"runtime_proxy"`
	AdminAPI     string `json:"admin_api"`
	AdminProxy   string `json:"admin_proxy"`
	Connections  string `json:"connections"`
	Sockets      string `json:"sockets"`
	Storage      string `json:"storage"`
	YJS          string `json:"yjs"`
	Events       string `json:"events"`
	CrashRuntime string `json:"crash_runtime"`
	CrashAdmin   string `json:"crash_admin"`
}

func Main(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "probe" {
		return probeMain(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "token" {
		return tokenMain(args[1:], stdout, stderr)
	}

	output := stderr
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			output = stdout
			break
		}
	}
	opts, err := parseOptions(args, output)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}
	if err := run(context.Background(), opts); err != nil {
		_, _ = fmt.Fprintf(stderr, "openrtc dev: %v\n", err)
		return 1
	}
	return 0
}

func parseOptions(args []string, output io.Writer) (options, error) {
	var rawSeedRooms string
	var rawStorage string
	opts := options{}
	flags := flag.NewFlagSet("openrtc dev", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&opts.host, "host", envOr("OPENRTC_DEV_HOST", "127.0.0.1"), "host for local dev servers")
	flags.IntVar(&opts.appPort, "app-port", envInt("OPENRTC_DEV_APP_PORT", 3000), "dev app and issuer port")
	flags.IntVar(&opts.runtimePort, "runtime-port", envInt("OPENRTC_DEV_RUNTIME_PORT", 8080), "runtime port")
	flags.IntVar(&opts.adminPort, "admin-port", envInt("OPENRTC_DEV_ADMIN_PORT", 8090), "admin API port")
	flags.StringVar(&opts.redisURL, "redis-url", envOr("OPENRTC_DEV_REDIS_URL", envOr("REDIS_URL", "redis://localhost:6379/0")), "Redis URL for local durable state")
	flags.StringVar(&rawStorage, "storage", envOr("OPENRTC_DEV_STORAGE", ""), "local storage backend: memory or redis")
	flags.StringVar(&opts.staticDir, "static-dir", envOr("OPENRTC_DEV_STATIC_DIR", ""), "static UI directory")
	flags.StringVar(&rawSeedRooms, "seed-rooms", envOr("OPENRTC_DEV_SEED_ROOMS", defaultSeedRooms), "comma-separated rooms to seed")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if flags.NArg() > 0 {
		return options{}, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}

	redisURLFlagSet := false
	flags.Visit(func(item *flag.Flag) {
		if item.Name == "redis-url" {
			redisURLFlagSet = true
		}
	})
	opts.storage = normalizeStorageBackend(rawStorage, redisURLFlagSet || envSet("OPENRTC_DEV_REDIS_URL") || envSet("REDIS_URL"))
	if opts.storage == "" {
		return options{}, fmt.Errorf("storage must be memory or redis")
	}
	opts.seedRooms = csvList(rawSeedRooms)
	if opts.staticDir == "" {
		opts.staticDir = findStaticDir()
	}
	return opts, nil
}

func run(ctx context.Context, opts options) error {
	var err error
	privateKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate RSA key: %w", err)
	}

	appURL := fmt.Sprintf("http://%s:%d", opts.host, opts.appPort)
	jwksURL := appURL + "/jwks"

	devStore, err := startDevStore(ctx, opts)
	if err != nil {
		return err
	}
	defer devStore.close()
	store := devStore.store
	if err := seedRooms(ctx, store, opts.seedRooms); err != nil {
		return fmt.Errorf("seed rooms: %w", err)
	}

	var runtimeMu sync.RWMutex
	var runtimeService *runtimeapp.Service
	setRuntimeService := func(service *runtimeapp.Service) {
		runtimeMu.Lock()
		runtimeService = service
		runtimeMu.Unlock()
	}
	currentRuntimeService := func() *runtimeapp.Service {
		runtimeMu.RLock()
		defer runtimeMu.RUnlock()
		return runtimeService
	}

	runtimeSvc := &managedService{
		name:   "runtime",
		addr:   fmt.Sprintf("%s:%d", opts.host, opts.runtimePort),
		logger: log.New(os.Stdout, "openrtc-dev runtime ", log.LstdFlags),
		startFn: func() (http.Handler, func() error, error) {
			cfg, err := devConfig("openrtc-dev-runtime", opts.host, opts.runtimePort, "/ws", appURL, devStore.redisURL, jwksURL)
			if err != nil {
				return nil, nil, err
			}
			service, err := runtimeapp.NewService(cfg, log.New(os.Stdout, "openrtc-runtime ", log.LstdFlags))
			if err != nil {
				return nil, nil, err
			}
			setRuntimeService(service)
			closeFn := func() error {
				setRuntimeService(nil)
				return service.Close()
			}
			return service.Handler(), closeFn, nil
		},
	}
	adminSvc := &managedService{
		name:   "admin",
		addr:   fmt.Sprintf("%s:%d", opts.host, opts.adminPort),
		logger: log.New(os.Stdout, "openrtc-dev admin ", log.LstdFlags),
		startFn: func() (http.Handler, func() error, error) {
			cfg, err := devConfig("openrtc-dev-admin", opts.host, opts.adminPort, "/ws", appURL, devStore.redisURL, jwksURL)
			if err != nil {
				return nil, nil, err
			}
			service, err := admin.NewService(cfg, log.New(os.Stdout, "openrtc-admin ", log.LstdFlags))
			if err != nil {
				return nil, nil, err
			}
			return service.Handler(), service.Close, nil
		},
	}

	if err := runtimeSvc.start(); err != nil {
		return err
	}
	defer runtimeSvc.stop(context.Background())
	if err := adminSvc.start(); err != nil {
		return err
	}
	defer adminSvc.stop(context.Background())

	mux := http.NewServeMux()
	tokenHandler := handleTokenWithOptions(opts)
	mux.HandleFunc("/jwks", handleJWKS)
	mux.HandleFunc("/token", tokenHandler)
	mux.HandleFunc("/dev/token", tokenHandler)
	mux.HandleFunc("/dev/config", handleDevConfig(opts))
	mux.HandleFunc("/config", handleDevConfig(opts))
	mux.HandleFunc("/dev/status", handleStatus(opts, store, runtimeSvc, adminSvc))
	mux.HandleFunc("/dev/connections", handleConnections(store))
	mux.HandleFunc("/dev/sockets", handleSockets(currentRuntimeService))
	mux.HandleFunc("/dev/storage", handleStorage(store, currentRuntimeService))
	mux.HandleFunc("/dev/yjs", handleYJSDocument(store, currentRuntimeService))
	mux.HandleFunc("/dev/events", handleEvents(store))
	mux.HandleFunc("/dev/crash/runtime", handleRestart(runtimeSvc))
	mux.HandleFunc("/dev/crash/admin", handleRestart(adminSvc))
	mux.Handle("/admin/", reverseProxy("/admin", fmt.Sprintf("http://%s:%d", opts.host, opts.adminPort)))
	mux.Handle("/admin", reverseProxy("/admin", fmt.Sprintf("http://%s:%d", opts.host, opts.adminPort)))
	mux.Handle("/runtime/", reverseProxy("/runtime", fmt.Sprintf("http://%s:%d", opts.host, opts.runtimePort)))
	mux.Handle("/runtime", reverseProxy("/runtime", fmt.Sprintf("http://%s:%d", opts.host, opts.runtimePort)))
	if opts.staticDir != "" {
		mux.Handle("/", noStore(http.FileServer(http.Dir(opts.staticDir))))
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = fmt.Fprintln(w, "OpenRTC dev server is running")
		})
	}

	server := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", opts.host, opts.appPort),
		Handler: mux,
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()

	log.Printf("OpenRTC dev:     http://%s:%d", opts.host, opts.appPort)
	log.Printf("Runtime WS:      ws://%s:%d/ws", opts.host, opts.runtimePort)
	log.Printf("Runtime Yjs:     ws://%s:%d/yjs/{room}", opts.host, opts.runtimePort)
	log.Printf("Admin API:       http://%s:%d", opts.host, opts.adminPort)
	log.Printf("Storage backend: %s", opts.storage)
	if opts.storage == devStorageMemory {
		log.Printf("Embedded Redis:  %s", devStore.redisURL)
	}
	log.Printf("Local public key: %s", localPublicKey)
	log.Printf("Seed rooms:      %s", strings.Join(opts.seedRooms, ","))
	log.Printf("Dev sockets:     http://%s:%d/dev/sockets", opts.host, opts.appPort)
	log.Printf("Dev storage:     http://%s:%d/dev/storage?room=%s", opts.host, opts.appPort, firstSeedRoom(opts.seedRooms))
	log.Printf("Dev Yjs:         http://%s:%d/dev/yjs?room=%s", opts.host, opts.appPort, firstSeedRoom(opts.seedRooms))

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("dev server exited: %w", err)
	}
	return nil
}

func (s *managedService) start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		return nil
	}
	handler, closeFn, err := s.startFn()
	if err != nil {
		return fmt.Errorf("start %s service: %w", s.name, err)
	}
	server := &http.Server{Addr: s.addr, Handler: handler}
	s.server = server
	s.closeFn = closeFn
	s.generation++
	s.startedAt = time.Now().UTC()
	go func() {
		s.logger.Printf("%s server starting: %s", s.name, s.addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Printf("%s server exited: %v", s.name, err)
		}
	}()
	return nil
}

func (s *managedService) stop(ctx context.Context) {
	s.mu.Lock()
	server := s.server
	closeFn := s.closeFn
	s.server = nil
	s.closeFn = nil
	s.startedAt = time.Time{}
	s.mu.Unlock()

	if server != nil {
		_ = server.Shutdown(ctx)
	}
	if closeFn != nil {
		_ = closeFn()
	}
}

func (s *managedService) restart() error {
	s.stop(context.Background())
	return s.start()
}

func (s *managedService) running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.server != nil
}

func startDevStore(ctx context.Context, opts options) (*devStoreHandle, error) {
	switch opts.storage {
	case devStorageMemory:
		embedded, err := miniredis.Run()
		if err != nil {
			return nil, fmt.Errorf("start embedded Redis: %w", err)
		}
		redisURL := "redis://" + embedded.Addr()
		store, err := cluster.NewRedisStore(redisURL, "room:")
		if err != nil {
			embedded.Close()
			return nil, fmt.Errorf("connect embedded Redis: %w", err)
		}
		handle := &devStoreHandle{
			store:    store,
			redisURL: redisURL,
			closeFn: func() error {
				err := store.Close()
				embedded.Close()
				return err
			},
		}
		if err := store.Healthy(ctx); err != nil {
			_ = handle.close()
			return nil, fmt.Errorf("embedded Redis is not healthy: %w", err)
		}
		return handle, nil
	case devStorageRedis:
		store, err := cluster.NewRedisStore(opts.redisURL, "room:")
		if err != nil {
			return nil, fmt.Errorf("connect redis: %w", err)
		}
		handle := &devStoreHandle{
			store:    store,
			redisURL: opts.redisURL,
			closeFn:  store.Close,
		}
		if err := store.Healthy(ctx); err != nil {
			_ = handle.close()
			return nil, fmt.Errorf("redis is required for openrtc dev at %s: %w", opts.redisURL, err)
		}
		return handle, nil
	default:
		return nil, fmt.Errorf("storage must be memory or redis")
	}
}

func (s *devStoreHandle) close() error {
	if s == nil || s.closeFn == nil {
		return nil
	}
	return s.closeFn()
}

func devConfig(nodeID string, host string, port int, wsPath string, allowedOrigin string, redisURL string, jwksURL string) (config.RuntimeConfig, error) {
	return config.LoadFromMap(map[string]string{
		"OPENRTC_MODE":                  string(config.ModeCluster),
		"OPENRTC_NODE_ID":               nodeID,
		"OPENRTC_SERVER_HOST":           host,
		"OPENRTC_SERVER_PORT":           strconv.Itoa(port),
		"OPENRTC_WS_PATH":               wsPath,
		"OPENRTC_ALLOWED_ORIGINS":       allowedOrigin,
		"OPENRTC_REDIS_URL":             redisURL,
		"OPENRTC_REDIS_CHANNEL_PREFIX":  "room:",
		"OPENRTC_AUTH_ISSUER":           devIssuer,
		"OPENRTC_AUTH_AUDIENCE":         clientAudience,
		"OPENRTC_AUTH_JWKS_URL":         jwksURL,
		"OPENRTC_TENANT_ENFORCE_PREFIX": "false",
		"OPENRTC_ADMIN_AUTH_ISSUER":     devIssuer,
		"OPENRTC_ADMIN_AUTH_AUDIENCE":   adminAudience,
		"OPENRTC_ADMIN_AUTH_JWKS_URL":   jwksURL,
	})
}

func seedRooms(ctx context.Context, store seedStore, rooms []string) error {
	for _, room := range rooms {
		if room == "" {
			continue
		}
		_, err := store.CreateRoom(ctx, cluster.RoomRecord{
			ID:              room,
			Metadata:        json.RawMessage(fmt.Sprintf(`{"name":%q,"dev":true}`, room)),
			DefaultAccesses: []string{cluster.PermissionRoomWrite},
		})
		if errors.Is(err, cluster.ErrRoomAlreadyExists) {
			err = nil
		}
		if err != nil {
			return err
		}
		if err := seedRoomStorage(ctx, store, room); err != nil {
			return err
		}
	}
	return nil
}

func seedRoomStorage(ctx context.Context, store seedStore, room string) error {
	if _, err := store.GetStorage(ctx, room); err == nil {
		return nil
	} else if !errors.Is(err, cluster.ErrStorageNotFound) {
		return err
	}
	_, err := store.SetStorage(ctx, room, defaultSeedStorage(room))
	return err
}

func defaultSeedStorage(room string) json.RawMessage {
	document := map[string]any{
		"liveblocksType": "LiveObject",
		"data": map[string]any{
			"title": "OpenRTC dev room",
			"room":  room,
			"items": map[string]any{
				"liveblocksType": "LiveList",
				"data": []any{
					map[string]any{"id": "intro", "text": "Seeded typed storage"},
				},
			},
			"props": map[string]any{
				"liveblocksType": "LiveMap",
				"data": map[string]any{
					"dev": true,
				},
			},
		},
	}
	raw, err := json.Marshal(document)
	if err != nil {
		return json.RawMessage(`{"liveblocksType":"LiveObject","data":{}}`)
	}
	return raw
}

func handleJWKS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"keys": []map[string]any{
			{
				"kty": "RSA",
				"kid": "dev-key",
				"n":   base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.PublicKey.E)).Bytes()),
			},
		},
	})
}

func handleToken(w http.ResponseWriter, r *http.Request) {
	issueToken(w, r, nil)
}

func handleTokenWithOptions(opts options) http.HandlerFunc {
	config := devClientConfig(opts)
	return func(w http.ResponseWriter, r *http.Request) {
		issueToken(w, r, &config)
	}
}

func issueToken(w http.ResponseWriter, r *http.Request, config *devClientConfigSnapshot) {
	username := strings.TrimSpace(r.URL.Query().Get("username"))
	pubkey := strings.TrimSpace(r.URL.Query().Get("pubkey"))
	if username == "" && pubkey == localPublicKey {
		username = "anon-" + randomSuffix(8)
	}
	if pubkey != "" && pubkey != localPublicKey {
		http.Error(w, "pubkey must be pk_localdev", http.StatusForbidden)
		return
	}
	if username == "" {
		http.Error(w, "username required unless pubkey=pk_localdev is used", http.StatusBadRequest)
		return
	}

	kind := firstNonEmpty(r.URL.Query().Get("kind"), "client")
	if kind != "client" && kind != "admin" {
		http.Error(w, "kind must be client or admin", http.StatusBadRequest)
		return
	}

	tenant := firstNonEmpty(r.URL.Query().Get("tenant"), "demo")
	groups := csvList(r.URL.Query().Get("groups"))
	expiresAt := time.Now().Add(24 * time.Hour)
	claims := jwt.MapClaims{
		"iss":    devIssuer,
		"aud":    clientAudience,
		"sub":    username,
		"exp":    expiresAt.Unix(),
		"tenant": tenant,
	}
	if len(groups) > 0 {
		claims["groups"] = groups
		claims["groupIds"] = groups
	}
	if kind == "admin" {
		claims["aud"] = adminAudience
		claims["scope"] = firstNonEmpty(r.URL.Query().Get("scope"), "publish:* presence:* rooms:* storage:* comments:* notifications:*")
	} else if r.URL.Query().Get("access") != "grants" {
		claims["join"] = []string{"*"}
		claims["publish"] = []string{"*"}
		claims["presence"] = []string{"*"}
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "dev-key"
	signed, err := token.SignedString(privateKey)
	if err != nil {
		http.Error(w, "token signing failed", http.StatusInternalServerError)
		return
	}

	response := map[string]any{
		"token":     signed,
		"kind":      kind,
		"username":  username,
		"tenant":    tenant,
		"groups":    groups,
		"expiresAt": expiresAt.Format(time.RFC3339),
	}
	if room := devTokenRoom(r, config); room != "" {
		response["room"] = room
	}
	if config != nil {
		response["config"] = config
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func devTokenRoom(r *http.Request, config *devClientConfigSnapshot) string {
	if room := strings.TrimSpace(r.URL.Query().Get("room")); room != "" {
		return room
	}
	if config != nil {
		return firstSeedRoom(config.SeedRooms)
	}
	return ""
}

func handleDevConfig(opts options) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(devClientConfig(opts))
	}
}

func devClientConfig(opts options) devClientConfigSnapshot {
	appURL := fmt.Sprintf("http://%s:%d", opts.host, opts.appPort)
	firstRoom := firstSeedRoom(opts.seedRooms)
	return devClientConfigSnapshot{
		PublicKey:        localPublicKey,
		TokenURL:         "/dev/token",
		JWKSURL:          appURL + "/jwks",
		WSURL:            fmt.Sprintf("ws://%s:%d/ws", opts.host, opts.runtimePort),
		YJSURL:           fmt.Sprintf("ws://%s:%d/yjs", opts.host, opts.runtimePort),
		AdminURL:         fmt.Sprintf("http://%s:%d", opts.host, opts.adminPort),
		AdminProxyURL:    "/admin",
		RuntimeURL:       fmt.Sprintf("http://%s:%d", opts.host, opts.runtimePort),
		RuntimeProxyURL:  "/runtime",
		StatusURL:        appURL + "/dev/status",
		ConnectionsURL:   appURL + "/dev/connections?room=" + firstRoom,
		SocketsURL:       appURL + "/dev/sockets",
		StorageURL:       appURL + "/dev/storage?room=" + firstRoom,
		YJSInspectionURL: appURL + "/dev/yjs?room=" + firstRoom,
		EventsURL:        appURL + "/dev/events?room=" + firstRoom,
		CrashRuntimeURL:  appURL + "/dev/crash/runtime",
		CrashAdminURL:    appURL + "/dev/crash/admin",
		SeedRooms:        opts.seedRooms,
	}
}

func handleStatus(opts options, store devStatusStore, runtimeSvc *managedService, adminSvc *managedService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method must be GET", http.StatusMethodNotAllowed)
			return
		}

		snapshot := devStatusSnapshot{
			Status:         "ok",
			StorageBackend: opts.storage,
			Redis:          devDependencyStatus{Healthy: true},
			Runtime:        managedServiceStatus(runtimeSvc, devHTTPURL(opts.host, opts.runtimePort, "")),
			Admin:          managedServiceStatus(adminSvc, devHTTPURL(opts.host, opts.adminPort, "")),
			SeedRooms:      seedRoomStatuses(r.Context(), store, opts.seedRooms),
			Endpoints:      devEndpoints(opts),
		}
		if err := store.Healthy(r.Context()); err != nil {
			snapshot.Redis.Healthy = false
			snapshot.Redis.Error = err.Error()
		}

		w.Header().Set("Content-Type", "application/json")
		if !devStatusHealthy(snapshot) {
			snapshot.Status = "degraded"
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(snapshot)
	}
}

func managedServiceStatus(service *managedService, baseURL string) devManagedServiceStatus {
	status := devManagedServiceStatus{
		URL:    baseURL,
		Health: baseURL + "/healthz",
		Ready:  baseURL + "/readyz",
	}
	if service == nil {
		return status
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	status.Running = service.server != nil
	status.Generation = service.generation
	if !service.startedAt.IsZero() {
		status.StartedAt = service.startedAt.Format(time.RFC3339Nano)
	}
	return status
}

func seedRoomStatuses(ctx context.Context, store devStatusStore, rooms []string) []devSeedRoomStatus {
	statuses := make([]devSeedRoomStatus, 0, len(rooms))
	for _, room := range rooms {
		if room == "" {
			continue
		}
		status := devSeedRoomStatus{Room: room}
		if _, err := store.GetRoom(ctx, room); err != nil {
			status.Error = err.Error()
		} else {
			status.Exists = true
		}
		if _, err := store.GetStorage(ctx, room); err != nil {
			if !errors.Is(err, cluster.ErrStorageNotFound) {
				status.Error = joinErrors(status.Error, err.Error())
			}
		} else {
			status.StorageFound = true
		}
		statuses = append(statuses, status)
	}
	return statuses
}

func devStatusHealthy(snapshot devStatusSnapshot) bool {
	if !snapshot.Redis.Healthy || !snapshot.Runtime.Running || !snapshot.Admin.Running {
		return false
	}
	for _, room := range snapshot.SeedRooms {
		if !room.Exists || !room.StorageFound || room.Error != "" {
			return false
		}
	}
	return true
}

func joinErrors(existing string, next string) string {
	if existing == "" {
		return next
	}
	if next == "" {
		return existing
	}
	return existing + "; " + next
}

func devEndpoints(opts options) devEndpointSnapshot {
	firstRoom := firstSeedRoom(opts.seedRooms)
	appURL := devHTTPURL(opts.host, opts.appPort, "")
	adminURL := devHTTPURL(opts.host, opts.adminPort, "")
	return devEndpointSnapshot{
		App:          appURL,
		Config:       appURL + "/dev/config",
		JWKS:         appURL + "/jwks",
		Token:        appURL + "/dev/token?pubkey=" + localPublicKey,
		RuntimeWS:    devWSURL(opts.host, opts.runtimePort, "/ws"),
		RuntimeYJS:   devWSURL(opts.host, opts.runtimePort, "/yjs/"+firstRoom),
		RuntimeProxy: appURL + "/runtime",
		AdminAPI:     adminURL,
		AdminProxy:   appURL + "/admin",
		Connections:  appURL + "/dev/connections?room=" + firstRoom,
		Sockets:      appURL + "/dev/sockets",
		Storage:      appURL + "/dev/storage?room=" + firstRoom,
		YJS:          appURL + "/dev/yjs?room=" + firstRoom,
		Events:       appURL + "/dev/events?room=" + firstRoom,
		CrashRuntime: appURL + "/dev/crash/runtime",
		CrashAdmin:   appURL + "/dev/crash/admin",
	}
}

func devHTTPURL(host string, port int, path string) string {
	return fmt.Sprintf("http://%s:%d%s", host, port, path)
}

func devWSURL(host string, port int, path string) string {
	return fmt.Sprintf("ws://%s:%d%s", host, port, path)
}

func handleConnections(store cluster.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		room := strings.TrimSpace(r.URL.Query().Get("room"))
		if room == "" {
			http.Error(w, "room query parameter is required", http.StatusBadRequest)
			return
		}
		users, err := store.ActiveUsers(r.Context(), room)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"room":        room,
			"connections": users,
		})
	}
}

func handleSockets(currentRuntimeService func() *runtimeapp.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method must be GET", http.StatusMethodNotAllowed)
			return
		}
		service := currentRuntimeService()
		if service == nil {
			http.Error(w, "runtime service is not running", http.StatusServiceUnavailable)
			return
		}

		snapshot := service.DevConnectionsSnapshot()
		if room := strings.TrimSpace(r.URL.Query().Get("room")); room != "" {
			snapshot = filterSocketSnapshot(snapshot, room)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(snapshot)
	}
}

func handleStorage(store storageGetter, currentRuntimeService func() *runtimeapp.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method must be GET", http.StatusMethodNotAllowed)
			return
		}
		room := strings.TrimSpace(r.URL.Query().Get("room"))
		if room == "" {
			http.Error(w, "room query parameter is required", http.StatusBadRequest)
			return
		}

		snapshot := devStorageSnapshot{Room: room}
		document, sequence, err := devStorageDocument(r.Context(), store, room)
		if err != nil {
			if !errors.Is(err, cluster.ErrStorageNotFound) {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			snapshot.Durable = devStorageDocumentSnapshot{
				Found:    true,
				Sequence: sequence,
				Document: document,
			}
		}

		if service := currentRuntimeService(); service != nil {
			runtimeSnapshot := service.DevStorageSnapshot(room)
			snapshot.Runtime = &runtimeSnapshot
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(snapshot)
	}
}

func devStorageDocument(ctx context.Context, store storageGetter, room string) (json.RawMessage, uint64, error) {
	if sequenced, ok := store.(cluster.SequencedStorageReader); ok {
		return sequenced.GetStorageWithSequence(ctx, room)
	}
	document, err := store.GetStorage(ctx, room)
	return document, 0, err
}

func handleYJSDocument(store yjsDocumentLoader, currentRuntimeService func() *runtimeapp.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method must be GET", http.StatusMethodNotAllowed)
			return
		}
		room := strings.TrimSpace(r.URL.Query().Get("room"))
		if room == "" {
			http.Error(w, "room query parameter is required", http.StatusBadRequest)
			return
		}

		snapshot := devYJSSnapshot{Room: room}
		document, err := store.LoadYJSDocument(r.Context(), room)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		snapshot.Durable = summarizeDevYJSDocument(document)

		if service := currentRuntimeService(); service != nil {
			runtimeSnapshot := service.DevYJSDocumentSnapshot(room)
			snapshot.Runtime = &runtimeSnapshot
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(snapshot)
	}
}

func summarizeDevYJSDocument(document cluster.YJSDocument) devYJSDocumentSnapshot {
	snapshot := devYJSDocumentSnapshot{
		SnapshotFound:      len(document.Snapshot) > 0,
		SnapshotBytes:      len(document.Snapshot),
		SnapshotHash:       document.SnapshotHash,
		SnapshotCheckpoint: document.SnapshotCheckpoint,
		UpdateCount:        len(document.Updates),
		UpdateSequences:    append([]int64(nil), document.UpdateSequences...),
	}
	if snapshot.SnapshotFound || snapshot.UpdateCount > 0 || snapshot.SnapshotCheckpoint > 0 {
		snapshot.Found = true
	}
	if snapshot.UpdateCount > 0 {
		snapshot.UpdateKinds = make([]string, 0, snapshot.UpdateCount)
		for index, update := range document.Updates {
			snapshot.UpdateBytes += len(update)
			kind := cluster.YJSEventUpdate
			if index < len(document.UpdateKinds) {
				kind = document.UpdateKinds[index]
			}
			snapshot.UpdateKinds = append(snapshot.UpdateKinds, devYJSEventKindLabel(kind))
		}
	}
	return snapshot
}

func devYJSEventKindLabel(kind cluster.YJSEventKind) string {
	switch kind {
	case cluster.YJSEventUpdate:
		return "update"
	case cluster.YJSEventSnapshot:
		return "snapshot"
	case cluster.YJSEventStateVectorRequest:
		return "state-vector"
	case cluster.YJSEventStateVectorDiff:
		return "state-vector-diff"
	case cluster.YJSEventSubdocUpdate:
		return "subdoc-update"
	case cluster.YJSEventSubdocStateVector:
		return "subdoc-state-vector"
	case cluster.YJSEventSubdocDiff:
		return "subdoc-state-vector-diff"
	default:
		return fmt.Sprintf("unknown-%d", kind)
	}
}

func handleEvents(store publishedEventLister) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method must be GET", http.StatusMethodNotAllowed)
			return
		}
		room := strings.TrimSpace(r.URL.Query().Get("room"))
		if room == "" {
			http.Error(w, "room query parameter is required", http.StatusBadRequest)
			return
		}
		afterSequence, err := parseOptionalUint(r.URL.Query().Get("after_seq"))
		if err != nil {
			http.Error(w, "after_seq must be a non-negative integer", http.StatusBadRequest)
			return
		}
		limit, err := parseOptionalLimit(r.URL.Query().Get("limit"), devEventsLimit, devEventsMax)
		if err != nil {
			http.Error(w, fmt.Sprintf("limit must be an integer between 1 and %d", devEventsMax), http.StatusBadRequest)
			return
		}
		list, err := store.ListPublishedEvents(r.Context(), room, afterSequence, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		events := list.Events
		if events == nil {
			events = []cluster.PublishedEvent{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(devEventsSnapshot{
			Room:          room,
			AfterSequence: afterSequence,
			Limit:         limit,
			Events:        events,
		})
	}
}

func filterSocketSnapshot(snapshot runtimeapp.DevConnectionsSnapshot, room string) runtimeapp.DevConnectionsSnapshot {
	filtered := runtimeapp.DevConnectionsSnapshot{
		NodeID:         snapshot.NodeID,
		Connections:    make([]runtimeapp.DevConnectionSnapshot, 0),
		YJSConnections: make([]runtimeapp.DevYJSConnectionSnapshot, 0),
	}
	for _, conn := range snapshot.Connections {
		for _, joinedRoom := range conn.Rooms {
			if joinedRoom == room {
				filtered.Connections = append(filtered.Connections, conn)
				break
			}
		}
	}
	for _, conn := range snapshot.YJSConnections {
		if conn.Room == room {
			filtered.YJSConnections = append(filtered.YJSConnections, conn)
		}
	}
	filtered.ActiveSockets = len(filtered.Connections) + len(filtered.YJSConnections)
	filtered.ActiveRoomCount = snapshot.ActiveRoomCount
	return filtered
}

func firstSeedRoom(seedRooms []string) string {
	if len(seedRooms) == 0 {
		return "demo:room-1"
	}
	return seedRooms[0]
}

func handleRestart(service *managedService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method must be POST", http.StatusMethodNotAllowed)
			return
		}
		if err := service.restart(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(devRestartSnapshot{
			Status:        "restarted",
			Service:       service.name,
			ServiceStatus: managedServiceStatus(service, "http://"+service.addr),
		})
	}
}

func reverseProxy(prefix string, target string) http.Handler {
	targetURL, err := url.Parse(target)
	if err != nil {
		log.Fatalf("parse proxy target %s: %v", target, err)
	}
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = targetURL.Scheme
		req.URL.Host = targetURL.Host
		req.URL.Path = strings.TrimPrefix(req.URL.Path, prefix)
		if req.URL.Path == "" {
			req.URL.Path = "/"
		}
		req.Host = targetURL.Host
	}
	return proxy
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func findStaticDir() string {
	candidates := []string{
		filepath.Join("reference-app", "static"),
		filepath.Join("..", "reference-app", "static"),
		filepath.Join("..", "..", "reference-app", "static"),
	}
	for _, candidate := range candidates {
		if fileExists(filepath.Join(candidate, "index.html")) {
			return candidate
		}
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func envOr(key string, defaultValue string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return defaultValue
}

func envSet(key string) bool {
	return strings.TrimSpace(os.Getenv(key)) != ""
}

func envInt(key string, defaultValue int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return defaultValue
	}
	return parsed
}

func parseOptionalUint(raw string) (uint64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	return strconv.ParseUint(raw, 10, 64)
}

func parseOptionalLimit(raw string, defaultValue int, maxValue int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 1 || parsed > maxValue {
		return 0, fmt.Errorf("invalid limit")
	}
	return parsed, nil
}

func normalizeStorageBackend(raw string, redisConfigured bool) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		if redisConfigured {
			return devStorageRedis
		}
		return devStorageMemory
	}
	switch raw {
	case devStorageMemory, "mem", "embedded":
		return devStorageMemory
	case devStorageRedis:
		return devStorageRedis
	default:
		return ""
	}
}

func csvList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func randomSuffix(size int) string {
	if size <= 0 {
		return ""
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
