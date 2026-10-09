package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
)

// AgentEvent is the runtime-neutral event envelope emitted while an external
// agent is running. Data preserves the adapter's source event for diagnostics.
type AgentEvent struct {
	Type      string         `json:"type"`
	Agent     string         `json:"agent"`
	SessionID string         `json:"session_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

// ExecuteAgent invokes an external agent runtime configured by name. Pipeline
// parameters form the portable contract: task, cwd, limits, permissions, and
// optional session_id. The initial adapter consumes Claude Code's documented
// headless stream-json CLI.
func ExecuteAgent(ctx context.Context, definition config.AgentDefinition, params map[string]string, onEvent func(AgentEvent)) (map[string]any, error) {
	if definition.Runtime != "claude_code_cli" {
		return nil, fmt.Errorf("unsupported agent runtime %q", definition.Runtime)
	}
	task := strings.TrimSpace(params["task"])
	if task == "" {
		return nil, fmt.Errorf("agent task is required")
	}
	root, cwd, err := resolveAgentDirectory(params["cwd"])
	if err != nil {
		return nil, err
	}
	permissionMode := strings.TrimSpace(params["permission_mode"])
	if permissionMode == "" {
		permissionMode = "plan"
	}
	switch permissionMode {
	case "default", "acceptEdits", "plan", "auto", "dontAsk":
	default:
		return nil, fmt.Errorf("unsupported or unsafe agent permission_mode %q", permissionMode)
	}
	maxTurns := 10
	if raw := strings.TrimSpace(params["max_turns"]); raw != "" {
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil || value < 1 || value > 100 {
			return nil, fmt.Errorf("agent max_turns must be between 1 and 100")
		}
		maxTurns = value
	}

	binary := strings.TrimSpace(definition.Binary)
	if binary == "" {
		binary = "claude"
	}
	executable, err := exec.LookPath(binary)
	if err != nil {
		return nil, fmt.Errorf("Claude Code CLI %q not found in PATH: %w", binary, err)
	}
	args := []string{"-p", task, "--output-format", "stream-json", "--verbose", "--permission-mode", permissionMode, "--max-turns", strconv.Itoa(maxTurns)}
	loadProjectConfig := strings.EqualFold(strings.TrimSpace(params["load_project_config"]), "true")
	if !loadProjectConfig {
		args = append(args, "--bare")
	}
	if allowed := strings.TrimSpace(params["allowed_tools"]); allowed != "" {
		args = append(args, "--allowedTools", allowed)
	}
	if budget := strings.TrimSpace(params["max_budget_usd"]); budget != "" {
		amount, parseErr := strconv.ParseFloat(budget, 64)
		if parseErr != nil || amount <= 0 {
			return nil, fmt.Errorf("agent max_budget_usd must be a positive number")
		}
		args = append(args, "--max-budget-usd", strconv.FormatFloat(amount, 'f', -1, 64))
	}
	if sessionID := strings.TrimSpace(params["session_id"]); sessionID != "" {
		args = append(args, "--resume", sessionID)
	}

	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = cwd
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open Claude Code stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start Claude Code: %w", err)
	}

	result := map[string]any{
		"agent":   definition.Name,
		"runtime": definition.Runtime,
		"cwd":     relativeAgentPath(root, cwd),
		"status":  "running",
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var parseErr error
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			parseErr = fmt.Errorf("invalid Claude stream-json event: %w", err)
			break
		}
		captureClaudeResult(event, result, definition.Name, onEvent)
	}
	if err := scanner.Err(); err != nil && parseErr == nil {
		parseErr = fmt.Errorf("read Claude Code event stream: %w", err)
	}
	waitErr := cmd.Wait()
	if parseErr != nil {
		return nil, parseErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if waitErr != nil {
		result["status"] = "failed"
		if stderr.Len() > 0 {
			return result, fmt.Errorf("Claude Code exited: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
		}
		return result, fmt.Errorf("Claude Code exited: %w", waitErr)
	}
	if status, _ := result["status"].(string); status == "running" {
		return result, fmt.Errorf("Claude Code exited without a final result event")
	}
	if status, _ := result["status"].(string); status == "failed" {
		return result, fmt.Errorf("Claude Code returned a failed result: %v", result["subtype"])
	}
	return result, nil
}

func resolveAgentDirectory(requested string) (string, string, error) {
	root := os.Getenv("AOS_WORKSPACE_ROOT")
	if root == "" {
		root, _ = os.Getwd()
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("resolve AOS workspace root: %w", err)
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	requested = strings.TrimSpace(requested)
	if requested == "" {
		requested = "."
	}
	cwd := requested
	if !filepath.IsAbs(cwd) {
		cwd = filepath.Join(root, cwd)
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return "", "", fmt.Errorf("resolve agent cwd: %w", err)
	}
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = real
	}
	if cwd != root && !strings.HasPrefix(cwd, root+string(filepath.Separator)) {
		return "", "", fmt.Errorf("agent cwd is outside AOS workspace root")
	}
	info, err := os.Stat(cwd)
	if err != nil {
		return "", "", fmt.Errorf("agent cwd is unavailable: %w", err)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("agent cwd must be a directory")
	}
	return root, cwd, nil
}

func relativeAgentPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

func captureClaudeResult(source map[string]any, result map[string]any, agentName string, onEvent func(AgentEvent)) {
	kind, _ := source["type"].(string)
	if kind == "stream_event" {
		return
	}
	subtype, _ := source["subtype"].(string)
	sessionID, _ := source["session_id"].(string)
	if sessionID != "" {
		result["session_id"] = sessionID
	}
	if kind == "result" {
		for _, key := range []string{"result", "subtype", "is_error", "total_cost_usd", "usage", "num_turns", "permission_denials"} {
			if value, ok := source[key]; ok {
				result[key] = value
			}
		}
		if isError, _ := source["is_error"].(bool); isError || (subtype != "" && subtype != "success") {
			result["status"] = "failed"
			emitAgentEvent(onEvent, AgentEvent{Type: "agent.failed", Agent: agentName, SessionID: sessionID, Data: source})
		} else {
			result["status"] = "completed"
			emitAgentEvent(onEvent, AgentEvent{Type: "agent.completed", Agent: agentName, SessionID: sessionID, Data: source})
		}
		return
	}
	if kind == "system" && subtype == "init" {
		emitAgentEvent(onEvent, AgentEvent{Type: "agent.started", Agent: agentName, SessionID: sessionID, Data: source})
		return
	}
	if kind == "assistant" {
		message, _ := source["message"].(map[string]any)
		blocks, _ := message["content"].([]any)
		for _, rawBlock := range blocks {
			block, _ := rawBlock.(map[string]any)
			eventType := "agent.message"
			if block["type"] == "tool_use" {
				eventType = "agent.tool_call"
			}
			emitAgentEvent(onEvent, AgentEvent{Type: eventType, Agent: agentName, SessionID: sessionID, Data: block})
		}
		return
	}
	if kind == "user" {
		message, _ := source["message"].(map[string]any)
		blocks, _ := message["content"].([]any)
		for _, rawBlock := range blocks {
			block, _ := rawBlock.(map[string]any)
			if block["type"] == "tool_result" {
				emitAgentEvent(onEvent, AgentEvent{Type: "agent.tool_result", Agent: agentName, SessionID: sessionID, Data: block})
			}
		}
		return
	}
	if kind != "" {
		emitAgentEvent(onEvent, AgentEvent{Type: "agent.progress", Agent: agentName, SessionID: sessionID, Data: source})
	}
}

func emitAgentEvent(callback func(AgentEvent), event AgentEvent) {
	if callback != nil {
		callback(event)
	}
}
