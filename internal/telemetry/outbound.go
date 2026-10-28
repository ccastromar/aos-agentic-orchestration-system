package telemetry

import (
	"context"
	"net/http"
	"regexp"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Calls to models and tools carry the flow they belong to, so an AI gateway or a tool can tie
// its own log to the AOS run, the Prudence decision and the Ernest evidence: the W3C
// traceparent of the caller's span, the task id and, inside a pipeline, its AI system id.

// CorrelationHeader carries the task id (the correlation id).
const CorrelationHeader = "X-Correlation-Id"

// AISystemHeader carries the Prudence AI system id of the pipeline making the call.
const AISystemHeader = "X-AI-System-Id"

// Plain tokens only: the Decision API accepts at most 128 characters, and nothing a user typed
// can turn into a header.
var headerToken = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

type correlationKey struct{}
type aiSystemKey struct{}

// WithCorrelation marks the calls made with ctx as part of task taskID's flow.
func WithCorrelation(ctx context.Context, taskID string) context.Context {
	return context.WithValue(ctx, correlationKey{}, taskID)
}

// CorrelationID is the correlation id the calls made with ctx carry: a valid token, or "".
func CorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(correlationKey{}).(string)
	return validToken(id)
}

// WithAISystem marks the calls made with ctx as made on behalf of AI system id.
func WithAISystem(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, aiSystemKey{}, id)
}

// AISystemID is the AI system id the calls made with ctx carry: a valid token, or "".
func AISystemID(ctx context.Context) string {
	id, _ := ctx.Value(aiSystemKey{}).(string)
	return validToken(id)
}

// ValidCorrelation is id when it can travel as a correlation id, else "".
func ValidCorrelation(id string) string { return validToken(id) }

func validToken(id string) string {
	if headerToken.MatchString(id) {
		return id
	}
	return ""
}

// OutboundTransport wraps base (nil: http.DefaultTransport) so each request gets a client span,
// the traceparent of the caller's span and the correlation headers of its context. Headers a
// tool definition sets explicitly win.
func OutboundTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return otelhttp.NewTransport(correlationTransport{base})
}

type correlationTransport struct{ base http.RoundTripper }

func (t correlationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	id, system := CorrelationID(req.Context()), AISystemID(req.Context())
	if (id == "" || req.Header.Get(CorrelationHeader) != "") && (system == "" || req.Header.Get(AISystemHeader) != "") {
		return t.base.RoundTrip(req)
	}
	// A RoundTripper must not modify the caller's request.
	req = req.Clone(req.Context())
	if id != "" && req.Header.Get(CorrelationHeader) == "" {
		req.Header.Set(CorrelationHeader, id)
	}
	if system != "" && req.Header.Get(AISystemHeader) == "" {
		req.Header.Set(AISystemHeader, system)
	}
	return t.base.RoundTrip(req)
}
