package agent

import (
    "context"
    "testing"
    "time"

    "github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
    "github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
)

// llmNewTaskDummy causes DetectIntent to return a key not present in cfg
type llmNewTaskDummy struct{}

func (llmNewTaskDummy) Ping(ctx context.Context) error { return nil }
func (f llmNewTaskDummy) Embed(ctx context.Context, text string) ([]float32, error) { return []float32{}, nil }
func (f llmNewTaskDummy) Chat(ctx context.Context, prompt string) (string, error) {
    return "unknown.intent", nil
}

func TestPlanner_NewTask_DispatchesDetectIntent(t *testing.T) {
    b := bus.New()
    // Minimal cfg and UI not used in this path
    cfg := &config.Config{Intents: map[string]config.Intent{}}
    p := NewPlanner(b, cfg, llmNewTaskDummy{}, nil)

    // Capture messages sent to "planner"
    plannerCh := make(chan bus.Message, 1)
    b.Subscribe("planner", plannerCh)

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    go func() { _ = p.Start(ctx) }()

    // Send a new_task into the planner's inbox
    id := "abc-123"
    p.Inbox() <- bus.Message{
        Type: "new_task",
        Payload: map[string]any{
            "id":      id,
            "message": "saldo",
            "mode":    "structured",
        },
    }

    select {
    case msg := <-plannerCh:
        if msg.Type != "detect_intent" {
            t.Fatalf("expected detect_intent, got %s", msg.Type)
        }
        if msg.Payload["id"].(string) != id {
            t.Fatalf("id mismatch in forwarded message")
        }
        if msg.Payload["message"].(string) != "saldo" {
            t.Fatalf("message not forwarded correctly: %#v", msg.Payload)
        }
    case <-time.After(500 * time.Millisecond):
        t.Fatal("timeout waiting detect_intent dispatch")
    }
}

func TestPlanner_HandleDetectIntent_UnknownIntentStoresError(t *testing.T) {
    // Config without the returned intent ensures DetectIntent validation fails
    cfg := &config.Config{Intents: map[string]config.Intent{}}

    b := bus.New()
    p := NewPlanner(b, cfg, llmNewTaskDummy{}, nil)

    // Call handleDetectIntent through the dispatch loop
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    go func() { _ = p.Start(ctx) }()

    id := "task-err"
    p.Inbox() <- bus.Message{
        Type: "detect_intent",
        Payload: map[string]any{
            "id":      id,
            "message": "some msg",
        },
    }

    // Wait until result is stored
    deadline := time.Now().Add(1 * time.Second)
    for time.Now().Before(deadline) {
        time.Sleep(20 * time.Millisecond)
        resultsMu.Lock()
        res, ok := results[id]
        resultsMu.Unlock()
        if ok {
            if res.Status != "error" || res.Err == "" {
                t.Fatalf("expected error result stored, got: %+v", res)
            }
            return
        }
    }
    t.Fatal("timeout waiting for error result to be stored")
}
