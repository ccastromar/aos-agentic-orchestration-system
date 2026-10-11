package auth

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// OIDC JWKS verification for the shared Chestertown IdP (Keycloak). When JWTConfig.JWKURL is set,
// AOS validates RS256 tokens against the realm's published keys instead of the local HMAC
// secret — converging AOS onto the same identity provider as Ernest and Prudence. No extra
// dependency: JWKS is fetched and RSA public keys are reconstructed from n/e.

type jwksEntry struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	N   string `json:"n"`
	E   string `json:"e"`
}
type jwksDoc struct {
	Keys []jwksEntry `json:"keys"`
}

type jwksCache struct {
	mu      sync.Mutex
	url     string
	keys    map[string]*rsa.PublicKey
	fetched time.Time
}

var jwksCaches sync.Map // jwkURL -> *jwksCache

func cacheFor(url string) *jwksCache {
	if c, ok := jwksCaches.Load(url); ok {
		return c.(*jwksCache)
	}
	c := &jwksCache{url: url, keys: map[string]*rsa.PublicKey{}}
	actual, _ := jwksCaches.LoadOrStore(url, c)
	return actual.(*jwksCache)
}

func (c *jwksCache) key(kid string) (*rsa.PublicKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if k, ok := c.keys[kid]; ok {
		return k, nil
	}
	// Unknown kid (or first use): refresh, but not more than every few seconds.
	if time.Since(c.fetched) < 5*time.Second && len(c.keys) > 0 {
		return nil, fmt.Errorf("unknown kid %q", kid)
	}
	if err := c.refreshLocked(); err != nil {
		return nil, err
	}
	if k, ok := c.keys[kid]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("unknown kid %q after refresh", kid)
}

func (c *jwksCache) refreshLocked() error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(c.url)
	if err != nil {
		return fmt.Errorf("jwks fetch: %w", err)
	}
	defer resp.Body.Close()
	var doc jwksDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return fmt.Errorf("jwks decode: %w", err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, e := range doc.Keys {
		if e.Kty != "RSA" {
			continue
		}
		nb, err := base64.RawURLEncoding.DecodeString(e.N)
		if err != nil {
			continue
		}
		eb, err := base64.RawURLEncoding.DecodeString(e.E)
		if err != nil {
			continue
		}
		keys[e.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: int(new(big.Int).SetBytes(eb).Int64())}
	}
	c.keys = keys
	c.fetched = time.Now()
	return nil
}

// rsaKeyfunc returns a jwt.Keyfunc that resolves the signing key from the realm JWKS.
func rsaKeyfunc(jwkURL string) jwt.Keyfunc {
	return func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, fmt.Errorf("missing kid")
		}
		return cacheFor(jwkURL).key(kid)
	}
}
