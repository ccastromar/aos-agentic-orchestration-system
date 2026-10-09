package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/audit"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
	"github.com/stretchr/testify/require"
)

func TestExpiredCallbackWaitIsPersistedForRestartRecovery(t *testing.T) {
	sm := state.NewStateManager(state.NewMemoryStore())
	deadline := time.Now().UTC().Add(-time.Second)
	require.NoError(t, sm.Save(context.Background(), &state.ExecutionState{
		ID: "expired-task", Pipeline: "pipeline", Gate: "await_callback",
		CallbackToken: "one-time-token", AwaitDeadline: &deadline, Status: "await_callback",
	}))

	// A fresh verifier using the same persistent store models the next AOS
	// process scanning state after a restart.
	v := NewVerifier(bus.New(), &config.Config{}, nil, bus.New(), sm)
	v.expireAwaitingCallbacks(context.Background())

	got, err := sm.Load(context.Background(), "expired-task")
	require.NoError(t, err)
	require.Equal(t, "timed_out", got.Status)
	require.Equal(t, "callback deadline exceeded", got.Error)
	require.Empty(t, got.CallbackToken)
	require.Empty(t, got.Gate)
	result, ok := getResult("expired-task")
	require.True(t, ok)
	require.Equal(t, "error", result.Status)
	deleteResult("expired-task")
}

func TestCallbackTokenCanResumeOnlyOnce(t *testing.T) {
	ctx := context.Background()
	sm := state.NewStateManager(state.NewMemoryStore())
	messageBus := bus.New()
	analystInbox := make(chan bus.Message, 4)
	messageBus.Subscribe("analyst", analystInbox)
	pipe := config.Pipeline{Name: "callback-pipeline", Mode: "linear", Steps: []config.PipelineStep{
		{ID: "submitted", Tool: "already.submitted"},
		{ID: "wait_result", Type: "await_callback"},
		{ID: "summarize", Analyst: true},
	}}
	require.NoError(t, sm.Save(ctx, &state.ExecutionState{
		ID: "callback-task", Intent: "test.intent", Pipeline: pipe.Name,
		CompletedSteps: []string{"submitted"}, Params: map[string]string{},
		StepResults: map[string]any{"already.submitted": map[string]any{"ok": true}},
		Gate:        "await_callback", CallbackToken: "callback-token", AwaitStep: "wait_result",
		CallbackData: map[string]any{"status": "completed"},
	}))
	v := NewVerifier(messageBus, &config.Config{Pipelines: map[string]config.Pipeline{pipe.Name: pipe}}, nil, bus.New(), sm)
	v.recoverPendingCallbacks(ctx)
	v.recoverPendingCallbacks(ctx)
	v.handleCallback(bus.Message{Type: "callback_received", Payload: map[string]any{"token": "callback-token", "data": map[string]any{"status": "duplicate"}}})

	select {
	case got := <-analystInbox:
		require.Equal(t, "summarize", got.Type)
	case <-time.After(time.Second):
		t.Fatal("callback did not resume the pipeline")
	}
	select {
	case got := <-analystInbox:
		t.Fatalf("duplicate callback resumed pipeline again: %#v", got)
	case <-time.After(30 * time.Millisecond):
	}
	got, err := sm.Load(ctx, "callback-task")
	require.NoError(t, err)
	require.Empty(t, got.CallbackToken)
	require.Contains(t, got.CompletedSteps, "wait_result")
	require.Equal(t, "completed", got.StepResults["wait_result"].(map[string]any)["status"])
}

func TestCallbackPayloadIsPersistedBeforeAcceptanceAndDeduplicated(t *testing.T) {
	ctx := context.Background()
	sm := state.NewStateManager(state.NewMemoryStore())
	require.NoError(t, sm.Save(ctx, &state.ExecutionState{
		ID: "durable-callback", Gate: "await_callback", CallbackToken: "durable-token",
	}))
	messageBus := bus.New()
	verifierInbox := make(chan bus.Message, 4)
	messageBus.Subscribe("verifier", verifierInbox)
	api := NewAPIAgent(messageBus, messageBus, sm)

	post := func(status string) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/callbacks/durable-token", strings.NewReader(`{"status":"`+status+`"}`))
		res := httptest.NewRecorder()
		api.handleCallback(res, req)
		require.Equal(t, http.StatusOK, res.Code)
		var decoded map[string]any
		require.NoError(t, json.NewDecoder(res.Body).Decode(&decoded))
		return decoded
	}

	first := post("completed")
	require.Equal(t, false, first["duplicate"])
	second := post("different-payload")
	require.Equal(t, true, second["duplicate"])
	stored, err := sm.Load(ctx, "durable-callback")
	require.NoError(t, err)
	require.Equal(t, "completed", stored.CallbackData["status"])
	require.NotNil(t, stored.CallbackAt)
	message := <-verifierInbox
	require.Equal(t, "completed", message.Payload["data"].(map[string]any)["status"])
}

func TestUnavailableAuditJournalMarksExecutionFailed(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/audit.jsonl"
	chain, err := audit.NewFileChain(path)
	require.NoError(t, err)
	require.NoError(t, chain.Close())
	chain.Log(audit.Event{TraceID: "audit-failed", Phase: "tool.started", Result: "pending"})
	require.Error(t, chain.Err())

	sm := state.NewStateManager(state.NewMemoryStore())
	require.NoError(t, sm.Save(ctx, &state.ExecutionState{ID: "audit-failed", Intent: "test.intent", Pipeline: "test"}))
	v := NewVerifier(bus.New(), &config.Config{}, nil, bus.New(), sm)
	v.SetAuditLogger(chain)
	st, err := sm.Load(ctx, "audit-failed")
	require.NoError(t, err)
	logErr := v.logLifecycleAudit(st, "tool.started", "pending", nil)
	require.Error(t, logErr)
	v.failAuditExecution(st, "audit-failed", logErr)
	failed, err := sm.Load(ctx, "audit-failed")
	require.NoError(t, err)
	require.Equal(t, "error", failed.Status)
	require.Contains(t, failed.Error, "audit journal unavailable")
}
