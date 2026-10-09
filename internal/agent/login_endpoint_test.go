package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
)

func TestHandleLogin_IssuesToken(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	a := NewAPIAgent(bus.New(), bus.New())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"username":"carlos","password":"whatever","roles":["finance"]}`))
	a.handleLogin(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if resp["token"] == "" || resp["token"] == nil {
		t.Fatalf("expected token in response: %v", resp)
	}
	if resp["user_id"] != "carlos" {
		t.Errorf("user_id = %v, want carlos", resp["user_id"])
	}
}

func TestHandleLogin_RequiresUsernameAndPost(t *testing.T) {
	a := NewAPIAgent(bus.New(), bus.New())

	rec := httptest.NewRecorder()
	a.handleLogin(rec, httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{}`)))
	if rec.Code != 400 {
		t.Fatalf("expected 400 without username, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	a.handleLogin(rec, httptest.NewRequest("GET", "/auth/login", nil))
	if rec.Code != 405 {
		t.Fatalf("expected 405 for GET, got %d", rec.Code)
	}
}

func TestHandleLogin_DisabledOutsideDevelopment(t *testing.T) {
	a := NewAPIAgent(bus.New(), &config.EnvVars{AppEnv: "production"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"username":"attacker"}`))
	a.handleLogin(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("production dev-login status = %d, want 404", rec.Code)
	}
}
