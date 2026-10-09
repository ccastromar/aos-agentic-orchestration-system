package audit

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
)

func TestPipelineSnapshotIsStableAndRedactsCredentials(t *testing.T) {
	cfg := &config.Config{
		Tools:  map[string]config.Tool{"demo.call": {Name: "demo.call", URL: "https://example.test/events?token=private", Headers: map[string]string{"Authorization": "Bearer private"}}},
		Agents: map[string]config.AgentDefinition{},
	}
	pipe := config.Pipeline{Name: "demo", Mode: "linear", Steps: []config.PipelineStep{{ID: "call", Tool: "demo.call"}}}
	first, version, err := BuildPipelineSnapshot(pipe, cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, secondVersion, err := BuildPipelineSnapshot(pipe, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if version != secondVersion || string(first) != string(second) {
		t.Fatalf("snapshot is not stable: %s != %s", first, second)
	}
	if strings.Contains(string(first), "private") || !strings.Contains(string(first), "[REDACTED]") {
		t.Fatalf("snapshot did not safely redact credentials: %s", first)
	}
	var decoded map[string]any
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyExecutionFileChecksManifestAndStepDigests(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	chain, err := NewFileChain(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := json.RawMessage(`{"pipeline":{"name":"demo"}}`)
	inputs := map[string]any{"amount": "2"}
	version := Digest(snapshot)
	chain.Log(Event{TraceID: "trace-1", Phase: "pipeline.started", Result: "success", Details: map[string]any{
		"pipeline_version": version, "pipeline_snapshot": snapshot,
		"input_digest": Digest(inputs), "inputs": inputs,
	}})
	callInputs := map[string]any{"amount": "2"}
	chain.Log(Event{TraceID: "trace-1", Phase: "tool.started", Result: "pending", Details: map[string]any{
		"input_digest": Digest(callInputs), "inputs": callInputs,
	}})
	output := map[string]any{"status": "completed", "operationId": "op-1"}
	chain.Log(Event{TraceID: "trace-1", Phase: "tool.completed", Result: "success", Details: map[string]any{
		"result": output, "output_digest": Digest(output),
	}})
	if err := chain.Close(); err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyExecutionFile(path, "trace-1")
	if err != nil || !verified.Valid || verified.ToolCalls != 1 {
		t.Fatalf("execution verification = %#v, err=%v", verified, err)
	}

	chain, err = NewFileChain(path)
	if err != nil {
		t.Fatal(err)
	}
	chain.Log(Event{TraceID: "trace-2", Phase: "pipeline.started", Result: "success", Details: map[string]any{
		"pipeline_version": version, "pipeline_snapshot": snapshot,
		"input_digest": "sha256:wrong", "inputs": inputs,
	}})
	_ = chain.Close()
	if _, err := VerifyExecutionFile(path, "trace-2"); err == nil {
		t.Fatal("expected bad input digest to fail execution verification")
	}
}
