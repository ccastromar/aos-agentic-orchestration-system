package agent

import (
	"context"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
	"strings"
	"testing"
)

func TestMemoryRoundTripDoesNotRememberItsOwnOperations(t *testing.T) {
	ctx := context.Background()
	vs := state.NewMemoryVectorStore()
	sm := state.NewStateManager(state.NewMemoryStore(), vs)
	v := &Verifier{stateManager: sm, llmClient: &fakeLLM{}}
	// No chat model: listing and confirming memory operations must work without one.
	a := NewAnalyst(bus.New(), nil, bus.New(), sm)
	out, err := v.executeMemoryTool(ctx, "system.memory.add", "alice", map[string]any{"fact": "my dog's name is Rex"})
	if err != nil {
		t.Fatal(err)
	}
	a.handleSummarize(bus.Message{Payload: map[string]any{"id": "memory-add-roundtrip", "intent": "memory.add", "language": "en", "session_id": "alice", "rawResult": map[string]any{"system.memory.add": out}}})
	for i := 0; i < 3; i++ {
		out, err = v.executeMemoryTool(ctx, "system.memory.list", "alice", nil)
		if err != nil {
			t.Fatal(err)
		}
		a.handleSummarize(bus.Message{Payload: map[string]any{"id": "memory-list-roundtrip", "intent": "memory.list", "language": "en", "session_id": "alice", "rawResult": map[string]any{"system.memory.list": out}}})
		result, ok := getResult("memory-list-roundtrip")
		if !ok {
			t.Fatal("no result")
		}
		summary := result.Data.(map[string]any)["summary"].(string)
		if !strings.Contains(summary, "my dog's name is Rex") {
			t.Fatalf("fact lost in summary: %s", summary)
		}
		memories, _ := vs.ListMemories(ctx, "alice")
		if len(memories) != 1 || memories[0].Text != "my dog's name is Rex" {
			t.Fatalf("memory polluted: %+v", memories)
		}
	}
	// Legacy execution summaries are not personal facts.
	vs.AddMemory(ctx, state.Memory{ID: "legacy", SessionID: "alice", Text: "Intent: memory.list\nAction Summary: done"})
	out, err = v.executeMemoryTool(ctx, "system.memory.list", "alice", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out["memories"].([]string)) != 1 {
		t.Fatal("legacy execution summary leaked into personal memories")
	}
}
