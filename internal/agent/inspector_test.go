package agent

import (
    "context"
    "testing"
    "time"

    "github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
)

func TestInspector_NewTask_ForwardsToPlanner(t *testing.T) {
    b := bus.New()
    insp := NewInspector(b)

    // Capture what inspector forwards to planner
    plannerCh := make(chan bus.Message, 1)
    b.Subscribe("planner", plannerCh)

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    go func() { _ = insp.Start(ctx) }()

    // Send a new_task into inspector's inbox
    id := "task-123"
    insp.Inbox() <- bus.Message{
        Type: "new_task",
        Payload: map[string]any{
            "id":      id,
            "mode":    "structured",
            "message": "check balance",
        },
    }

    select {
    case msg := <-plannerCh:
        if msg.Type != "detect_intent" {
            t.Fatalf("expected detect_intent, got %s", msg.Type)
        }
        if msg.Payload["id"].(string) != id {
            t.Fatalf("forwarded id mismatch: %v", msg.Payload["id"])
        }
        if msg.Payload["message"].(string) != "check balance" {
            t.Fatalf("missing/incorrect message in forwarded payload: %#v", msg.Payload)
        }
    case <-time.After(500 * time.Millisecond):
        t.Fatal("timeout waiting forwarded message to planner")
    }
}
