package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/audit"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/llm"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/logx"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
)

func executeReAct(v *Verifier, pipe config.Pipeline, id, sessionID, intentType, language string, taskCtx context.Context, stepResults map[string]any, baseParams map[string]any) {
	execState, _ := v.stateManager.Load(context.Background(), id)

	var params map[string]string
	var reactHistory []state.ReActStep

	if execState != nil {
		params = execState.Params
		reactHistory = execState.ReActHistory
	} else {
		params = make(map[string]string)
		for k, val := range baseParams {
			params[k] = fmt.Sprintf("%v", val)
		}
	}

	// 1. Build available tools from pipeline steps
	var allowedTools []config.Tool
	toolToStep := make(map[string]config.PipelineStep)
	for _, step := range pipe.Steps {
		t, ok := v.cfg.Tools[step.Tool]
		if ok {
			allowedTools = append(allowedTools, t)
			toolToStep[t.Name] = step
		}
	}

	// Build task description. The workspace root is trusted runtime context,
	// while tool calls should use paths relative to it.
	paramsJSON, _ := json.Marshal(params)
	workspaceRoot := os.Getenv("AOS_WORKSPACE_ROOT")
	if workspaceRoot == "" {
		workspaceRoot, _ = os.Getwd()
	}
	workspaceRoot, _ = filepath.Abs(workspaceRoot)
	taskDesc := fmt.Sprintf(`Solve user intent: '%s'.
Available context variables: %s
Workspace root: %s
Path policy: pass paths relative to the workspace root. Use "." for the whole project. Do not prepend the workspace root to relative paths and do not access paths outside it.`, intentType, string(paramsJSON), workspaceRoot)

	maxIterations := pipe.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 10
	}
	llmClient := v.getLLMClientForPipeline(pipe)
	for i := 0; i < maxIterations; i++ {
		// 1. Prompt LLM
		step, err := llm.AskReActStep(taskCtx, llmClient, taskDesc, allowedTools, reactHistory)
		if err != nil {
			v.storeError(id, "ReAct LLM error: "+err.Error())
			return
		}
		if step.LLMStats != nil {
			statsJSON, _ := json.Marshal(step.LLMStats)
			v.eventBus.PublishEventWithData(
				id,
				"Ollama",
				"llm_stats",
				fmt.Sprintf("%.1f tok/s", step.LLMStats.OutputTokensPerSecond),
				fmt.Sprintf("%.2fs", step.LLMStats.TotalDurationSeconds),
				string(statsJSON),
			)
		}
		decision := map[string]any{
			"iteration":           i + 1,
			"action":              step.Action,
			"action_input_digest": audit.Digest(step.ActionInput),
			"thought_digest":      audit.Digest(step.Thought),
		}
		if step.LLMStats != nil {
			decision["model"] = step.LLMStats.Model
		}
		if err := v.logLifecycleAudit(execState, "agent.decision", "recorded", decision); err != nil {
			v.failAuditExecution(execState, id, err)
			return
		}

		v.eventBus.PublishEvent(id, "Verifier", "ReAct Thought", step.Thought, "")
		stagnantThought := false
		if len(reactHistory) > 0 {
			stagnantThought = normalizeThought(reactHistory[len(reactHistory)-1].Thought) == normalizeThought(step.Thought)
		}

		if step.Action == "FINISH" {
			answer := strings.TrimSpace(step.ActionInput)
			if answer == "" || answer == "null" || answer == `""` {
				step.Observation = "Error: FINISH requires a concrete, non-empty final answer based on the observations."
				reactHistory = append(reactHistory, *step)
				continue
			}
			if validationError := validateReActFinish(reactHistory); validationError != "" {
				step.Observation = validationError
				reactHistory = append(reactHistory, *step)
				continue
			}
			// Done
			stepResults["_pipeline"] = map[string]any{
				"pipeline":         pipe.Name,
				"pipeline_version": executionPipelineVersion(execState),
				"intent":           intentType,
				"status":           "success",
				"steps":            len(reactHistory) + 1,
				"endedAt":          time.Now().Format(time.RFC3339),
			}
			stepResults["final_answer"] = step.ActionInput

			if pipe.SkipSummary {
				storeResult(id, Result{Status: "completed", Data: map[string]any{"raw": cloneMap(stepResults), "react_history": reactHistory}})
				return
			}

			v.bus.Send("analyst", bus.Message{
				Type: "summarize",
				Payload: map[string]any{
					"id":         id,
					"session_id": sessionID,
					"intent":     intentType,
					"pipeline":   pipe.Name,
					"llm_model":  pipe.LLMModel,
					"rawResult":  cloneMap(stepResults),
					"language":   language,
				},
			})
			return
		}

		if len(reactHistory) > 0 {
			previous := reactHistory[len(reactHistory)-1]
			if previous.Action == step.Action && previous.ActionInput == step.ActionInput {
				step.Observation = stagnationWarning("Error: identical consecutive action blocked. Use the previous observation and choose a different action.")
				reactHistory = append(reactHistory, *step)
				continue
			}
		}

		// Execute Tool
		toolConfig, ok := v.cfg.Tools[step.Action]
		pipelineStep, okStep := toolToStep[step.Action]

		if !ok || !okStep {
			step.Observation = fmt.Sprintf("Error: Tool '%s' not found or not allowed in this pipeline.", step.Action)
			if stagnantThought {
				step.Observation = stagnationWarning(step.Observation)
			}
			reactHistory = append(reactHistory, *step)
			continue
		}

		// Handle HumanGate
		if pipelineStep.HumanGate != "" {
			// Save state and pause
			reactHistory = append(reactHistory, *step) // save intent to execute tool

			// We inject a special observation that wait happened so we resume correctly
			reactHistory[len(reactHistory)-1].Observation = "Paused for human approval. Awaiting decision."

			nextState := &state.ExecutionState{
				ID:           id,
				SessionID:    sessionID,
				Intent:       intentType,
				Pipeline:     pipe.Name,
				Params:       params,
				StepResults:  stepResults,
				ReActHistory: reactHistory,
				Gate:         pipelineStep.HumanGate,
			}

			if execState != nil {
				nextState.UserID = execState.UserID
				nextState.CreatedAt = execState.CreatedAt
				copyExecutionManifest(nextState, execState)
			}
			if err := v.stateManager.Save(context.Background(), nextState); err != nil {
				v.storeError(id, "cannot persist execution state")
				return
			}
			if err := v.logLifecycleAudit(nextState, "human.approval_requested", "pending", map[string]any{"gate": pipelineStep.HumanGate, "step": pipelineStep.ID, "action": step.Action}); err != nil {
				v.failAuditExecution(nextState, id, err)
				return
			}

			v.eventBus.PublishEvent(id, "Verifier", "await_human", pipelineStep.HumanGate, "")

			v.bus.Send("api", bus.Message{
				Type: "await_human",
				Payload: map[string]any{
					"id":   id,
					"gate": pipelineStep.HumanGate,
				},
			})
			return
		}

		// Parse action input
		callParams := make(map[string]string)
		json.Unmarshal([]byte(step.ActionInput), &callParams)

		// Fallback copy pipeline step params
		for k, val := range pipelineStep.WithParams {
			if callParams[k] == "" {
				callParams[k] = val
			}
		}

		logx.Info("Verifier", "[ReAct %s] Executing %s with params %v", id, step.Action, callParams)
		if err := v.logLifecycleAudit(execState, "tool.started", "pending", map[string]any{
			"step": pipelineStep.ID, "tool": step.Action, "decision": "invoke",
			"input_digest": audit.Digest(audit.SanitizeMap(stringMapToAny(callParams))),
			"inputs":       audit.SanitizeMap(stringMapToAny(callParams)),
		}); err != nil {
			v.failAuditExecution(execState, id, err)
			return
		}
		out, err := executeToolWithResiliency(taskCtx, toolConfig, callParams, pipelineStep)

		if err != nil {
			step.Observation = fmt.Sprintf("Error executing tool: %v", err)
			logx.Warn("Verifier", "[ReAct %s] %s", id, step.Observation)
			if auditErr := v.logLifecycleAudit(execState, "tool.failed", "error", map[string]any{"step": pipelineStep.ID, "tool": step.Action, "error": err.Error()}); auditErr != nil {
				v.failAuditExecution(execState, id, auditErr)
				return
			}
		} else {
			outBytes, _ := json.Marshal(out)
			step.Observation = string(outBytes)
			v.eventBus.PublishEvent(id, "Verifier", "tool "+step.Action, "ok", "")
			stepResults[step.Action] = out
			if auditErr := v.logLifecycleAudit(execState, "tool.completed", "success", map[string]any{"step": pipelineStep.ID, "tool": step.Action, "result": auditSafeToolResult(out), "output_digest": audit.Digest(auditSafeToolResult(out))}); auditErr != nil {
				v.failAuditExecution(execState, id, auditErr)
				return
			}
		}
		if stagnantThought {
			step.Observation = stagnationWarning(step.Observation)
		}

		reactHistory = append(reactHistory, *step)
	}

	v.storeError(id, "ReAct max iterations reached without FINISH")
}

