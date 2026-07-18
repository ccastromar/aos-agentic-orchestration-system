package auth

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestIssueToken_RoundTripsThroughAuthenticate(t *testing.T) {
	t.Setenv("JWT_SECRET", "")

	token, err := IssueToken("carlos", []string{"finance"}, time.Hour)
	if err != nil {
		t.Fatalf("issuing token: %v", err)
	}

	r := httptest.NewRequest("POST", "/ask", nil)
	r.Header.Set("Authorization", "Bearer "+token)

	id, err := NewJWTAuthenticator(JWTConfig{}).Authenticate(r)
	if err != nil {
		t.Fatalf("authenticating issued token: %v", err)
	}
	if id.UserID != "carlos" {
		t.Errorf("UserID = %q, want carlos", id.UserID)
	}
	if len(id.Roles) != 1 || id.Roles[0] != "finance" {
		t.Errorf("Roles = %v, want [finance]", id.Roles)
	}
}

func TestIssueToken_DefaultRoleAndSecretEnv(t *testing.T) {
	t.Setenv("JWT_SECRET", "super-secret")

	token, err := IssueToken("ana", nil, time.Hour)
	if err != nil {
		t.Fatalf("issuing token: %v", err)
	}

	r := httptest.NewRequest("POST", "/ask", nil)
	r.Header.Set("Authorization", "Bearer "+token)

	id, err := NewJWTAuthenticator(JWTConfig{}).Authenticate(r)
	if err != nil {
		t.Fatalf("authenticating issued token: %v", err)
	}
	if id.UserID != "ana" || len(id.Roles) != 1 || id.Roles[0] != "user" {
		t.Errorf("unexpected identity: %#v", id)
	}
}

func TestAuthenticate_GarbageTokenFallsBackToMockInDev(t *testing.T) {
	t.Setenv("JWT_SECRET", "")

	r := httptest.NewRequest("POST", "/ask", nil)
	r.Header.Set("Authorization", "Bearer e2e-key")

	id, err := NewJWTAuthenticator(JWTConfig{}).Authenticate(r)
	if err != nil {
		t.Fatalf("expected dev fallback, got error: %v", err)
	}
	if id.UserID != "demo-user" {
		t.Errorf("expected demo-user fallback, got %q", id.UserID)
	}
}

func TestAuthenticate_GarbageTokenRejectedWithSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "super-secret")

	r := httptest.NewRequest("POST", "/ask", nil)
	r.Header.Set("Authorization", "Bearer garbage")

	if _, err := NewJWTAuthenticator(JWTConfig{}).Authenticate(r); err == nil {
		t.Fatalf("expected rejection of garbage token when JWT_SECRET is set")
	}
}
