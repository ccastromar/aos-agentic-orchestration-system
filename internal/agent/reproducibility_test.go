package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/audit"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
	"github.com/stretchr/testify/require"
)

type captureAudit struct{ events []audit.Event }

func (c *captureAudit) Log(event audit.Event) { c.events = append(c.events, event) }

func TestPipelineManifestPersistsSnapshotAndRejectsChangedDefinition(t *testing.T) {
	ctx := context.Background()
	sm := state.NewStateManager(state.NewMemoryStore())
	pipe := config.Pipeline{Name: "demo", Mode: "linear", Steps: []config.PipelineStep{{ID: "read", Tool: "workspace.search"}}}
	cfg := &config.Config{
		Pipelines: map[string]config.Pipeline{pipe.Name: pipe},
		Tools:     map[string]config.Tool{"workspace.search": {Name: "workspace.search", Headers: map[string]string{"Authorization": "Bearer hidden"}}},
		Agents:    map[string]config.AgentDefinition{},
	}
	require.NoError(t, sm.Save(ctx, &state.ExecutionState{ID: "trace-1", UserID: "alice", Intent: "workspace.search", Pipeline: pipe.Name, Params: map[string]string{"query": "needle"}}))
	v := NewVerifier(bus.New(), cfg, nil, bus.New(), sm)
	logger := &captureAudit{}
	v.SetAuditLogger(logger)

	initial, err := sm.Load(ctx, "trace-1")
	require.NoError(t, err)
	manifest, err := v.ensurePipelineManifest("trace-1", "session-1", "workspace.search", pipe, map[string]any{"query": "needle"}, initial)
	require.NoError(t, err)
	require.NotEmpty(t, manifest.PipelineVersion)
	require.NotEmpty(t, manifest.InputDigest)
	require.Contains(t, string(manifest.PipelineSnapshot), "[REDACTED]")
	require.NotContains(t, string(manifest.PipelineSnapshot), "hidden")
	require.Len(t, logger.events, 1)
	require.Equal(t, "pipeline.started", logger.events[0].Phase)
	var persisted state.ExecutionState
	encoded, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(encoded, &persisted))
	require.Equal(t, manifest.PipelineVersion, persisted.PipelineVersion)

	changed := pipe
	changed.Steps = append([]config.PipelineStep(nil), pipe.Steps...)
	changed.Steps[0].Tool = "workspace.write_file"
	_, err = v.ensurePipelineManifest("trace-1", "session-1", "workspace.search", changed, map[string]any{"query": "needle"}, manifest)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "pipeline definition changed"))
}

func TestPipelineStartedRecordsWhyAPipelineIsNotGoverned(t *testing.T) {
	ctx := context.Background()
	start := func(t *testing.T, pipe config.Pipeline) audit.Event {
		t.Helper()
		sm := state.NewStateManager(state.NewMemoryStore())
		cfg := &config.Config{
			Pipelines: map[string]config.Pipeline{pipe.Name: pipe},
			Tools:     map[string]config.Tool{"lookup": {Name: "lookup", Type: "http", Mode: "read"}},
			Agents:    map[string]config.AgentDefinition{},
		}
		require.NoError(t, sm.Save(ctx, &state.ExecutionState{ID: "trace-x", UserID: "alice", Intent: "lookup", Pipeline: pipe.Name}))
		v := NewVerifier(bus.New(), cfg, nil, bus.New(), sm)
		logger := &captureAudit{}
		v.SetAuditLogger(logger)
		initial, err := sm.Load(ctx, "trace-x")
		require.NoError(t, err)
		_, err = v.ensurePipelineManifest("trace-x", "session-1", "lookup", pipe, map[string]any{}, initial)
		require.NoError(t, err)
		require.Len(t, logger.events, 1)
		require.Equal(t, "pipeline.started", logger.events[0].Phase)
		return logger.events[0]
	}
	steps := []config.PipelineStep{{ID: "read", Tool: "lookup"}}
	exemptFlag, governedFlag := false, true

	exempt := start(t, config.Pipeline{Name: "lookup", Governed: &exemptFlag, SkipSummary: true, UngovernedReason: "deterministic lookup, no AI", Steps: steps})
	require.Equal(t, "deterministic lookup, no AI", exempt.Details["ungoverned_reason"])

	governed := start(t, config.Pipeline{Name: "lookup", Governed: &governedFlag, AiSystem: "sys-1", Steps: steps})
	require.NotContains(t, governed.Details, "ungoverned_reason")
}
