package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/auth"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
	"github.com/stretchr/testify/require"
)

// After the one-shot result is consumed, /task must still show why the task was blocked:
// the verdict comes back from the persisted state, not a generic "error" or empty data.
func TestTaskShowsPersistedGovernanceVerdict(t *testing.T) {
	t.Setenv("API_KEY", "")
	sm := state.NewStateManager(state.NewMemoryStore())
	verdict := map[string]any{
		"decision": "APPROVAL_REQUIRED", "reason": "high-risk system awaiting governance approval",
		"approver": "risk-officer", "aiSystemId": "sys-credit", "pipeline": "pipeline_aos_credit_pre_screening",
		"message": "governance approval required in Prudence: high-risk system awaiting governance approval",
	}
	require.NoError(t, sm.Save(context.Background(), &state.ExecutionState{
		ID: "gov-task-1", UserID: "user-1", Pipeline: "pipeline_aos_credit_pre_screening",
		Status: "governance_blocked", StepResults: map[string]any{"_governance": verdict},
	}))
	api := NewAPIAgent(bus.New(), sm)

	req := httptest.NewRequest(http.MethodGet, "/task?id=gov-task-1", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), &auth.Identity{UserID: "user-1"}))
	rec := httptest.NewRecorder()
	api.handleTask(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Equal(t, "governance_blocked", body["status"])
	require.Equal(t, verdict, body["data"])
}
