package audit

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PhasePipelineStarted is the lifecycle phase recorded once a pipeline actually starts, i.e.
// after any governance gate allowed it. For a governed pipeline it is the one execution of
// the AI system that Ernest counts (and Prudence's drift detection reads).
const PhasePipelineStarted = "pipeline.started"

// Evidence is the hash-only record AOS sends to Ernest for one audit event, over HTTP or Kafka.
type Evidence struct {
	EventType     string
	SourceEventID string
	Body          []byte
}

// BuildEvidence turns an audit event into Ernest's event shape. eventHash identifies the event
// (the journal record hash when shipping from the journal). aiSystemID is the Prudence system
// the event's pipeline runs as ("" for ungoverned pipelines); extra adds metadata fields.
//
// Only identifiers, status and digests leave AOS: prompts, params, user IDs, tool outputs and
// error messages stay in the local audit journal.
func BuildEvidence(event Event, eventHash, aiSystemID string, extra map[string]any) (Evidence, error) {
	ts := event.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	// Millisecond RFC 3339 in UTC parses everywhere (Mongo dates, Java Instant, JS Date).
	occurredAt := ts.UTC().Format("2006-01-02T15:04:05.000Z07:00")

	eventType := "aos.audit.event"
	if phase := strings.Trim(strings.TrimSpace(event.Phase), "."); phase != "" {
		eventType += "." + phase
	}
	sourceEventID := strings.Join([]string{event.TraceID, event.Phase, event.Step, eventHash}, ":")
	if event.TraceID == "" {
		sourceEventID = eventHash
	}

	metadata := map[string]any{
		"producer":      "aos-agentic-orchestration-system",
		"schemaVersion": "2",
		"eventHash":     eventHash,
		"phase":         event.Phase,
		"traceId":       event.TraceID,
		"intent":        event.Intent,
		"pipeline":      event.Pipeline,
		"result":        event.Result,
		"latencyMs":     event.LatencyMs,
		"occurredAt":    occurredAt,
		"stage":         "lifecycle",
	}
	if event.Step != "" {
		metadata["step"] = event.Step
	}
	if aiSystemID != "" {
		metadata["aiSystemId"] = aiSystemID
		if event.Phase == PhasePipelineStarted {
			metadata["stage"] = "execution"
		}
	}
	for k, v := range extra {
		metadata[k] = v
	}
	body, err := json.Marshal(map[string]any{
		"source":        "aos",
		"eventType":     eventType,
		"sourceEventId": sourceEventID,
		"occurredAt":    occurredAt,
		"modelId":       "aos-agentic-orchestration-system",
		"metadata":      metadata,
	})
	if err != nil {
		return Evidence{}, fmt.Errorf("marshal Ernest event: %w", err)
	}
	return Evidence{EventType: eventType, SourceEventID: sourceEventID, Body: body}, nil
}
