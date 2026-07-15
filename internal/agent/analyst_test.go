package agent

import (
    "context"
    "errors"
    "testing"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"

    "github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
    "github.com/ccastromar/aos-agentic-orchestration-system/internal/llm"
    "github.com/ccastromar/aos-agentic-orchestration-system/internal/ui"
)

// fakeLLM implements llm.LLMClient for testing Analyst
type fakeLLM struct{
    out string
    err error
}

func (f *fakeLLM) Ping(ctx context.Context) error { return nil }
func (f *fakeLLM) Embed(ctx context.Context, text string) ([]float32, error) {
	return []float32{1.0, 2.0, 3.0}, nil
}

func (f *fakeLLM) Chat(ctx context.Context, prompt string) (string, error) { return f.out, f.err }

var _ llm.LLMClient = (*fakeLLM)(nil)

func TestAnalyst_HandleSummarize_InvalidRawStoresError(t *testing.T) {
    b := bus.New()
    uiStore := ui.NewUIStore()
    a := NewAnalyst(b, &fakeLLM{}, uiStore, state.NewStateManager(state.NewMemoryStore()))

    id := "task-analyst-1"
    // rawResult is not a map[string]any → should store error
    msg := bus.Message{
        Type: "summarize",
        Payload: map[string]any{
            "id":        id,
            "intent":    "banking.get_balance",
            "rawResult": "not-a-map",
        },
    }

    // Call dispatch synchronously
    a.dispatch(msg)

    resultsMu.Lock()
    res, ok := results[id]
    resultsMu.Unlock()
    if !ok {
        t.Fatalf("expected result to be stored for id=%s", id)
    }
    if res.Status != "error" {
        t.Fatalf("expected status=error, got %s", res.Status)
    }
    if res.Err == "" {
        t.Fatalf("expected error message to be set")
    }
}

func TestAnalyst_HandleSummarize_LLMError_DegradesToRaw(t *testing.T) {
    b := bus.New()
    uiStore := ui.NewUIStore()
    a := NewAnalyst(b, &fakeLLM{err: errors.New("down")}, uiStore, state.NewStateManager(state.NewMemoryStore()))

    id := "task-analyst-2"
    raw := map[string]any{"ok": true}
    msg := bus.Message{
        Type: "summarize",
        Payload: map[string]any{
            "id":        id,
            "intent":    "banking.get_balance",
            "rawResult": raw,
        },
    }

    a.dispatch(msg)

    resultsMu.Lock()
    res := results[id]
    resultsMu.Unlock()

    if res.Status != "ok" {
        t.Fatalf("expected status=ok on degrade path, got %s", res.Status)
    }
    data, ok := res.Data.(map[string]any)
    if !ok {
        t.Fatalf("expected Data to be map, got %#v", res.Data)
    }
    if _, has := data["summary"]; has {
        t.Fatalf("did not expect summary on degrade path, got: %#v", data)
    }
    if gotRaw, has := data["raw"]; !has {
        t.Fatalf("expected raw to be present in data")
    } else if gotRaw.(map[string]any)["ok"].(bool) != true {
        t.Fatalf("unexpected raw content: %#v", gotRaw)
    }
}

func TestAnalyst_HandleSummarize_SuccessStoresSummary(t *testing.T) {
    b := bus.New()
    uiStore := ui.NewUIStore()
    sm := state.NewStateManager(state.NewMemoryStore())
    a := NewAnalyst(b, &fakeLLM{out: "Resumen breve"}, uiStore, sm)

    id := "task-analyst-3"
    raw := map[string]any{"balance": 123.0}
    msg := bus.Message{
        Type: "summarize",
        Payload: map[string]any{
            "id":        id,
            "intent":    "banking.get_balance",
            "rawResult": raw,
        },
    }

    a.dispatch(msg)

    resultsMu.Lock()
    res := results[id]
    resultsMu.Unlock()

    if res.Status != "completed" {
        t.Fatalf("expected status=completed, got %s", res.Status)
    }
    data, ok := res.Data.(map[string]any)
    if !ok {
        t.Fatalf("expected Data to be map, got %#v", res.Data)
    }
    if data["summary"] != "Resumen breve" {
        t.Fatalf("expected summary to be stored, got: %#v", data["summary"])
    }
    if rawGot, ok := data["raw"].(map[string]any); !ok || rawGot["balance"].(float64) != 123.0 {
        t.Fatalf("expected raw to be present and correct, got: %#v", data["raw"]) 
    }
}
