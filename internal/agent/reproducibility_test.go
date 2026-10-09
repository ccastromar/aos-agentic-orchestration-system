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
