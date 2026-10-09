package tools

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
)

func TestExecuteAgentClaudeCLIStreamsNormalizedEventsAndResult(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	binary := filepath.Join(binDir, "fake-claude")
	script := `#!/bin/sh
cat <<'JSON'
{"type":"system","subtype":"init","session_id":"session-123"}
{"type":"assistant","session_id":"session-123","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"README.md"}}]}}
{"type":"result","subtype":"success","session_id":"session-123","result":"done","is_error":false,"num_turns":2}
JSON
`
	if err := os.WriteFile(binary, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AOS_WORKSPACE_ROOT", root)
	var events []AgentEvent

	out, err := ExecuteAgent(context.Background(), config.AgentDefinition{Name: "claude", Runtime: "claude_code_cli", Binary: "fake-claude"}, map[string]string{
		"task":            "inspect this project",
		"cwd":             ".",
		"permission_mode": "plan",
		"max_turns":       "3",
	}, func(event AgentEvent) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	if out["status"] != "completed" || out["session_id"] != "session-123" || out["result"] != "done" {
		t.Fatalf("unexpected agent result: %#v", out)
	}
	want := []string{"agent.started", "agent.tool_call", "agent.completed"}
	got := make([]string, 0, len(events))
	for _, event := range events {
		got = append(got, event.Type)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected normalized events: %#v", got)
	}
}

func TestExecuteAgentClaudeCLIRejectsWorkspaceEscape(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AOS_WORKSPACE_ROOT", root)
	_, err := ExecuteAgent(context.Background(), config.AgentDefinition{Name: "claude", Runtime: "claude_code_cli"}, map[string]string{
		"task": "inspect",
		"cwd":  t.TempDir(),
	}, nil)
	if err == nil {
		t.Fatal("expected agent cwd outside workspace root to be rejected")
	}
}

func TestExecuteAgentClaudeCLIRejectsUnsafePermissionMode(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AOS_WORKSPACE_ROOT", root)
	_, err := ExecuteAgent(context.Background(), config.AgentDefinition{Name: "claude", Runtime: "claude_code_cli"}, map[string]string{
		"task":            "inspect",
		"cwd":             ".",
		"permission_mode": "bypassPermissions",
	}, nil)
	if err == nil {
		t.Fatal("expected unsafe permission mode to be rejected")
	}
}
