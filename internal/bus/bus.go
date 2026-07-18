package bus

import (
	"sync"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/metrics"
)

type Message struct {
	Type    string
	Payload map[string]any
}

type Bus struct {
	mu   sync.RWMutex
	subs map[string]map[chan Message]struct{}
}

func New() *Bus {
	return &Bus{
		subs: make(map[string]map[chan Message]struct{}),
	}
}

// Subscribe adds a channel to receive messages for the given target topic
func (b *Bus) Subscribe(target string, ch chan Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subs[target] == nil {
		b.subs[target] = make(map[chan Message]struct{})
	}
	b.subs[target][ch] = struct{}{}
}

// Unsubscribe removes a channel from a topic
func (b *Bus) Unsubscribe(target string, ch chan Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subs[target] != nil {
		delete(b.subs[target], ch)
	}
}

func (b *Bus) Send(target string, msg Message) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if channels, ok := b.subs[target]; ok {
		for ch := range channels {
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
}

// PublishEvent is a helper method to emit system_events without manually building the payload map
func (b *Bus) PublishEvent(taskID, agent, kind, msg, duration string) {
	b.Send("system_events", Message{
		Type: "agent_event",
		Payload: map[string]any{
			"task_id":  taskID,
			"agent":    agent,
			"kind":     kind,
			"message":  msg,
			"duration": duration,
		},
	})
}

// PublishEventWithData is a helper method to emit system_events with extra data payload
func (b *Bus) PublishEventWithData(taskID, agent, kind, msg, duration, data string) {
	b.Send("system_events", Message{
		Type: "agent_event",
		Payload: map[string]any{
			"task_id":  taskID,
			"agent":    agent,
			"kind":     kind,
			"message":  msg,
			"duration": duration,
			"data":     data,
		},
	})
}
