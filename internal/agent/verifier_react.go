package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ccastromar/aos-agent-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/llm"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/logx"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/state"
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
	
	// Build task description
	paramsJSON, _ := json.Marshal(params)
	taskDesc := fmt.Sprintf("Solve user intent: '%s'. Available context variables: %s", intentType, string(paramsJSON))

	maxIterations := 10
	for i := 0; i < maxIterations; i++ {
		// 1. Prompt LLM
		step, err := llm.AskReActStep(taskCtx, v.llmClient, taskDesc, allowedTools, reactHistory)
		if err != nil {
			v.storeError(id, "ReAct LLM error: " + err.Error())
			return
		}

		v.uiStore.AddEvent(id, "Verifier", "ReAct Thought", step.Thought, "")
		
		if step.Action == "FINISH" {
			// Done
			stepResults["_pipeline"] = map[string]any{
				"pipeline": pipe.Name,
				"intent":   intentType,
				"status":   "success",
				"steps":    len(reactHistory) + 1,
				"endedAt":  time.Now().Format(time.RFC3339),
			}
			stepResults["final_answer"] = step.ActionInput

			v.bus.Send("analyst", bus.Message{
				Type: "summarize",
				Payload: map[string]any{
					"id":         id,
					"session_id": sessionID,
					"intent":     intentType,
					"rawResult":  stepResults,
					"language":   language,
				},
			})
			return
		}

		// Execute Tool
		toolConfig, ok := v.cfg.Tools[step.Action]
		pipelineStep, okStep := toolToStep[step.Action]
		
		if !ok || !okStep {
			step.Observation = fmt.Sprintf("Error: Tool '%s' not found or not allowed in this pipeline.", step.Action)
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
				ID:             id,
				SessionID:      sessionID,
				Intent:         intentType,
				Pipeline:       pipe.Name,
				Params:         params,
				StepResults:    stepResults,
				ReActHistory:   reactHistory,
				Gate:           pipelineStep.HumanGate,
			}

			if err := v.stateManager.Save(context.Background(), nextState); err != nil {
				v.storeError(id, "cannot persist execution state")
				return
			}
			
			v.uiStore.AddEvent(id, "Verifier", "await_human", pipelineStep.HumanGate, "")

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
		out, err := executeToolWithResiliency(taskCtx, toolConfig, callParams, pipelineStep)
		
		if err != nil {
			step.Observation = fmt.Sprintf("Error executing tool: %v", err)
			logx.Warn("Verifier", "[ReAct %s] %s", id, step.Observation)
		} else {
			outBytes, _ := json.Marshal(out)
			step.Observation = string(outBytes)
			v.uiStore.AddEvent(id, "Verifier", "tool "+step.Action, "ok", "")
			stepResults[step.Action] = out
		}

		reactHistory = append(reactHistory, *step)
	}

	v.storeError(id, "ReAct max iterations reached without FINISH")
}
