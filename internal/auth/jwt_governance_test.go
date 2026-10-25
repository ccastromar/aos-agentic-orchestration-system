package auth

import (
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/governance"
	"github.com/golang-jwt/jwt/v5"
)

func TestJWTIdentityCarriesGovernanceFunctions(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-for-governance")
	model, err := governance.LoadModel("")
	if err != nil {
		t.Fatal(err)
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   "alice",
		"roles": []string{"aos-operator", "prudence-author"},
		"exp":   time.Now().Add(time.Minute).Unix(),
	}).SignedString([]byte("test-secret-for-governance"))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/task", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	id, err := NewJWTAuthenticator(JWTConfig{Governance: model}).Authenticate(req)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ai.use:operator"}; !reflect.DeepEqual(id.Functions, want) {
		t.Fatalf("functions = %v, want %v", id.Functions, want)
	}
}
