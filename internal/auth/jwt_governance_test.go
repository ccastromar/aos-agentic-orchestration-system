package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/governance"
	"github.com/golang-jwt/jwt/v5"
)

// idpToken serves a JWKS for a fresh RSA key and returns a token it signs, plus the JWKS URL.
func idpToken(t *testing.T, claims jwt.MapClaims) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	b64 := base64.RawURLEncoding.EncodeToString
	jwks := map[string]any{"keys": []map[string]string{{
		"kid": "k1", "kty": "RSA", "n": b64(key.N.Bytes()), "e": b64(big.NewInt(int64(key.E)).Bytes()),
	}}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(srv.Close)
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "k1"
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return signed, srv.URL
}

func authenticate(t *testing.T, cfg JWTConfig, token string) (*Identity, error) {
	t.Helper()
	req := httptest.NewRequest("GET", "/task", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return NewJWTAuthenticator(cfg).Authenticate(req)
}

func TestIdPTokenGetsFunctionsFromTheModel(t *testing.T) {
	model, err := governance.LoadModel("")
	if err != nil {
		t.Fatal(err)
	}
	token, jwks := idpToken(t, jwt.MapClaims{
		"sub": "alice", "roles": []string{"aos-operator", "prudence-author"}, "exp": time.Now().Add(time.Minute).Unix(),
	})
	id, err := authenticate(t, JWTConfig{JWKURL: jwks, Governance: model}, token)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ai.use:operator"}; !reflect.DeepEqual(id.Functions, want) {
		t.Fatalf("functions = %v, want %v", id.Functions, want)
	}
}

func TestIdPTokenWithoutFunctionsIsForbidden(t *testing.T) {
	model, err := governance.LoadModel("")
	if err != nil {
		t.Fatal(err)
	}
	// A Prudence role grants nothing in AOS: the request is refused before any work starts.
	token, jwks := idpToken(t, jwt.MapClaims{
		"sub": "bob", "roles": []string{"prudence-system-approver"}, "exp": time.Now().Add(time.Minute).Unix(),
	})
	_, err = authenticate(t, JWTConfig{JWKURL: jwks, Governance: model}, token)
	if authErr, ok := err.(*AuthError); !ok || authErr.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %v", err)
	}
}

func TestDevelopmentTokenGetsFunctionsFromItsRoles(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-for-governance")
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "carol", "roles": []string{"finance", "admin"}, "exp": time.Now().Add(time.Minute).Unix(),
	}).SignedString([]byte("test-secret-for-governance"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := authenticate(t, JWTConfig{}, token)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ai.use:finance", "ai.use:admin", "platform.admin"}
	if !reflect.DeepEqual(id.Functions, want) {
		t.Fatalf("functions = %v, want %v", id.Functions, want)
	}
}
