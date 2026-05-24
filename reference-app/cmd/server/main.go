package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var privateKey *rsa.PrivateKey

func main() {
	var err error
	privateKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatalf("generate RSA key: %v", err)
	}

	appPort := envOr("APP_PORT", "3000")
	runtimePort := envOr("RUNTIME_PORT", "8080")
	adminPort := envOr("ADMIN_PORT", "8090")
	redisURL := envOr("REDIS_URL", "redis://localhost:6379/0")
	appDir := findReferenceAppDir()
	repoDir := filepath.Clean(filepath.Join(appDir, ".."))

	jwksURL := fmt.Sprintf("http://localhost:%s/jwks", appPort)

	// Start OpenRTC runtime
	go startProcess("openrtc-runtime", map[string]string{
		"OPENRTC_MODE":                  "cluster",
		"OPENRTC_NODE_ID":               "ref-runtime",
		"OPENRTC_SERVER_HOST":           "0.0.0.0",
		"OPENRTC_SERVER_PORT":           runtimePort,
		"OPENRTC_WS_PATH":               "/ws",
		"OPENRTC_ALLOWED_ORIGINS":       fmt.Sprintf("http://localhost:%s", appPort),
		"OPENRTC_REDIS_URL":             redisURL,
		"OPENRTC_AUTH_ISSUER":           "openrtc-reference",
		"OPENRTC_AUTH_AUDIENCE":         "openrtc-clients",
		"OPENRTC_AUTH_JWKS_URL":         jwksURL,
		"OPENRTC_TENANT_ENFORCE_PREFIX": "false",
		"OPENRTC_ADMIN_AUTH_ISSUER":     "openrtc-reference",
		"OPENRTC_ADMIN_AUTH_AUDIENCE":   "openrtc-admin",
	}, repoDir)

	// Start OpenRTC admin
	go startProcess("openrtc-admin", map[string]string{
		"OPENRTC_MODE":                  "cluster",
		"OPENRTC_NODE_ID":               "ref-admin",
		"OPENRTC_SERVER_HOST":           "0.0.0.0",
		"OPENRTC_SERVER_PORT":           adminPort,
		"OPENRTC_ALLOWED_ORIGINS":       fmt.Sprintf("http://localhost:%s", appPort),
		"OPENRTC_REDIS_URL":             redisURL,
		"OPENRTC_AUTH_ISSUER":           "openrtc-reference",
		"OPENRTC_AUTH_AUDIENCE":         "openrtc-clients",
		"OPENRTC_AUTH_JWKS_URL":         jwksURL,
		"OPENRTC_TENANT_ENFORCE_PREFIX": "false",
		"OPENRTC_ADMIN_AUTH_ISSUER":     "openrtc-reference",
		"OPENRTC_ADMIN_AUTH_AUDIENCE":   "openrtc-admin",
	}, repoDir)

	adminProxy := reverseProxy("/admin", fmt.Sprintf("http://localhost:%s", adminPort))
	runtimeProxy := reverseProxy("/runtime", fmt.Sprintf("http://localhost:%s", runtimePort))

	mux := http.NewServeMux()
	mux.HandleFunc("/jwks", handleJWKS)
	mux.HandleFunc("/token", handleToken)
	mux.HandleFunc("/config", handleConfig(runtimePort, adminPort))
	mux.Handle("/admin/", adminProxy)
	mux.Handle("/admin", adminProxy)
	mux.Handle("/runtime/", runtimeProxy)
	mux.Handle("/runtime", runtimeProxy)
	mux.Handle("/", http.FileServer(http.Dir(filepath.Join(appDir, "static"))))

	log.Printf("Reference app: http://localhost:%s", appPort)
	log.Printf("Runtime WS:    ws://localhost:%s/ws", runtimePort)
	log.Printf("Admin API:     http://localhost:%s", adminPort)
	log.Fatal(http.ListenAndServe(":"+appPort, mux))
}

func handleJWKS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"keys": []map[string]any{
			{
				"kty": "RSA",
				"kid": "ref-key",
				"n":   base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.PublicKey.E)).Bytes()),
			},
		},
	})
}

func handleToken(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")
	if username == "" {
		http.Error(w, "username required", http.StatusBadRequest)
		return
	}

	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "client"
	}
	if kind != "client" && kind != "admin" {
		http.Error(w, "kind must be client or admin", http.StatusBadRequest)
		return
	}
	tenant := r.URL.Query().Get("tenant")
	if tenant == "" {
		tenant = "demo"
	}
	groups := csvList(r.URL.Query().Get("groups"))
	expiresAt := time.Now().Add(24 * time.Hour)
	claims := jwt.MapClaims{
		"iss":    "openrtc-reference",
		"aud":    "openrtc-clients",
		"sub":    username,
		"exp":    expiresAt.Unix(),
		"tenant": tenant,
	}
	if len(groups) > 0 {
		claims["groups"] = groups
		claims["groupIds"] = groups
	}
	if kind == "admin" {
		claims["aud"] = "openrtc-admin"
		claims["scope"] = firstNonEmpty(r.URL.Query().Get("scope"), "publish:* presence:* rooms:* storage:* comments:* notifications:*")
	} else if r.URL.Query().Get("access") != "grants" {
		claims["join"] = []string{"*"}
		claims["publish"] = []string{"*"}
		claims["presence"] = []string{"*"}
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "ref-key"
	signed, err := token.SignedString(privateKey)
	if err != nil {
		http.Error(w, "token signing failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"token":     signed,
		"kind":      kind,
		"username":  username,
		"tenant":    tenant,
		"groups":    groups,
		"expiresAt": expiresAt.Format(time.RFC3339),
	})
}

func handleConfig(runtimePort, adminPort string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"wsURL":           fmt.Sprintf("ws://localhost:%s/ws", runtimePort),
			"yjsURL":          fmt.Sprintf("ws://localhost:%s/yjs", runtimePort),
			"adminURL":        fmt.Sprintf("http://localhost:%s", adminPort),
			"adminProxyURL":   "/admin",
			"runtimeURL":      fmt.Sprintf("http://localhost:%s", runtimePort),
			"runtimeProxyURL": "/runtime",
		})
	}
}

func startProcess(binary string, env map[string]string, repoDir string) {
	binPath := fmt.Sprintf("./server/cmd/%s", binary)
	cmd := exec.Command("go", "run", binPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmdEnv := os.Environ()
	for k, v := range env {
		cmdEnv = append(cmdEnv, k+"="+v)
	}
	cmd.Env = cmdEnv
	cmd.Dir = repoDir

	if err := cmd.Run(); err != nil {
		log.Printf("%s exited: %v", binary, err)
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

func findReferenceAppDir() string {
	if fileExists(filepath.Join("static", "index.html")) {
		return "."
	}
	if fileExists(filepath.Join("reference-app", "static", "index.html")) {
		return "reference-app"
	}
	return "."
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
