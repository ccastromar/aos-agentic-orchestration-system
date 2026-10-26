package audit

import (
	"encoding/json"
	"log"
	"strings"
	"time"
)

type Event struct {
	Timestamp time.Time              `json:"timestamp"`
	UserID    string                 `json:"user_id"`
	Roles     []string               `json:"roles"`
	Intent    string                 `json:"intent"`
	Pipeline  string                 `json:"pipeline"`
	Params    map[string]interface{} `json:"params"`
	Tools     []string               `json:"tools"`
	Result    string                 `json:"result"` // success|denied|error
	Error     string                 `json:"error,omitempty"`
	LatencyMs int64                  `json:"latency_ms"`
	TraceID   string                 `json:"trace_id,omitempty"`
	Phase     string                 `json:"phase,omitempty"`
	Step      string                 `json:"step,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
	// Traceparent is the W3C trace context that wrote the event (contract/observability): kept
	// with it so its delivery to Ernest, later, is a new span linked to it. TraceID, despite
	// its name, is the task id: the business correlation id, not a trace.
	Traceparent string `json:"traceparent,omitempty"`
}

type Logger interface {
	Log(e Event)
}

type ErrorReporter interface {
	Err() error
}

type CheckedLogger interface {
	LogChecked(e Event) error
}

// LogChecked writes an event and immediately returns any sink error when the
// logger supports an atomic write-and-check operation.
func LogChecked(logger Logger, event Event) error {
	if checked, ok := logger.(CheckedLogger); ok {
		return checked.LogChecked(event)
	}
	logger.Log(event)
	return LoggerError(logger)
}

func LoggerError(logger Logger) error {
	if reporter, ok := logger.(ErrorReporter); ok {
		return reporter.Err()
	}
	return nil
}

type StdLogger struct{}

// RedactParams removes common credential fields before the parameters are
// written to stdout or the audit chain.
func RedactParams(params map[string]interface{}) map[string]interface{} {
	redacted := make(map[string]interface{}, len(params))
	for key, value := range params {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "api_key") || strings.Contains(lower, "apikey") || strings.Contains(lower, "authorization") {
			redacted[key] = "[REDACTED]"
			continue
		}
		redacted[key] = value
	}
	return redacted
}

func (StdLogger) Log(e Event) {
	e.Timestamp = e.Timestamp.UTC()
	b, err := json.Marshal(e)
	if err != nil {
		log.Printf("AUDIT marshal error: %v", err)
		return
	}
	log.Printf("AUDIT %s", string(b))
}
