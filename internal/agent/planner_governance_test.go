package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// governanceFixture is a planner wired to a stub Prudence PDP that always answers body.
type governanceFixture struct {
	planner *Planner
	sm      *state.StateManager
	calls   *atomic.Int32
	subject *atomic.Value
}

func newGovernanceFixture(t *testing.T, body string) governanceFixture {
	t.Helper()
	// Keep a developer's local m2m settings out of the test: the PDP stub is unauthenticated.
	t.Setenv("PRUDENCE_PDP_TOKEN_URL", "")
	t.Setenv("PRUDENCE_PDP_SUBJECT", "")

	var calls atomic.Int32
	var subject atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req struct {
			Subject string `json:"subject"`
		}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		subject.Store(req.Subject)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("PRUDENCE_PDP_URL", srv.URL)

	sm := state.NewStateManager(state.NewMemoryStore())
	p := NewPlanner(bus.New(), &config.Config{}, llmNewTaskDummy{}, bus.New(), sm)
	return governanceFixture{planner: p, sm: sm, calls: &calls, subject: &subject}
}

func (f governanceFixture) allows(t *testing.T, id string, pipe config.Pipeline) bool {
	t.Helper()
	t.Cleanup(func() { deleteResult(id) })
	return f.planner.governanceAllows(context.Background(), id, "session-1", "user-1", "demo.intent", pipe.Name, pipe, map[string]string{"k": "v"})
}

func takeResult(t *testing.T, id string) Result {
	t.Helper()
	res, ok := getResult(id)
	require.True(t, ok, "expected a stored result for %s", id)
	return res
}

func TestGovernanceAllowRunsWithoutStoringAResult(t *testing.T) {
	f := newGovernanceFixture(t, `{"decision":"ALLOW"}`)

	require.True(t, f.allows(t, "gov-allow", config.Pipeline{Name: "p", AiSystem: "sys-1", Governed: true}))
	_, stored := getResult("gov-allow")
	require.False(t, stored, "an allowed task continues; it must not end with a result")
	require.Equal(t, int32(1), f.calls.Load())
	require.Equal(t, "user-1", f.subject.Load(), "without PRUDENCE_PDP_SUBJECT the signed-in user is the subject")
}

func TestGovernanceUsesTheConfiguredServiceSubject(t *testing.T) {
	f := newGovernanceFixture(t, `{"decision":"ALLOW"}`)
	t.Setenv("PRUDENCE_PDP_SUBJECT", "service-account-aos-pdp")

	require.True(t, f.allows(t, "gov-subject", config.Pipeline{Name: "p", AiSystem: "sys-1"}))
	require.Equal(t, "service-account-aos-pdp", f.subject.Load())
}

func TestGovernanceVerdictsEndTheTaskAsGovernanceBlocked(t *testing.T) {
	cases := []struct {
		name, body, decision, reason, approver, message string
	}{
		{"deny", `{"decision":"DENY","reason":"prohibited AI system"}`,
			"DENY", "prohibited AI system", "", "governance: denied running pipeline_x: prohibited AI system"},
		{"approval required", `{"decision":"APPROVAL_REQUIRED","reason":"high-risk system awaiting governance approval","approver":"risk-officer"}`,
			"APPROVAL_REQUIRED", "high-risk system awaiting governance approval", "risk-officer",
			"governance approval required in Prudence: high-risk system awaiting governance approval"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newGovernanceFixture(t, tc.body)
			id := "gov-blocked-" + tc.decision
			pipe := config.Pipeline{Name: "pipeline_x", AiSystem: "sys-x", Governed: true}

			require.False(t, f.allows(t, id, pipe))

			res := takeResult(t, id)
			require.Equal(t, "governance_blocked", res.Status, "a policy verdict is not a technical error")
			require.Empty(t, res.Err)
			verdict := map[string]any{
				"decision": tc.decision, "reason": tc.reason, "approver": tc.approver,
				"aiSystemId": "sys-x", "pipeline": "pipeline_x", "message": tc.message,
			}
			require.Equal(t, verdict, res.Data)

			// The verdict is persisted too, so it survives the one-shot result being consumed.
			st, err := f.sm.Load(context.Background(), id)
			require.NoError(t, err)
			require.NotNil(t, st)
			require.Equal(t, "governance_blocked", st.Status)
			require.Equal(t, "user-1", st.UserID)
			require.Equal(t, "pipeline_x", st.Pipeline)
			require.Equal(t, verdict, st.StepResults["_governance"])
		})
	}
}

func TestGovernedPipelineFailsClosedWhenNotConfigured(t *testing.T) {
	t.Run("missing ai_system (e.g. unset env var)", func(t *testing.T) {
		f := newGovernanceFixture(t, `{"decision":"ALLOW"}`)

		require.False(t, f.allows(t, "gov-no-system", config.Pipeline{Name: "pipeline_x", AiSystem: "  ", Governed: true}))
		res := takeResult(t, "gov-no-system")
		require.Equal(t, "error", res.Status)
		require.Contains(t, res.Err, "fail-closed")
		require.Zero(t, f.calls.Load(), "nothing to ask the PDP about; it must not be called")
	})
	t.Run("missing PRUDENCE_PDP_URL", func(t *testing.T) {
		f := newGovernanceFixture(t, `{"decision":"ALLOW"}`)
		t.Setenv("PRUDENCE_PDP_URL", "")

		require.False(t, f.allows(t, "gov-no-pdp", config.Pipeline{Name: "pipeline_x", AiSystem: "sys-x", Governed: true}))
		res := takeResult(t, "gov-no-pdp")
		require.Equal(t, "error", res.Status)
		require.Contains(t, res.Err, "fail-closed")
	})
}

func TestUngovernedPipelineWithoutSystemStillRuns(t *testing.T) {
	f := newGovernanceFixture(t, `{"decision":"DENY"}`)
	t.Setenv("PRUDENCE_PDP_URL", "")

	require.True(t, f.allows(t, "gov-legacy", config.Pipeline{Name: "pipeline_plain"}))
	require.Zero(t, f.calls.Load())
}

func TestGovernanceTransportFailureIsAnErrorNotAVerdict(t *testing.T) {
	cases := map[string]string{
		"unexpected decision": `{"decision":"MAYBE"}`,
		"unreadable response": `not json`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			f := newGovernanceFixture(t, body)
			id := "gov-transport-" + name

			require.False(t, f.allows(t, id, config.Pipeline{Name: "pipeline_x", AiSystem: "sys-x", Governed: true}))
			res := takeResult(t, id)
			require.Equal(t, "error", res.Status)
			require.Contains(t, res.Err, "governance:")
		})
	}
}
