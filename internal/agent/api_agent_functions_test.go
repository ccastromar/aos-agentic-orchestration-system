package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/auth"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/governance"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
)

// Governance functions (contract v1): oversight may decide anyone's human step; oversight and
// audit may read anyone's task; nobody else sees tasks they do not own.

func requestWithFunctions(method, target, userID string, functions ...string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	return req.WithContext(auth.WithIdentity(req.Context(), &auth.Identity{UserID: userID, Functions: functions}))
}

func TestOversightDecidesAnotherPersonsHumanStep(t *testing.T) {
	api, manager, executionBus, _ := securityTestAgent()
	if err := manager.Save(context.Background(), &state.ExecutionState{ID: "task-o", UserID: "alice", Gate: "manager_review"}); err != nil {
		t.Fatal(err)
	}
	decisions := make(chan bus.Message, 2)
	executionBus.Subscribe("verifier", decisions)

	// Another user without oversight, even with every ai.use domain, cannot.
	rec := httptest.NewRecorder()
	api.handleHumanApprove(rec, requestWithFunctions(http.MethodPost, "/task/approve?id=task-o&gate=manager_review", "bob", "ai.use:*", "audit"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-overseer status = %d, want 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	api.handleHumanReject(rec, requestWithFunctions(http.MethodPost, "/task/reject?id=task-o&gate=manager_review", "olga", "oversight"))
	if rec.Code != http.StatusOK {
		t.Fatalf("overseer status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	select {
	case msg := <-decisions:
		if msg.Payload["decision"] != "rejected" || msg.Payload["id"] != "task-o" {
			t.Fatalf("unexpected decision payload: %#v", msg.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("the overseer's decision did not reach the verifier")
	}
}

func TestOversightCannotAnswerAnotherPersonsClarification(t *testing.T) {
	api, manager, _, _ := securityTestAgent()
	if err := manager.Save(context.Background(), &state.ExecutionState{ID: "task-c", UserID: "alice", Gate: "clarification"}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	api.handleClarifyReply(rec, requestWithFunctions(http.MethodPost, "/task/reply?id=task-c", "olga", "oversight"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (only the requester answers)", rec.Code)
	}
}

func TestOversightAndAuditListEveryonesTasks(t *testing.T) {
	api, manager, _, _ := securityTestAgent()
	for _, st := range []state.ExecutionState{{ID: "t-a", UserID: "alice"}, {ID: "t-b", UserID: "bob"}} {
		st := st
		if err := manager.Save(context.Background(), &st); err != nil {
			t.Fatal(err)
		}
	}
	count := func(functions ...string) int {
		rec := httptest.NewRecorder()
		api.HandleListTasks(rec, requestWithFunctions(http.MethodGet, "/api/v1/tasks", "carol", functions...))
		var tasks []state.ExecutionState
		if err := json.NewDecoder(rec.Body).Decode(&tasks); err != nil {
			t.Fatal(err)
		}
		return len(tasks)
	}
	if n := count("ai.use:finance"); n != 0 {
		t.Fatalf("a user sees %d tasks of others, want 0", n)
	}
	if n := count("audit"); n != 2 {
		t.Fatalf("audit sees %d tasks, want 2", n)
	}
	if n := count("oversight"); n != 2 {
		t.Fatalf("oversight sees %d tasks, want 2", n)
	}
}

func TestIncompatibleAiUseAndOversightStopsTheRequesterDecidingTheirOwnStep(t *testing.T) {
	api, manager, _, _ := securityTestAgent()
	model, err := governance.NewModel(strictModel())
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(context.Background(), &state.ExecutionState{ID: "task-s", UserID: "alice", Gate: "manager_review"}); err != nil {
		t.Fatal(err)
	}
	approve := func(userID string, functions ...string) int {
		rec := httptest.NewRecorder()
		api.handleHumanApprove(rec, requestWithFunctions(http.MethodPost, "/task/approve?id=task-s&gate=manager_review", userID, functions...))
		return rec.Code
	}

	// Without the rule the requester may decide their own step (today's behaviour).
	api.SetGovernance(nil)
	if code := approve("alice", "ai.use:operator", "oversight"); code != http.StatusOK {
		t.Fatalf("without the rule: status = %d, want 200", code)
	}

	api.SetGovernance(model)
	if code := approve("alice", "ai.use:operator", "oversight"); code != http.StatusForbidden {
		t.Fatalf("requester with the rule: status = %d, want 403", code)
	}
	if code := approve("olga", "oversight"); code != http.StatusOK {
		t.Fatalf("independent overseer: status = %d, want 200", code)
	}
}

// strictModel is the default model plus [ai.use, oversight] as an incompatible pair.
func strictModel() map[string]any {
	return map[string]any{
		"version": governance.ModelVersion,
		"rules": map[string]any{
			"approvals":    map[string]any{"HIGH": map[string]any{"count": 2, "distinct": true}},
			"incompatible": []any{[]any{"ai.use", "oversight"}},
			"accountable":  map[string]any{"HIGH": []any{"SUPERVISOR"}},
		},
		"identity": map[string]any{"people": map[string]any{"claim": "roles"}, "machines": map[string]any{"claim": "scope"}},
		"map":      map[string]any{"aos-operator": []any{"ai.use:operator@aos"}},
	}
}
