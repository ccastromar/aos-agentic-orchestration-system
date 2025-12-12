package audit

import (
	"encoding/json"
	"log"
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
}

type Logger interface {
	Log(e Event)
}

type StdLogger struct{}

func (StdLogger) Log(e Event) {
	e.Timestamp = e.Timestamp.UTC()
	b, err := json.Marshal(e)
	if err != nil {
		log.Printf("AUDIT marshal error: %v", err)
		return
	}
	log.Printf("AUDIT %s", string(b))
}
