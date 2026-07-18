package agent

import (
	"context"
	"testing"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
)

func seedExecution(t *testing.T, sm *state.StateManager, id, session, intent, pipeline string, createdAt time.Time, stepResults map[string]any) {
	t.Helper()
	err := sm.Save(context.Background(), &state.ExecutionState{
		ID:          id,
		SessionID:   session,
		Intent:      intent,
		Pipeline:    pipeline,
		CreatedAt:   createdAt,
		StepResults: stepResults,
	})
	if err != nil {
		t.Fatalf("seeding execution %s: %v", id, err)
	}
}

func TestBuildRecallResult_FiltersSessionAndExcludesCurrent(t *testing.T) {
	sm := state.NewStateManager(state.NewMemoryStore())
	now := time.Now()

	seedExecution(t, sm, "ex-1", "sess-A", "helpdesk.create_ticket", "pipeline_support_create", now.Add(-2*time.Hour), map[string]any{
		"create_ticket": map[string]any{"ok": true, "ticketId": "T-777"},
		"_pipeline":     map[string]any{"summary": "Ticket T-777 creado", "raw": map[string]any{}},
	})
	seedExecution(t, sm, "ex-2", "sess-A", "banking.get_balance", "pipeline_get_balance", now.Add(-1*time.Hour), map[string]any{
		"get_balance": map[string]any{"balance": 15.56, "currency": "EUR"},
	})
	seedExecution(t, sm, "ex-other", "sess-B", "crm.get_customer_profile", "pipeline_crm", now, nil)
	seedExecution(t, sm, "ex-current", "sess-A", "system.recall_history", "pipeline_recall_history", now, nil)

	res := buildRecallResult(context.Background(), sm, "sess-A", "ex-current")

	if res["count"] != 2 {
		t.Fatalf("expected 2 executions, got %v: %#v", res["count"], res)
	}
	execs := res["executions"].([]map[string]any)

	// Newest first: banking before helpdesk
	if execs[0]["intent"] != "banking.get_balance" || execs[1]["intent"] != "helpdesk.create_ticket" {
		t.Fatalf("unexpected order: %v, %v", execs[0]["intent"], execs[1]["intent"])
	}

	facts := execs[0]["facts"].(map[string]string)
	if facts["get_balance.balance"] != "15.56" {
		t.Errorf("expected balance fact, got %#v", facts)
	}

	if execs[1]["summary"] != "Ticket T-777 creado" {
		t.Errorf("expected summary from _pipeline, got %#v", execs[1]["summary"])
	}
	ticketFacts := execs[1]["facts"].(map[string]string)
	if ticketFacts["create_ticket.ticketId"] != "T-777" {
		t.Errorf("expected ticketId fact, got %#v", ticketFacts)
	}
}

func TestBuildRecallResult_CapsAndNilManager(t *testing.T) {
	sm := state.NewStateManager(state.NewMemoryStore())
	now := time.Now()
	for i := 0; i < maxRecallExecutions+5; i++ {
		seedExecution(t, sm, "ex-"+string(rune('a'+i)), "sess-C", "x", "p", now.Add(time.Duration(-i)*time.Minute), nil)
	}
	res := buildRecallResult(context.Background(), sm, "sess-C", "none")
	if res["count"] != maxRecallExecutions {
		t.Fatalf("expected cap at %d, got %v", maxRecallExecutions, res["count"])
	}

	nilRes := buildRecallResult(context.Background(), nil, "sess-C", "none")
	if nilRes["count"] != 0 {
		t.Fatalf("expected 0 for nil manager, got %v", nilRes["count"])
	}
}

func TestVerifier_SystemRecall_SendsHistoryToAnalyst(t *testing.T) {
	sm := state.NewStateManager(state.NewMemoryStore())
	seedExecution(t, sm, "ex-past", "sess-R", "helpdesk.create_ticket", "pipeline_support_create", time.Now().Add(-time.Hour), map[string]any{
		"create_ticket": map[string]any{"ticketId": "T-42"},
	})

	cfg := &config.Config{Tools: map[string]config.Tool{}}
	pipe := config.Pipeline{
		Name: "pipeline_recall_history",
		Steps: []config.PipelineStep{
			{Tool: "system.recall"},
			{Analyst: true},
		},
	}

	b := bus.New()
	v := NewVerifier(b, cfg, nil, bus.New(), sm)

	analystCh := make(chan bus.Message, 1)
	b.Subscribe("analyst", analystCh)

	v.dispatch(bus.Message{
		Type: "run_pipeline",
		Payload: map[string]any{
			"id":         "id-recall",
			"session_id": "sess-R",
			"intent":     "system.recall_history",
			"pipeline":   pipe,
			"params":     map[string]string{},
		},
	})

	select {
	case msg := <-analystCh:
		raw, ok := msg.Payload["rawResult"].(map[string]any)
		if !ok {
			t.Fatalf("expected rawResult map, got %#v", msg.Payload["rawResult"])
		}
		recall, ok := raw["system.recall"].(map[string]any)
		if !ok {
			t.Fatalf("expected system.recall result, got %#v", raw)
		}
		if recall["count"] != 1 {
			t.Fatalf("expected 1 recalled execution, got %v", recall["count"])
		}
		execs := recall["executions"].([]map[string]any)
		if execs[0]["intent"] != "helpdesk.create_ticket" {
			t.Errorf("unexpected recalled intent: %v", execs[0]["intent"])
		}
		facts := execs[0]["facts"].(map[string]string)
		if facts["create_ticket.ticketId"] != "T-42" {
			t.Errorf("expected ticketId fact in recall, got %#v", facts)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for analyst message")
	}
}
