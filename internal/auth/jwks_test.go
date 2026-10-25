package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// A fake identity provider whose published keys and availability a test can change.
type fakeIdP struct {
	mu     sync.Mutex
	keys   map[string]*rsa.PrivateKey
	use    map[string]string
	status int
	url    string
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	idp := &fakeIdP{keys: map[string]*rsa.PrivateKey{}, use: map[string]string{}, status: http.StatusOK}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		idp.mu.Lock()
		defer idp.mu.Unlock()
		if idp.status != http.StatusOK {
			w.WriteHeader(idp.status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unavailable"})
			return
		}
		b64 := base64.RawURLEncoding.EncodeToString
		var keys []map[string]string
		for kid, k := range idp.keys {
			keys = append(keys, map[string]string{
				"kid": kid, "kty": "RSA", "use": idp.use[kid],
				"n": b64(k.N.Bytes()), "e": b64(big.NewInt(int64(k.E)).Bytes()),
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	}))
	t.Cleanup(srv.Close)
	idp.url = srv.URL
	return idp
}

func (idp *fakeIdP) addKey(t *testing.T, kid, use string) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp.mu.Lock()
	defer idp.mu.Unlock()
	idp.keys[kid], idp.use[kid] = k, use
}

func (idp *fakeIdP) retire(kid string) {
	idp.mu.Lock()
	defer idp.mu.Unlock()
	delete(idp.keys, kid)
}

func (idp *fakeIdP) setStatus(status int) {
	idp.mu.Lock()
	defer idp.mu.Unlock()
	idp.status = status
}

func (idp *fakeIdP) sign(t *testing.T, kid string, claims jwt.MapClaims) string {
	t.Helper()
	idp.mu.Lock()
	key := idp.keys[kid]
	idp.mu.Unlock()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func valid(extra jwt.MapClaims) jwt.MapClaims {
	claims := jwt.MapClaims{"sub": "alice", "aud": "aos-spa", "exp": time.Now().Add(time.Minute).Unix()}
	for k, v := range extra {
		claims[k] = v
	}
	return claims
}

func verify(idp *fakeIdP, token string) error {
	_, err := verifyAndParseJWT(token, JWTConfig{JWKURL: idp.url, Audience: "aos-spa"})
	return err
}

// shortCache makes cached keys stale at once, as if the max age had passed.
func shortCache(t *testing.T) {
	maxAge, minRefresh := jwksMaxAge, jwksMinRefresh
	jwksMaxAge, jwksMinRefresh = 0, 0
	t.Cleanup(func() { jwksMaxAge, jwksMinRefresh = maxAge, minRefresh })
}

func TestARotatedKeyIsAccepted(t *testing.T) {
	idp := newFakeIdP(t)
	idp.addKey(t, "k1", "sig")
	if err := verify(idp, idp.sign(t, "k1", valid(nil))); err != nil {
		t.Fatal(err)
	}
	idp.addKey(t, "k2", "sig")
	minRefresh := jwksMinRefresh
	jwksMinRefresh = 0
	t.Cleanup(func() { jwksMinRefresh = minRefresh })
	if err := verify(idp, idp.sign(t, "k2", valid(nil))); err != nil {
		t.Fatalf("token signed with the new key: %v", err)
	}
}

func TestARetiredKeyStopsVerifying(t *testing.T) {
	idp := newFakeIdP(t)
	idp.addKey(t, "k1", "sig")
	idp.addKey(t, "k2", "sig")
	old := idp.sign(t, "k1", valid(nil))
	if err := verify(idp, old); err != nil {
		t.Fatal(err)
	}
	idp.retire("k1")
	shortCache(t)
	if err := verify(idp, old); err == nil {
		t.Fatal("a token signed with a retired key was accepted")
	}
}

func TestAnIdPOutageKeepsTheKnownKeys(t *testing.T) {
	idp := newFakeIdP(t)
	idp.addKey(t, "k1", "sig")
	token := idp.sign(t, "k1", valid(nil))
	if err := verify(idp, token); err != nil {
		t.Fatal(err)
	}
	idp.setStatus(http.StatusServiceUnavailable)
	shortCache(t)
	if err := verify(idp, token); err != nil {
		t.Fatalf("an outage signed everyone out: %v", err)
	}
}

func TestAnEncryptionKeyNeverVerifiesASignature(t *testing.T) {
	idp := newFakeIdP(t)
	idp.addKey(t, "enc1", "enc")
	if err := verify(idp, idp.sign(t, "enc1", valid(nil))); err == nil {
		t.Fatal("a token signed with an encryption key was accepted")
	}
}

func TestTheAudienceAndExpiryAreRequired(t *testing.T) {
	idp := newFakeIdP(t)
	idp.addKey(t, "k1", "sig")
	cases := map[string]jwt.MapClaims{
		"another app's audience": valid(jwt.MapClaims{"aud": "prudence-api"}),
		"no expiry":              {"sub": "alice", "aud": "aos-spa"},
		"expired":                valid(jwt.MapClaims{"exp": time.Now().Add(-time.Hour).Unix()}),
	}
	for name, claims := range cases {
		if err := verify(idp, idp.sign(t, "k1", claims)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// Without a configured audience any audience passes, as before.
	if _, err := verifyAndParseJWT(idp.sign(t, "k1", valid(jwt.MapClaims{"aud": "prudence-api"})), JWTConfig{JWKURL: idp.url}); err != nil {
		t.Fatalf("no audience configured: %v", err)
	}
}
