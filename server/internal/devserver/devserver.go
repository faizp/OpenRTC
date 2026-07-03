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
)

var privateKey *rsa.PrivateKey

type options struct {
	host        string
	appPort     int
	runtimePort int
	adminPort   int
	redisURL    string
	staticDir   string
	seedRooms   []string
}

type managedService struct {
	name    string
	addr    string
	startFn func() (http.Handler, func() error, error)
	logger  *log.Logger

	mu      sync.Mutex
	server  *http.Server
	closeFn func() error
}

type seedStore interface {
	CreateRoom(ctx context.Context, room cluster.RoomRecord) (cluster.RoomRecord, error)
	GetStorage(ctx context.Context, room string) (json.RawMessage, error)
	SetStorage(ctx context.Context, room string, document json.RawMessage) (json.RawMessage, error)
}

type storageGetter interface {
	GetStorage(ctx context.Context, room string) (json.RawMessage, error)
}

type publishedEventLister interface {
	ListPublishedEvents(ctx context.Context, room string, afterSequence uint64, limit int) (cluster.PublishedEventList, error)
}

type devStorageSnapshot struct {
	Room    string                         `json:"room"`
	Durable devStorageDocumentSnapshot     `json:"durable"`
	Runtime *runtimeapp.DevStorageSnapshot `json:"runtime,omitempty"`
}

type devStorageDocumentSnapshot struct {
	Found    bool            `json:"found"`
	Document json.RawMessage `json:"document,omitempty"`
}

type devEventsSnapshot struct {
	Room          string                   `json:"room"`
	AfterSequence uint64                   `json:"after_seq"`
	Limit         int                      `json:"limit"`
	Events        []cluster.PublishedEvent `json:"events"`
}

func Main(args []string, stdout io.Writer, stderr io.Writer) int {
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
	opts := options{}
	flags := flag.NewFlagSet("openrtc dev", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&opts.host, "host", envOr("OPENRTC_DEV_HOST", "127.0.0.1"), "host for local dev servers")
	flags.IntVar(&opts.appPort, "app-port", envInt("OPENRTC_DEV_APP_PORT", 3000), "dev app and issuer port")
	flags.IntVar(&opts.runtimePort, "runtime-port", envInt("OPENRTC_DEV_RUNTIME_PORT", 8080), "runtime port")
	flags.IntVar(&opts.adminPort, "admin-port", envInt("OPENRTC_DEV_ADMIN_PORT", 8090), "admin API port")
	flags.StringVar(&opts.redisURL, "redis-url", envOr("OPENRTC_DEV_REDIS_URL", envOr("REDIS_URL", "redis://localhost:6379/0")), "Redis URL for local durable state")
	flags.StringVar(&opts.staticDir, "static-dir", envOr("OPENRTC_DEV_STATIC_DIR", ""), "static UI directory")
	flags.StringVar(&rawSeedRooms, "seed-rooms", envOr("OPENRTC_DEV_SEED_ROOMS", defaultSeedRooms), "comma-separated rooms to seed")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if flags.NArg() > 0 {
		return options{}, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
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

	store, err := cluster.NewRedisStore(opts.redisURL, "room:")
	if err != nil {
		return fmt.Errorf("connect redis: %w", err)
	}
	defer store.Close()
	if err := store.Healthy(ctx); err != nil {
		return fmt.Errorf("redis is required for openrtc dev at %s: %w", opts.redisURL, err)
	}
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
			cfg, err := devConfig("openrtc-dev-runtime", opts.host, opts.runtimePort, "/ws", appURL, opts.redisURL, jwksURL)
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
			cfg, err := devConfig("openrtc-dev-admin", opts.host, opts.adminPort, "/ws", appURL, opts.redisURL, jwksURL)
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
	mux.HandleFunc("/jwks", handleJWKS)
	mux.HandleFunc("/token", handleToken)
	mux.HandleFunc("/dev/token", handleToken)
	mux.HandleFunc("/dev/config", handleDevConfig(opts))
	mux.HandleFunc("/config", handleDevConfig(opts))
	mux.HandleFunc("/dev/connections", handleConnections(store))
	mux.HandleFunc("/dev/sockets", handleSockets(currentRuntimeService))
	mux.HandleFunc("/dev/storage", handleStorage(store, currentRuntimeService))
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
	log.Printf("Local public key: %s", localPublicKey)
	log.Printf("Seed rooms:      %s", strings.Join(opts.seedRooms, ","))
	log.Printf("Dev sockets:     http://%s:%d/dev/sockets", opts.host, opts.appPort)
	log.Printf("Dev storage:     http://%s:%d/dev/storage?room=%s", opts.host, opts.appPort, firstSeedRoom(opts.seedRooms))

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

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"token":     signed,
		"kind":      kind,
		"username":  username,
		"tenant":    tenant,
		"groups":    groups,
		"expiresAt": expiresAt.Format(time.RFC3339),
	})
}

func handleDevConfig(opts options) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"publicKey":       localPublicKey,
			"tokenURL":        "/dev/token",
			"jwksURL":         fmt.Sprintf("http://%s:%d/jwks", opts.host, opts.appPort),
			"wsURL":           fmt.Sprintf("ws://%s:%d/ws", opts.host, opts.runtimePort),
			"yjsURL":          fmt.Sprintf("ws://%s:%d/yjs", opts.host, opts.runtimePort),
			"adminURL":        fmt.Sprintf("http://%s:%d", opts.host, opts.adminPort),
			"adminProxyURL":   "/admin",
			"runtimeURL":      fmt.Sprintf("http://%s:%d", opts.host, opts.runtimePort),
			"runtimeProxyURL": "/runtime",
			"seedRooms":       opts.seedRooms,
		})
	}
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
		document, err := store.GetStorage(r.Context(), room)
		if err != nil {
			if !errors.Is(err, cluster.ErrStorageNotFound) {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			snapshot.Durable = devStorageDocumentSnapshot{
				Found:    true,
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
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "restarted",
			"service": service.name,
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
