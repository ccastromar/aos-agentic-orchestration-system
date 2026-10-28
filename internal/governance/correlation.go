package governance

import (
	"context"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/telemetry"
)

// The business flow a call to Prudence belongs to: AOS's
// task id. It is the Decision API's correlationId and the X-Correlation-Id header, never a
// trace id: trace context travels as traceparent, from the caller's span.

// CorrelationHeader carries the correlation id on every call to Prudence.
const CorrelationHeader = telemetry.CorrelationHeader

// WithCorrelation marks the calls made with ctx as part of task taskID's flow.
func WithCorrelation(ctx context.Context, taskID string) context.Context {
	return telemetry.WithCorrelation(ctx, taskID)
}

// CorrelationID is the correlation id the calls made with ctx carry: a valid token, or "".
func CorrelationID(ctx context.Context) string {
	return telemetry.CorrelationID(ctx)
}
