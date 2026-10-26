package governance

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// W3C trace context for the calls AOS makes to Prudence. A task's id is a UUID, which is exactly
// a W3C trace id once the dashes are gone, so the decision and the execution report Prudence
// traces carry the task's id as their trace id: in Tempo, the task id finds Prudence's spans,
// and AOS's log line for the call links to them (trace_id=). AOS records no spans of its own.

type traceKey struct{}

// WithTrace marks the calls made with ctx as part of task taskID's trace.
func WithTrace(ctx context.Context, taskID string) context.Context {
	return context.WithValue(ctx, traceKey{}, taskID)
}

// TraceID is the W3C trace id of a task: its UUID without dashes, or "" if the id is not one.
func TraceID(taskID string) string {
	id := strings.ToLower(strings.ReplaceAll(taskID, "-", ""))
	if len(id) != 32 || strings.Trim(id, "0") == "" {
		return ""
	}
	if _, err := hex.DecodeString(id); err != nil {
		return ""
	}
	return id
}

// traceparent is the header for one call in the task's trace (a fresh parent span id, sampled),
// or "" when the task id is not a UUID.
func traceparent(taskID string) string {
	traceID := TraceID(taskID)
	if traceID == "" {
		return ""
	}
	span := make([]byte, 8)
	if _, err := rand.Read(span); err != nil {
		return ""
	}
	return "00-" + traceID + "-" + hex.EncodeToString(span) + "-01"
}

func traceparentFrom(ctx context.Context) string {
	if id, ok := ctx.Value(traceKey{}).(string); ok {
		return traceparent(id)
	}
	return ""
}
