package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/engine"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/logx"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
)

type dagNode struct {
	Step      config.PipelineStep
	Index     int
	Status    string // pending, running, success, failed, skipped, paused, cascaded_skip, cascaded_pause
	DoneCh    chan struct{}
}

func executeDAG(v *Verifier, pipe config.Pipeline, id, sessionID, intentType, language string, taskCtx context.Context, stepResults map[string]any, baseParams map[string]any) {
	ctx := engine.NewPipelineContext(baseParams)
	
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
	
	var mu sync.Mutex

	nodes := make(map[string]*dagNode)
	for i, step := range pipe.Steps {
		stepID := step.ID
		if stepID == "" {
			stepID = step.Tool
		}
		nodes[stepID] = &dagNode{
			Step:   step,
			Index:  i,
			Status: "pending",
			DoneCh: make(chan struct{}),
		}
	}
	
	if execState != nil && len(execState.CompletedSteps) > 0 {
		for _, stepID := range execState.CompletedSteps {
			if node, ok := nodes[stepID]; ok {
				node.Status = "success"
				close(node.DoneCh)
			}
		}
	}

	importJSON, err := json.Marshal(pipe.Steps)
	if err == nil {
		v.uiStore.AddEvent(id, "Verifier", "dag_structure", string(importJSON), "")
	}

	var wg sync.WaitGroup

	for stepID, node := range nodes {
		wg.Add(1)
		go func(nID string, n *dagNode) {
			defer wg.Done()
			
			for _, dep := range n.Step.DependsOn {
				depNode, ok := nodes[dep]
				if ok {
					<-depNode.DoneCh
					mu.Lock()
					depStatus := depNode.Status
					mu.Unlock()
					if depStatus == "failed" || depStatus == "skipped" || depStatus == "cascaded_skip" || depStatus == "paused" || depStatus == "cascaded_pause" {
						mu.Lock()
						if depStatus == "paused" || depStatus == "cascaded_pause" {
							n.Status = "cascaded_pause"
						} else {
							n.Status = "cascaded_skip"
						}
						mu.Unlock()
						close(n.DoneCh)
						return
					}
				}
			}
			
			mu.Lock()
			if n.Status == "success" {
				mu.Unlock()
				return
			}
			n.Status = "running"
			
			ctx.Vars = engine.AutoCastVars(ctx.Vars)
			
			if !engine.StepShouldRun(n.Step, ctx) {
				if n.Step.HumanGate != "" {
					logx.Debug("Verifier", "human gate '%s' skipped by condition", n.Step.HumanGate)
				} else {
					logx.Debug("Verifier", "skipping step=%s id=%s due to condition", n.Step.Tool, id)
					stepResults[n.Step.Tool] = map[string]any{
						"status":   "skipped",
						"effect":   "not_executed",
						"executed": false,
						"reason":   "when=false",
					}
				}
				n.Status = "skipped"
				mu.Unlock()
				close(n.DoneCh)
				return
			}
			mu.Unlock()

			if n.Step.HumanGate != "" {
				mu.Lock()

				if execState != nil && execState.Gate == n.Step.HumanGate {
					logx.Info("Verifier", "[%s] resuming after human gate: %s", id, n.Step.HumanGate)
					n.Status = "success"
					mu.Unlock()
					close(n.DoneCh)
					return
				}

				n.Status = "paused"
				ctx.Vars[n.Step.HumanGate+"_executed"] = true
				ctx.Vars[n.Step.HumanGate+"_decision"] = "pending"
				
				var completed []string
				for id, nd := range nodes {
					if nd.Status == "success" {
						completed = append(completed, id)
					}
				}

				nextState := &state.ExecutionState{
					ID:             id,
					SessionID:      sessionID,
					Intent:         intentType,
					Pipeline:       pipe.Name,
					CompletedSteps: completed,
					Params:         convertVarsToStringMap(ctx.Vars),
					StepResults:    stepResults,
					Gate:           n.Step.HumanGate,
				}

				if err := v.stateManager.Save(context.Background(), nextState); err != nil {
					v.storeError(id, "cannot persist execution state")
				}
				contextBytes, err := json.Marshal(ctx.Vars)
				if err != nil {
					logx.Error("Verifier", "failed to marshal ctx.Vars: %v", err)
				}
				if v.uiStore != nil {
					v.uiStore.AddEventWithData(id, "Verifier", "await_human", n.Step.HumanGate, "", string(contextBytes))
				}
				if v.bus != nil {
					v.bus.Send("api", bus.Message{
						Type: "await_human",
						Payload: map[string]any{
							"id":   id,
							"gate": n.Step.HumanGate,
						},
					})
				}
				mu.Unlock()
				close(n.DoneCh)
				return
			}

			if n.Step.Analyst {
				mu.Lock()
				v.bus.Send("analyst", bus.Message{
					Type: "summarize",
					Payload: map[string]any{
						"id":         id,
						"session_id": sessionID,
						"intent":     intentType,
						"rawResult":  cloneMap(stepResults),
						"language":   language,
					},
				})
				n.Status = "success"
				mu.Unlock()
				close(n.DoneCh)
				return
			}

			if n.Step.Tool == "system.save_preference" {
				mu.Lock()
				logx.Info("Verifier", "executing internal tool=%s id=%s", n.Step.Tool, id)
				callParams := buildCallParams(ctx.Vars, n.Step.WithParams)
				pref, _ := callParams["preference"].(string)
				if pref != "" && v.stateManager != nil && v.stateManager.Vector() != nil && v.getLLMClient() != nil {
					emb, err := v.getLLMClient().Embed(context.Background(), pref)
					if err == nil {
						v.stateManager.Vector().AddMemory(context.Background(), state.Memory{
							ID:        id + "_pref_" + n.Step.Tool,
							SessionID: sessionID,
							Text:      "Preference: " + pref,
							Embedding: emb,
							Timestamp: time.Now(),
						})
					}
				}
				stepResults[n.Step.Tool] = map[string]any{"status": "saved"}
				if v.uiStore != nil {
					v.uiStore.AddEvent(id, "Verifier", "tool "+n.Step.Tool, "ok", "")
				}
				n.Status = "success"
				mu.Unlock()
				close(n.DoneCh)
				return
			}

			t, ok := v.cfg.Tools[n.Step.Tool]
			if !ok {
				v.storeError(id, fmt.Sprintf("tool %s not found", n.Step.Tool))
				mu.Lock()
				n.Status = "failed"
				mu.Unlock()
				close(n.DoneCh)
				return
			}

			logx.Info("Verifier", "executing dag tool=%s id=%s", n.Step.Tool, id)
			
			mu.Lock()
			callParams := buildCallParams(ctx.Vars, n.Step.WithParams)
			mu.Unlock()

			timer := logx.Start(id, "Verifier", "tool_"+t.Name)
			out, err := executeToolWithResiliency(taskCtx, t, convertVarsToStringMap(callParams), n.Step)
			timer.End()

			mu.Lock()
			if err != nil {
				logx.Error("Verifier", "tool %s failed: %v", n.Step.Tool, err)
				storeResult(id, Result{
					Status: "error",
					Err:    fmt.Sprintf("tool error: %v", err),
				})
				n.Status = "failed"
			} else {
				if out != nil {
					if _, exists := out["executed"]; !exists {
						out["executed"] = true
					}
					out["_executedAt"] = time.Now().Format(time.RFC3339)
				}
				stepResults[n.Step.Tool] = out
				v.uiStore.AddEvent(id, "Verifier", "tool "+n.Step.Tool, "ok", "")
				for k, val := range out {
					ctx.Vars[n.Step.Tool+"."+k] = val
				}
				n.Status = "success"
			}
			mu.Unlock()
			close(n.DoneCh)

		}(stepID, node)
	}

	wg.Wait()
	
	paused := false
	failed := false
	for _, n := range nodes {
		if n.Status == "paused" || n.Status == "cascaded_pause" {
			paused = true
		}
		if n.Status == "failed" {
			failed = true
		}
	}
	if paused || failed {
		return
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
			"rawResult":  cloneMap(stepResults),
			"language":   language,
		},
	})
}
