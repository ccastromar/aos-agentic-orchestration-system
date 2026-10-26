package governance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

var traceparentFormat = regexp.MustCompile(`^00-([0-9a-f]{32})-[0-9a-f]{16}-01$`)

func TestTraceIDIsTheTaskUUIDWithoutDashes(t *testing.T) {
	if got := TraceID("4F1C2A5E-9B0D-4C67-8A21-3E5F6D7C8B90"); got != "4f1c2a5e9b0d4c678a213e5f6d7c8b90" {
		t.Fatalf("TraceID = %q", got)
	}
	for _, notUUID := range []string{"", "trace-7f3a9c", "00000000-0000-0000-0000-000000000000", "zz1c2a5e-9b0d-4c67-8a21-3e5f6d7c8b90"} {
		if got := TraceID(notUUID); got != "" {
			t.Errorf("TraceID(%q) = %q, want none", notUUID, got)
		}
	}
}

// Prudence continues the task's trace: the decision and the execution report carry it.
func TestCallsToPrudenceCarryTheTasksTrace(t *testing.T) {
	noPDPToken(t)
	const task = "4f1c2a5e-9b0d-4c67-8a21-3e5f6d7c8b90"
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("traceparent"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/executions" {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_, _ = w.Write([]byte(`{"decision":"ALLOW","decisionId":"d-1"}`))
	}))
	defer srv.Close()

	if _, err := Evaluate(WithTrace(context.Background(), task), srv.URL, "aos", "p", "sys-1", "run"); err != nil {
		t.Fatal(err)
	}
	if _, err := reportExecution(context.Background(), srv.URL, ExecutionReport{
		Runtime: "aos", ExecutionID: task, AiSystemID: "sys-1", Pipeline: "p", StartedAt: "2026-10-26T08:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatalf("calls = %d", len(seen))
	}
	for _, tp := range seen {
		m := traceparentFormat.FindStringSubmatch(tp)
		if m == nil || m[1] != TraceID(task) {
			t.Errorf("traceparent %q does not carry the task's trace", tp)
		}
	}

	seen = nil
	if _, err := Evaluate(context.Background(), srv.URL, "aos", "p", "sys-1", "run"); err != nil {
		t.Fatal(err)
	}
	if seen[0] != "" {
		t.Errorf("a call outside a task sent traceparent %q", seen[0])
	}
}
