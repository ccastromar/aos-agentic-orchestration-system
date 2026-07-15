package agent

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
    "github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
    "github.com/ccastromar/aos-agentic-orchestration-system/internal/ui"
)

// helper to wait for a stored result with timeout
func waitStoredResult(t *testing.T, id string, d time.Duration) Result {
    t.Helper()
    deadline := time.Now().Add(d)
    for time.Now().Before(deadline) {
        time.Sleep(20 * time.Millisecond)
        resultsMu.Lock()
        r, ok := results[id]
        resultsMu.Unlock()
        if ok {
            return r
        }
    }
    t.Fatalf("timeout waiting stored result for id=%s", id)
    return Result{}
}

func TestVerifier_RunPipeline_SendsToAnalyst(t *testing.T) {
    // Mock HTTP service that returns JSON for the tool
    ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        _ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
    }))
    defer ts.Close()

    // Config with one tool and then an analyst step
    cfg := &config.Config{
        Tools: map[string]config.Tool{
            "t1": {Name: "t1", Type: "http", Method: "GET", URL: ts.URL + "/x", TimeoutMs: 500},
        },
    }
    pipe := config.Pipeline{
        Name: "p1",
        Steps: []config.PipelineStep{
            {Tool: "t1"},
            {Analyst: true},
        },
    }

    b := bus.New()
    v := NewVerifier(b, cfg, nil, ui.NewUIStore())

    // Capture message sent to analyst
    analystCh := make(chan bus.Message, 1)
    b.Subscribe("analyst", analystCh)

    // Send run_pipeline directly via dispatch
    v.dispatch(bus.Message{
        Type: "run_pipeline",
        Payload: map[string]any{
            "id":       "id-1",
            "intent":   "banking.get_balance",
            "pipeline": pipe,
            "params":   map[string]string{},
        },
    })

    select {
    case msg := <-analystCh:
        if msg.Type != "summarize" {
            t.Fatalf("expected summarize, got %s", msg.Type)
        }
        if msg.Payload["id"].(string) != "id-1" {
            t.Fatalf("unexpected id in summarize payload: %#v", msg.Payload)
        }
        if _, ok := msg.Payload["rawResult"]; !ok {
            t.Fatalf("expected rawResult in summarize payload")
        }
    case <-time.After(1 * time.Second):
        t.Fatal("timeout waiting message to analyst")
    }
}

func TestVerifier_MissingTool_StoresError(t *testing.T) {
    cfg := &config.Config{Tools: map[string]config.Tool{}}
    pipe := config.Pipeline{Name: "p2", Steps: []config.PipelineStep{{Tool: "nonexistent"}}}

    b := bus.New()
    v := NewVerifier(b, cfg, nil, ui.NewUIStore())

    id := "id-err"
    v.dispatch(bus.Message{
        Type: "run_pipeline",
        Payload: map[string]any{
            "id":       id,
            "intent":   "x",
            "pipeline": pipe,
        },
    })

    res := waitStoredResult(t, id, 1*time.Second)
    if res.Status != "error" || res.Err == "" {
        t.Fatalf("expected error result stored, got: %+v", res)
    }
}
