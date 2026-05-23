package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const jwksMaxBytes = 1024 * 1024

type Claims struct {
	jwt.RegisteredClaims
	Tenant   string   `json:"tenant,omitempty"`
	Join     []string `json:"join,omitempty"`
	Publish  []string `json:"publish,omitempty"`
	Presence []string `json:"presence,omitempty"`
	Groups   []string `json:"groups,omitempty"`
	GroupIDs []string `json:"groupIds,omitempty"`
	Scope    string   `json:"scope,omitempty"`
}

type Verifier struct {
	issuer   string
	audience string
	jwksURL  string
	client   *http.Client

	mu        sync.Mutex
	cachedAt  time.Time
	cachedTTL time.Duration
	keys      map[string]*rsa.PublicKey
}

func NewVerifier(issuer string, audience string, jwksURL string) *Verifier {
	return &Verifier{
		issuer:    issuer,
		audience:  audience,
		jwksURL:   jwksURL,
		client:    &http.Client{Timeout: 5 * time.Second},
		cachedTTL: 5 * time.Minute,
	}
}

func (v *Verifier) Verify(ctx context.Context, rawToken string) (*Claims, error) {
	claims := &Claims{}
	parser := jwt.NewParser(
		jwt.WithAudience(v.audience),
		jwt.WithIssuer(v.issuer),
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg(), jwt.SigningMethodRS384.Alg(), jwt.SigningMethodRS512.Alg()}),
	)

	token, err := parser.ParseWithClaims(rawToken, claims, func(token *jwt.Token) (interface{}, error) {
		kid, _ := token.Header["kid"].(string)
		key, err := v.lookupKey(ctx, kid)
		if err != nil {
			return nil, err
		}
		return key, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("token is invalid")
	}
	return claims, nil
}

func (v *Verifier) lookupKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if time.Since(v.cachedAt) > v.cachedTTL || len(v.keys) == 0 {
		keys, err := v.fetchKeys(ctx)
		if err != nil {
			return nil, err
		}
		v.keys = keys
		v.cachedAt = time.Now()
	}

	if kid == "" && len(v.keys) == 1 {
		for _, key := range v.keys {
			return key, nil
		}
	}

	key := v.keys[kid]
	if key == nil {
		return nil, fmt.Errorf("jwks key %q not found", kid)
	}
	return key, nil
}

func (v *Verifier) fetchKeys(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := v.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("jwks request failed with status %d", response.StatusCode)
	}

	var jwks struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, jwksMaxBytes)).Decode(&jwks); err != nil {
		return nil, err
	}

	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, key := range jwks.Keys {
		if key.Kty != "RSA" {
			continue
		}
		publicKey, err := rsaKeyFromJWK(key.N, key.E)
		if err != nil {
			return nil, err
		}
		keys[key.Kid] = publicKey
	}

	if len(keys) == 0 {
		return nil, errors.New("jwks contains no rsa keys")
	}
	return keys, nil
}

func (c *Claims) Allows(action string, room string, enforcePrefix bool, separator string) bool {
	if enforcePrefix {
		if c.Tenant == "" {
			return false
		}
		if !strings.HasPrefix(room, c.Tenant+separator) {
			return false
		}
	}

	patterns := c.patternsFor(action)
	if len(patterns) == 0 {
		return false
	}

	for _, pattern := range patterns {
		matched, err := path.Match(pattern, room)
		if err == nil && matched {
			return true
		}
	}
	return false
}

func (c *Claims) patternsFor(action string) []string {
	patterns := []string{}
	switch action {
	case "join":
		patterns = append(patterns, c.Join...)
	case "publish":
		patterns = append(patterns, c.Publish...)
	case "presence":
		patterns = append(patterns, c.Presence...)
	}

	for _, token := range strings.Fields(c.Scope) {
		prefix := action + ":"
		if strings.HasPrefix(token, prefix) {
			patterns = append(patterns, strings.TrimPrefix(token, prefix))
		}
	}
	return patterns
}

func (c *Claims) RoomGroupIDs() []string {
	seen := map[string]struct{}{}
	groups := make([]string, 0, len(c.Groups)+len(c.GroupIDs))
	for _, groupID := range append(append([]string{}, c.GroupIDs...), c.Groups...) {
		if groupID == "" {
			continue
		}
		if _, ok := seen[groupID]; ok {
			continue
		}
		seen[groupID] = struct{}{}
		groups = append(groups, groupID)
	}
	return groups
}

func rsaKeyFromJWK(nValue string, eValue string) (*rsa.PublicKey, error) {
	modulusBytes, err := base64.RawURLEncoding.DecodeString(nValue)
	if err != nil {
		return nil, err
	}
	exponentBytes, err := base64.RawURLEncoding.DecodeString(eValue)
	if err != nil {
		return nil, err
	}

	modulus := new(big.Int).SetBytes(modulusBytes)
	exponent := new(big.Int).SetBytes(exponentBytes).Int64()
	publicKey := &rsa.PublicKey{
		N: modulus,
		E: int(exponent),
	}
	if publicKey.E <= 0 || publicKey.N.Sign() == 0 {
		return nil, errors.New("invalid jwks rsa key")
	}
	return publicKey, nil
}
