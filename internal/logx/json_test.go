package logx

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// withJSON captures the JSON lines written while fn runs.
func withJSON(t *testing.T, level string, fn func()) []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	mu.Lock()
	prevLevel, prevOut, prevLookup := minLevel, jsonOut, taskContext
	minLevel = parseLevel(level)
	jsonOut = NewJSONLogger(&buf, Config{Version: "0.4.0", Environment: "staging"})
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		minLevel, jsonOut, taskContext = prevLevel, prevOut, prevLookup
		mu.Unlock()
	})
	fn()
	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if raw == "" {
			continue
		}
		var line map[string]any
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatalf("not one JSON object per line: %q", raw)
		}
		lines = append(lines, line)
	}
	return lines
}

func spanContext() context.Context {
	traceID, _ := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	spanID, _ := trace.SpanIDFromHex("b7ad6b7169203331")
	return trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	}))
}

func TestALineHasTheSuitesFields(t *testing.T) {
	lines := withJSON(t, "info", func() { Info("Planner", "intent=%s", "credit.prescreen") })
	if len(lines) != 1 {
		t.Fatalf("lines = %v", lines)
	}
	line := lines[0]
	if !regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}Z$`).MatchString(line["timestamp"].(string)) {
		t.Errorf("timestamp = %v", line["timestamp"])
	}
	want := map[string]any{
		"severity": "INFO", "message": "intent=credit.prescreen", "component": "Planner",
		"service.name": "aos", "service.version": "0.4.0", "deployment.environment.name": "staging",
	}
	for k, v := range want {
		if line[k] != v {
			t.Errorf("%s = %v, want %v", k, line[k], v)
		}
	}
	for _, old := range []string{"time", "level", "msg"} {
		if _, ok := line[old]; ok {
			t.Errorf("field %q should be renamed", old)
		}
	}
}

// Debug may hold what users type: below the configured level it is not written at all.
func TestTheLevelIsApplied(t *testing.T) {
	lines := withJSON(t, "info", func() {
		Debug("Planner", "msg='%s'", "my salary is 3000")
		Warn("Planner", "slow")
	})
	if len(lines) != 1 || lines[0]["severity"] != "WARN" {
		t.Fatalf("lines = %v", lines)
	}
	if lines := withJSON(t, "debug", func() { Debug("Planner", "x") }); len(lines) != 1 || lines[0]["severity"] != "DEBUG" {
		t.Fatalf("debug lines = %v", lines)
	}
}

func TestATasksLinesCarryItsCorrelationAndTrace(t *testing.T) {
	lines := withJSON(t, "info", func() {
		SetTaskContext(func(id string) (context.Context, bool) { return spanContext(), id == "task-7" })
		L("task-7", "Governance", "asking Prudence")
		L("task-8", "Governance", "no trace")
	})
	if lines[0]["correlation_id"] != "task-7" || lines[0]["trace_id"] != "0af7651916cd43dd8448eb211c80319c" || lines[0]["span_id"] != "b7ad6b7169203331" {
		t.Errorf("line = %v", lines[0])
	}
	if lines[1]["correlation_id"] != "task-8" || lines[1]["trace_id"] != nil {
		t.Errorf("line = %v", lines[1])
	}
}

func TestANamedEvent(t *testing.T) {
	lines := withJSON(t, "info", func() {
		Event(spanContext(), slog.LevelError, "Audit", "audit.write_failed", "audit record not written",
			"outcome", "failure", "error.type", "os.PathError")
	})
	line := lines[0]
	for k, v := range map[string]any{
		"event.name": "audit.write_failed", "severity": "ERROR", "outcome": "failure",
		"error.type": "os.PathError", "trace_id": "0af7651916cd43dd8448eb211c80319c", "component": "Audit",
	} {
		if line[k] != v {
			t.Errorf("%s = %v, want %v", k, line[k], v)
		}
	}
}

func TestJSONIsTheDefaultOutsideDevelopment(t *testing.T) {
	mu.Lock()
	prevLevel, prevOut := minLevel, jsonOut
	mu.Unlock()
	prevDefault := slog.Default()
	t.Cleanup(func() {
		mu.Lock()
		minLevel, jsonOut = prevLevel, prevOut
		mu.Unlock()
		slog.SetDefault(prevDefault)
	})
	Configure(Config{Development: true})
	if _, out := current(); out != nil {
		t.Error("development logs JSON by default")
	}
	Configure(Config{})
	if _, out := current(); out == nil {
		t.Error("production logs text by default")
	}
	Configure(Config{Format: "text"})
	if _, out := current(); out != nil {
		t.Error("LOG_FORMAT=text ignored")
	}
}
