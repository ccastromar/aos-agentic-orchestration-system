package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/audit"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
)

// Knowing a callback token is not enough to finish an operation: a step that declares
// callback_secret_env only accepts callbacks its provider signed.
func TestSignedCallbacks(t *testing.T) {
	const secret = "custodian-secret"
	body := `{"status":"completed","txHash":"0xabc"}`
	sign := func(key string, at time.Time) http.Header {
		ts := strconv.FormatInt(at.Unix(), 10)
		return http.Header{
			"X-Aos-Callback-Timestamp": {ts},
			"X-Aos-Callback-Signature": {audit.ProviderSignature(key, ts, []byte(body))},
		}
	}
	cases := []struct {
		name     string
		envValue string
		headers  http.Header
		wantCode int
	}{
		{"signed", secret, sign(secret, time.Now()), http.StatusOK},
		{"unsigned", secret, http.Header{}, http.StatusUnauthorized},
		{"signed with another key", secret, sign("guess", time.Now()), http.StatusUnauthorized},
		{"replayed after the window", secret, sign(secret, time.Now().Add(-10*time.Minute)), http.StatusUnauthorized},
		{"secret not configured", "", sign(secret, time.Now()), http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TEST_CALLBACK_SECRET", tc.envValue)
			pipe := config.Pipeline{Name: "custody", Steps: []config.PipelineStep{
				{ID: "submit", Tool: "custody.submit"},
				{ID: "wait", Type: "await_callback", CallbackSecretEnv: "TEST_CALLBACK_SECRET"},
			}}
			cfg := &config.Config{Pipelines: map[string]config.Pipeline{pipe.Name: pipe}}
			manager := state.NewStateManager(state.NewMemoryStore())
			api := NewAPIAgent(bus.New(), cfg, manager)
			if err := manager.Save(context.Background(), &state.ExecutionState{
				ID: "t-1", Pipeline: "custody", Gate: "await_callback", CallbackToken: "tok", AwaitStep: "wait", Status: "await_callback",
			}); err != nil {
				t.Fatal(err)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/callbacks/tok", strings.NewReader(body))
			for k, v := range tc.headers {
				req.Header[k] = v
			}
			rec := httptest.NewRecorder()
			api.handleCallback(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.wantCode, rec.Body.String())
			}

			st, _ := manager.Load(context.Background(), "t-1")
			if accepted := st.CallbackData != nil; accepted != (tc.wantCode == http.StatusOK) {
				t.Fatalf("callback stored = %v, want %v: a refused callback must leave the wait as it was", accepted, !accepted)
			}
		})
	}
}
