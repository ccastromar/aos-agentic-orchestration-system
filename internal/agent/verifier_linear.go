package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/ccastromar/aos-agent-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/engine"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/logx"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/state"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/tools"
)

// executeLinear runs the pipeline steps in strict sequential order.
func executeLinear(v *Verifier, pipe config.Pipeline, id, sessionID, intentType, language string, taskCtx context.Context, stepResults map[string]any, baseParams map[string]any) {
	ctx := engine.NewPipelineContext(baseParams)
	
	// Flatten params and stepResults into ctx.Vars for evaluation
	execState, _ := v.stateManager.Load(context.Background(), id)
	var params map[string]string
	if execState != nil {
		params = execState.Params
	} else {
		params = map[string]string{}
	}

	for k, val := range params {
		ctx.Vars[k] = val
	}
	for k, val := range stepResults {
		if resMap, ok := val.(map[string]any); ok {
			for mk, mv := range resMap {
				ctx.Vars[k+"."+mk] = mv
			}
		}
	}

	startAt := 0
	if execState != nil {
		startAt = len(execState.CompletedSteps)
	}

	for index := startAt; index < len(pipe.Steps); index++ {
		step := pipe.Steps[index]
		ctx.Vars = engine.AutoCastVars(ctx.Vars)

		logx.Info("Verifier", "[%s] Vars before analyst: %#v", id, ctx.Vars)

		if !engine.StepShouldRun(step, ctx) {
			if step.HumanGate != "" {
				logx.Debug("Verifier", "human gate '%s' skipped by condition", step.HumanGate)
				continue
			}
			logx.Debug("Verifier", "skipping step=%s id=%s due to condition", step.Tool, id)

			stepResults[step.Tool] = map[string]any{
				"status":   "skipped",
				"effect":   "not_executed",
				"executed": false,
				"reason":   "when=false",
			}
			continue
		}

		if step.Analyst {
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

		if step.HumanGate != "" {
			if execState != nil && execState.Gate == step.HumanGate {
				logx.Info("Verifier", "[%s] resuming after human gate: %s", id, step.HumanGate)
				continue
			}

			ctx.Vars[step.HumanGate+"_executed"] = true
			ctx.Vars[step.HumanGate+"_decision"] = "pending"

			execState := &state.ExecutionState{
				ID:             id,
				SessionID:      sessionID,
				Intent:         intentType,
				Pipeline:       pipe.Name,
				CompletedSteps: make([]string, index),
				Params:         convertVarsToStringMap(ctx.Vars),
				StepResults:    stepResults,
				Gate:           step.HumanGate,
			}

			if err := v.stateManager.Save(context.Background(), execState); err != nil {
				v.storeError(id, "cannot persist execution state")
				return
			}

			v.uiStore.AddEvent(id, "Verifier", "await_human", step.HumanGate, "")

			v.bus.Send("api", bus.Message{
				Type: "await_human",
				Payload: map[string]any{
					"id":   id,
					"gate": step.HumanGate,
				},
			})
			return // pipeline pauses here
		}

		toolName := step.Tool

		if toolName == "system.save_preference" {
			logx.Info("Verifier", "executing internal tool=%s id=%s", toolName, id)
			callParams := buildCallParams(ctx.Vars, step.WithParams)
			pref, _ := callParams["preference"].(string)
			if pref != "" && v.stateManager != nil && v.stateManager.Vector() != nil && v.getLLMClient() != nil {
				emb, err := v.getLLMClient().Embed(context.Background(), pref)
				if err == nil {
					v.stateManager.Vector().AddMemory(context.Background(), state.Memory{
						ID:        id + "_pref",
						SessionID: sessionID,
						Text:      "Preference: " + pref,
						Embedding: emb,
						Timestamp: time.Now(),
					})
				}
			}
			stepResults[toolName] = map[string]any{"status": "saved"}
			if v.uiStore != nil {
				v.uiStore.AddEvent(id, "Verifier", "tool "+toolName, "ok", "")
			}
			continue
		}

		t, ok := v.cfg.Tools[toolName]
		if !ok {
			storeResult(id, Result{
				Status: "error",
				Err:    fmt.Sprintf("tool %s not found", toolName),
			})
			return
		}

		logx.Info("Verifier", "executing tool=%s id=%s", toolName, id)

		callParams := buildCallParams(ctx.Vars, step.WithParams)

		timer := logx.Start(id, "Verifier", "tool_"+t.Name)
		out, err := executeToolWithResiliency(taskCtx, t, convertVarsToStringMap(callParams), step)
		timer.End()

		if err != nil {
			logx.Error("Verifier", "tool %s failed: %v", toolName, err)
			storeResult(id, Result{
				Status: "error",
				Err:    fmt.Sprintf("tool error: %v", err),
			})
			return
		}

		stepResults[toolName] = out
		v.uiStore.AddEvent(id, "Verifier", "tool "+toolName, "ok", "")
		for k, val := range out {
			ctx.Vars[toolName+"."+k] = val
		}
	}

	stepResults["_pipeline"] = map[string]any{
		"pipeline": pipe.Name,
		"intent":   intentType,
		"status":   "success",
		"steps":    len(pipe.Steps),
		"endedAt":  time.Now().Format(time.RFC3339),
	}

	logx.Debug("Verifier", "[%s] Final Vars: %#v", id, ctx.Vars)
	logx.Debug("Verifier", "[%s] ToolOutputs: %#v", id, stepResults)

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
}

func executeToolWithResiliency(ctx context.Context, t config.Tool, params map[string]string, step config.PipelineStep) (map[string]any, error) {
	retries := step.Retries
	if retries < 0 {
		retries = 0
	}
	backoffMs := step.BackoffMs
	if backoffMs <= 0 {
		backoffMs = 1000 // default 1s
	}

	var lastErr error
	var out map[string]any
	
	fmt.Printf("RETRY_DEBUG: step=%s retries=%d backoff=%d timeout=%d\n", step.Tool, retries, backoffMs, step.TimeoutMs)

	for i := 0; i <= retries; i++ {
		var tryCtx context.Context
		var cancel context.CancelFunc
		if step.TimeoutMs > 0 {
			tryCtx, cancel = context.WithTimeout(ctx, time.Duration(step.TimeoutMs)*time.Millisecond)
		} else {
			tryCtx, cancel = context.WithCancel(ctx)
		}
		
		out, lastErr = tools.ExecuteToolCtx(tryCtx, t, params)
		cancel()
		
		if lastErr == nil {
			return out, nil
		}
		
		logx.Warn("Verifier", "tool %s failed (attempt %d/%d): %v", t.Name, i+1, retries+1, lastErr)
		
		if i < retries {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(backoffMs) * time.Millisecond):
				// Exponential backoff logic
				backoffMs *= 2
			}
		}
	}

	return nil, lastErr
}
