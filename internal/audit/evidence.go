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

// ungovernedModelID groups the evidence of pipelines that run without a governed AI system.
const ungovernedModelID = "aos-agentic-orchestration-system"

// Evidence is the hash-only record AOS sends to Ernest for one audit event, over HTTP or Kafka.
type Evidence struct {
	EventType     string
	SourceEventID string
	Body          []byte
	// CorrelationID (the task id) and Traceparent travel as headers next to the body.
	CorrelationID string
	Traceparent   string
}

// BuildEvidence turns an audit event into Ernest's event shape. eventDigest identifies the event:
// the EvidenceDigest of its record hash, never the hash itself (see EvidenceDigest). aiSystemID
// is the Prudence system the event's pipeline runs as ("" for ungoverned pipelines); extra adds
// metadata fields.
//
// Only identifiers, status and keyed digests leave AOS: prompts, params, user IDs, tool outputs
// and error messages stay in the local audit journal.
func BuildEvidence(event Event, eventDigest, aiSystemID string, extra map[string]any) (Evidence, error) {
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
	sourceEventID := strings.Join([]string{event.TraceID, event.Phase, event.Step, eventDigest}, ":")
	if event.TraceID == "" {
		sourceEventID = eventDigest
	}

	metadata := map[string]any{
		"producer":      "aos-agentic-orchestration-system",
		"schemaVersion": "3",
		"eventDigest":   eventDigest,
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
	if event.TraceID != "" {
		// The business flow, under the suite's name for it.
		metadata["correlationId"] = event.TraceID
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
	payload := map[string]any{
		"source":        "aos",
		"eventType":     eventType,
		"sourceEventId": sourceEventID,
		"occurredAt":    occurredAt,
		"modelId":       ungovernedModelID,
		"metadata":      metadata,
	}
	if aiSystemID != "" {
		// A governed pipeline's evidence belongs to its AI system, next to Prudence's
		// decisions about it. Prudence owns that system (name, risk tier, lifecycle), so AOS
		// names the system of record but sends no name: Ernest keeps the one Prudence gave.
		payload["modelId"] = aiSystemID
		payload["aiSystem"] = map[string]any{"id": aiSystemID, "systemOfRecord": "prudence"}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Evidence{}, fmt.Errorf("marshal Ernest event: %w", err)
	}
	return Evidence{EventType: eventType, SourceEventID: sourceEventID, Body: body,
		CorrelationID: event.TraceID, Traceparent: event.Traceparent}, nil
}
