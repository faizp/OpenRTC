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
	"os"
	"os/exec"
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

	jwksURL := fmt.Sprintf("http://localhost:%s/jwks", appPort)

	// Start OpenRTC runtime
	go startProcess("openrtc-runtime", map[string]string{
		"OPENRTC_MODE":                "cluster",
		"OPENRTC_NODE_ID":             "ref-runtime",
		"OPENRTC_SERVER_HOST":         "0.0.0.0",
		"OPENRTC_SERVER_PORT":         runtimePort,
		"OPENRTC_WS_PATH":             "/ws",
		"OPENRTC_REDIS_URL":           redisURL,
		"OPENRTC_AUTH_ISSUER":         "openrtc-reference",
		"OPENRTC_AUTH_AUDIENCE":       "openrtc-clients",
		"OPENRTC_AUTH_JWKS_URL":       jwksURL,
		"OPENRTC_TENANT_ENFORCE_PREFIX": "false",
		"OPENRTC_ADMIN_AUTH_ISSUER":   "openrtc-reference",
		"OPENRTC_ADMIN_AUTH_AUDIENCE": "openrtc-admin",
	})

	// Start OpenRTC admin
	go startProcess("openrtc-admin", map[string]string{
		"OPENRTC_MODE":                "cluster",
		"OPENRTC_NODE_ID":             "ref-admin",
		"OPENRTC_SERVER_HOST":         "0.0.0.0",
		"OPENRTC_SERVER_PORT":         adminPort,
		"OPENRTC_REDIS_URL":           redisURL,
		"OPENRTC_AUTH_ISSUER":         "openrtc-reference",
		"OPENRTC_AUTH_AUDIENCE":       "openrtc-clients",
		"OPENRTC_AUTH_JWKS_URL":       jwksURL,
		"OPENRTC_TENANT_ENFORCE_PREFIX": "false",
		"OPENRTC_ADMIN_AUTH_ISSUER":   "openrtc-reference",
		"OPENRTC_ADMIN_AUTH_AUDIENCE": "openrtc-admin",
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/jwks", handleJWKS)
	mux.HandleFunc("/token", handleToken)
	mux.HandleFunc("/config", handleConfig(runtimePort, adminPort))
	mux.Handle("/", http.FileServer(http.Dir("static")))

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

	claims := jwt.MapClaims{
		"iss":      "openrtc-reference",
		"aud":      "openrtc-clients",
		"sub":      username,
		"exp":      time.Now().Add(24 * time.Hour).Unix(),
		"tenant":   "demo",
		"join":     []string{"*"},
		"publish":  []string{"*"},
		"presence": []string{"*"},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "ref-key"
	signed, err := token.SignedString(privateKey)
	if err != nil {
		http.Error(w, "token signing failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": signed})
}

func handleConfig(runtimePort, adminPort string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"wsURL":    fmt.Sprintf("ws://localhost:%s/ws", runtimePort),
			"adminURL": fmt.Sprintf("http://localhost:%s", adminPort),
		})
	}
}

func startProcess(binary string, env map[string]string) {
	// Try local build first, then PATH
	binPath := fmt.Sprintf("../server/cmd/%s", binary)
	cmd := exec.Command("go", "run", binPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmdEnv := os.Environ()
	for k, v := range env {
		cmdEnv = append(cmdEnv, k+"="+v)
	}
	cmd.Env = cmdEnv
	cmd.Dir = ".."

	if err := cmd.Run(); err != nil {
		log.Printf("%s exited: %v", binary, err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
