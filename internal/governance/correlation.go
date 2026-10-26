package governance

import (
	"context"
	"regexp"
)

// The business flow a call to Prudence belongs to (contract/observability, section 3): AOS's
// task id. It is the Decision API's correlationId and the X-Correlation-Id header, never a
// trace id: trace context travels as traceparent, from the caller's span.

// CorrelationHeader carries the correlation id on every call to Prudence.
const CorrelationHeader = "X-Correlation-Id"

// The Decision API accepts a plain token of at most 128 characters; anything else is a 400.
var correlationToken = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

type correlationKey struct{}

// WithCorrelation marks the calls made with ctx as part of task taskID's flow.
func WithCorrelation(ctx context.Context, taskID string) context.Context {
	return context.WithValue(ctx, correlationKey{}, taskID)
}

// CorrelationID is the correlation id the calls made with ctx carry: a valid token, or "".
func CorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(correlationKey{}).(string)
	return validCorrelation(id)
}

func validCorrelation(id string) string {
	if correlationToken.MatchString(id) {
		return id
	}
	return ""
}
