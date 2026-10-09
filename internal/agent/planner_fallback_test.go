package agent

import (
	"context"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
	"testing"
	"time"
)

type fallbackLLM struct{ llmNewTaskDummy }

func (fallbackLLM) Chat(context.Context, string) (string, error) {
	return `{"intent":"","parameters":{},"errors":[],"new_facts":[]}`, nil
}
func TestUnknownIntentDispatchesDeclaredFallback(t *testing.T) {
	b := bus.New()
	ch := make(chan bus.Message, 1)
	b.Subscribe("verifier", ch)
	cfg := &config.Config{
		Intents:   map[string]config.Intent{"system.fallback": {Type: "system.fallback", Pipeline: "fallback"}},
		Pipelines: map[string]config.Pipeline{"fallback": {Name: "fallback"}},
	}
	p := NewPlanner(b, cfg, fallbackLLM{}, bus.New(), state.NewStateManager(state.NewMemoryStore()))
	p.handleDetectIntent(bus.Message{Payload: map[string]any{"id": "fallback-test", "message": "hello", "lang": "en"}})
	select {
	case msg := <-ch:
		if msg.Payload["intent"] != "system.fallback" {
			t.Fatalf("wrong intent: %v", msg.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("fallback did not reach verifier")
	}
}
