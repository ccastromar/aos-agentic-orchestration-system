package governance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// Calls to Prudence carry the task as the correlation id (header, and body where the Decision
// API has the field) and the caller's trace context, never a trace derived from the task.
func TestCallsToPrudenceCarryTheTaskAndTheCallersTrace(t *testing.T) {
	noPDPToken(t)
	const task = "4f1c2a5e-9b0d-4c67-8a21-3e5f6d7c8b90"
	type call struct{ traceparent, correlation, bodyCorrelation string }
	var seen []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodyCorrelation, _ := body["correlationId"].(string)
		seen = append(seen, call{r.Header.Get("traceparent"), r.Header.Get(CorrelationHeader), bodyCorrelation})
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/executions" {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_, _ = w.Write([]byte(`{"decision":"ALLOW","decisionId":"d-1"}`))
	}))
	defer srv.Close()

	traceID, _ := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	spanID, _ := trace.SpanIDFromHex("b7ad6b7169203331")
	parent := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	}))

	if _, err := Evaluate(WithCorrelation(parent, task), srv.URL, "aos", "p", "sys-1", "run"); err != nil {
		t.Fatal(err)
	}
	if _, err := reportExecution(parent, srv.URL, ExecutionReport{
		Runtime: "aos", ExecutionID: task, AiSystemID: "sys-1", Pipeline: "p", StartedAt: "2026-10-26T08:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatalf("calls = %d", len(seen))
	}
	for i, c := range seen {
		if !strings.HasPrefix(c.traceparent, "00-"+traceID.String()+"-") {
			t.Errorf("call %d: traceparent %q does not continue the caller's trace", i, c.traceparent)
		}
		if c.correlation != task {
			t.Errorf("call %d: %s = %q", i, CorrelationHeader, c.correlation)
		}
	}
	if seen[0].bodyCorrelation != task {
		t.Errorf("decision body correlationId = %q", seen[0].bodyCorrelation)
	}

	// Outside a task and a trace: neither.
	seen = nil
	if _, err := Evaluate(context.Background(), srv.URL, "aos", "p", "sys-1", "run"); err != nil {
		t.Fatal(err)
	}
	if seen[0] != (call{}) {
		t.Errorf("a call outside a task sent %+v", seen[0])
	}
	// A task id the Decision API would refuse is not sent.
	seen = nil
	if _, err := Evaluate(WithCorrelation(context.Background(), "not a token"), srv.URL, "aos", "p", "sys-1", "run"); err != nil {
		t.Fatal(err)
	}
	if seen[0].correlation != "" || seen[0].bodyCorrelation != "" {
		t.Errorf("an invalid correlation id was sent: %+v", seen[0])
	}
}
