package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestVerifierValidateAndClaimsAuthorize(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	defer jwksServer.Close()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    "https://issuer.example.com",
			Audience:  []string{"openrtc-clients"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		Tenant:  "tenant-a",
		Join:    []string{"tenant-a:*"},
		Publish: []string{"tenant-a:*"},
	})
	token.Header["kid"] = "runtime-key"

	rawToken, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	verifier := NewVerifier("https://issuer.example.com", "openrtc-clients", jwksServer.URL)
	claims, err := verifier.Verify(context.Background(), rawToken)
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}

	if !claims.Allows("join", "tenant-a:room-1", true, ":") {
		t.Fatalf("expected join to be allowed")
	}
	if claims.Allows("publish", "tenant-b:room-1", true, ":") {
		t.Fatalf("expected publish to be rejected")
	}
}
