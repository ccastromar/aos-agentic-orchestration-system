package bus

import "github.com/ccastromar/aos-agentic-orchestration-system/internal/metrics"

type Message struct {
    Type    string
    Payload map[string]any
}

type Bus struct {
    subs map[string]chan Message
}

func New() *Bus {
    return &Bus{
        subs: make(map[string]chan Message),
    }
}

func (b *Bus) Subscribe(name string, ch chan Message) {
    b.subs[name] = ch
}

func (b *Bus) Send(target string, msg Message) {
    if ch, ok := b.subs[target]; ok {
        // Non-blocking send to avoid deadlocks during shutdown or slow consumers
        select {
        case ch <- msg:
            metrics.BusMessages.Inc(map[string]string{"target": target, "result": "sent"})
        default:
            // drop message if receiver is not ready; best-effort bus
            metrics.BusMessages.Inc(map[string]string{"target": target, "result": "dropped"})
        }
    }
}
