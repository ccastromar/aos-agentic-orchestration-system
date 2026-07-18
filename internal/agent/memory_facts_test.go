package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
)

func TestExtractFacts_FlattensScalarsAndSkipsNoise(t *testing.T) {
	raw := map[string]any{
		"create_ticket": map[string]any{
			"ok":         true,
			"statusCode": 200,
			"ticketId":   "T-123",
			"priority":   "high",
			"amount":     42.5,
			"tags":       []any{"vpn", "urgent"},
		},
	}

	facts := extractFacts(raw)

	want := map[string]string{
		"create_ticket.ticketId": "T-123",
		"create_ticket.priority": "high",
		"create_ticket.amount":   "42.5",
		"create_ticket.tags.0":   "vpn",
		"create_ticket.tags.1":   "urgent",
	}
	if len(facts) != len(want) {
		t.Fatalf("expected %d facts, got %d: %#v", len(want), len(facts), facts)
	}
	for k, v := range want {
		if facts[k] != v {
			t.Errorf("fact %s = %q, want %q", k, facts[k], v)
		}
	}
}

func TestExtractFacts_TruncatesLongValues(t *testing.T) {
	long := strings.Repeat("x", maxFactValueLen+50)
	facts := extractFacts(map[string]any{"step": map[string]any{"description": long}})
	if got := len(facts["step.description"]); got != maxFactValueLen {
		t.Fatalf("expected value truncated to %d chars, got %d", maxFactValueLen, got)
	}
}

func TestExtractFacts_CapsFactCount(t *testing.T) {
	raw := map[string]any{}
	for i := 0; i < maxFacts*2; i++ {
		raw["k"+strings.Repeat("x", i%10)+string(rune('a'+i%26))+string(rune('0'+i/26))] = "v"
	}
	facts := extractFacts(raw)
	if len(facts) > maxFacts {
		t.Fatalf("expected at most %d facts, got %d", maxFacts, len(facts))
	}
}

func TestFactsText_SortedDeterministicOutput(t *testing.T) {
	txt := factsText(map[string]string{"b": "2", "a": "1"})
	if txt != "a: 1\nb: 2\n" {
		t.Fatalf("unexpected facts text: %q", txt)
	}
	if factsText(nil) != "" {
		t.Fatalf("expected empty text for no facts")
	}
}

func TestAnalyst_HandleSummarize_StoresEnrichedMemory(t *testing.T) {
	b := bus.New()
	eventBus := bus.New()
	vs := state.NewMemoryVectorStore()
	sm := state.NewStateManager(state.NewMemoryStore(), vs)
	a := NewAnalyst(b, &fakeLLM{out: "Ticket creado correctamente"}, eventBus, sm)

	id := "task-analyst-enriched"
	// Persist execution state so the Analyst can pick up the pipeline name
	_ = sm.Save(context.Background(), &state.ExecutionState{
		ID:       id,
		Intent:   "helpdesk.create_ticket",
		Pipeline: "pipeline_create_ticket",
	})

	msg := bus.Message{
		Type: "summarize",
		Payload: map[string]any{
			"id":         id,
			"intent":     "helpdesk.create_ticket",
			"session_id": "sess-enriched",
			"rawResult": map[string]any{
				"create_ticket": map[string]any{
					"ok":       true,
					"ticketId": "T-777",
					"subject":  "VPN down",
				},
			},
		},
	}

	a.dispatch(msg)

	mems, err := vs.SearchMemories(context.Background(), "sess-enriched", nil, 10)
	if err != nil {
		t.Fatalf("unexpected error searching memories: %v", err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(mems))
	}
	mem := mems[0]

	for _, want := range []string{
		"Intent: helpdesk.create_ticket",
		"Pipeline: pipeline_create_ticket",
		"Ticket creado correctamente",
		"create_ticket.ticketId: T-777",
		"create_ticket.subject: VPN down",
	} {
		if !strings.Contains(mem.Text, want) {
			t.Errorf("memory text missing %q, got:\n%s", want, mem.Text)
		}
	}

	if mem.Metadata == nil {
		t.Fatalf("expected metadata to be set")
	}
	if mem.Metadata["execution_id"] != id {
		t.Errorf("metadata execution_id = %v, want %s", mem.Metadata["execution_id"], id)
	}
	if mem.Metadata["pipeline"] != "pipeline_create_ticket" {
		t.Errorf("metadata pipeline = %v", mem.Metadata["pipeline"])
	}
	facts, ok := mem.Metadata["facts"].(map[string]string)
	if !ok {
		t.Fatalf("expected facts map in metadata, got %#v", mem.Metadata["facts"])
	}
	if facts["create_ticket.ticketId"] != "T-777" {
		t.Errorf("facts ticketId = %q, want T-777", facts["create_ticket.ticketId"])
	}
}