func normalizeThought(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func stagnationWarning(observation string) string {
	return "STAGNATION WARNING: You repeated the same diagnosis. Use the concrete tool output below, inspect the current file if needed, and take a different corrective action. Do not merely restate the plan.\n" + observation
}

func validateReActFinish(history []state.ReActStep) string {
	latestCodeMutation := -1
	latestTest := -1
	for i, step := range history {
		if isCodeMutation(step) {
			latestCodeMutation = i
		}
		if step.Action == "workspace.run_tests" {
			latestTest = i
		}
	}
	if latestCodeMutation < 0 {
		return ""
	}
	if latestTest < latestCodeMutation {
		return "Final answer rejected: run workspace.run_tests after the latest code change and use its result before finishing."
	}
	observation := strings.ToLower(history[latestTest].Observation)
	if strings.Contains(observation, `"status":"failed"`) || strings.Contains(observation, "error executing tool") {
		return "Final answer rejected: the latest test run failed. Inspect the failure, fix it, and run workspace.run_tests again."
	}
	return ""
}

func isCodeMutation(step state.ReActStep) bool {
	if step.Action != "workspace.write_file" && step.Action != "workspace.replace_in_file" {
		return false
	}
	var input map[string]any
	if json.Unmarshal([]byte(step.ActionInput), &input) != nil {
		return false
	}
	path, _ := input["path"].(string)
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".java", ".go", ".py", ".js", ".jsx", ".ts", ".tsx", ".kt", ".kts", ".xml":
		return true
	default:
		return false
	}
}
