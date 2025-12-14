package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/ccastromar/aos-agent-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/engine"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/logx"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/payload"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/state"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/tools"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/ui"
)

type Verifier struct {
	bus          *bus.Bus
	cfg          *config.Config
	inbox        chan bus.Message
	uiStore      *ui.UIStore
	stateManager *state.StateManager
}

func NewVerifier(b *bus.Bus, cfg *config.Config, ui *ui.UIStore, sm *state.StateManager) *Verifier {
	return &Verifier{
		bus:          b,
		cfg:          cfg,
		inbox:        make(chan bus.Message, 16),
		uiStore:      ui,
		stateManager: sm,
	}
}

func (v *Verifier) Inbox() chan bus.Message {
	return v.inbox
}

func (v *Verifier) Start(ctx context.Context) error {
	defer func() {
		if r := recover(); r != nil {
			logx.Error("Verifier", "panic recovered in Start: %v", r)
		}
	}()
	for {
		select {
		case msg := <-v.inbox:
			func() {
				defer func() {
					if r := recover(); r != nil {
						logx.Error("Verifier", "panic recovered in dispatch: %v", r)
					}
				}()
				v.dispatch(msg)
			}()

		case <-ctx.Done():
			return nil
		}
	}
}

func (v *Verifier) dispatch(msg bus.Message) {
	//message from the planner agent
	switch msg.Type {
	case "run_pipeline":
		v.handleRunPipeline(msg)
	case "human_decision":
		v.handleHumanDecision(msg)
	default:
		logx.Warn("Verifier", "unknown message: %#v", msg)
	}
}

func (v *Verifier) handleRunPipeline(msg bus.Message) {
	id, ok := payload.GetString(msg.Payload, "id")
	if !ok {
		logx.Error("Verifier", "invalid payload: missing id")
		return
	}
	intentType, ok := payload.GetString(msg.Payload, "intent")
	if !ok {
		logx.Error("Verifier", "invalid payload: missing intent")
		return
	}
	language, ok := payload.GetString(msg.Payload, "language")
	if !ok {
		logx.Error("Verifier", "invalid payload: missing language")
		return
	}

	pipeAny := msg.Payload["pipeline"]
	paramsAny := msg.Payload["params"]

	pipe, ok := pipeAny.(config.Pipeline)
	if !ok {
		storeResult(id, Result{
			Status: "error",
			Err:    "invalid pipeline",
		})
		return
	}

	// ============================================================
	// 1) Extract initial params (Planner → Verifier)
	// ============================================================
	baseParams := make(map[string]interface{})
	if paramsAny != nil {
		switch t := paramsAny.(type) {
		case map[string]string:
			for k, v := range t {
				baseParams[k] = v
			}
		case map[string]any:
			for k, v := range t {
				baseParams[k] = v
			}
		}
	}

	logx.Info("Verifier", "executing pipeline=%s id=%s intent=%s params=%#v",
		pipe.Name, id, intentType, baseParams)

	// ============================================================
	// 2) Create PipelineContext
	// ============================================================
	ctx := engine.NewPipelineContext(baseParams)
	// ctx.Vars now contains all user + planner + intent params
	// ctx.ToolOutputs will be filled automatically

	stepResults := make(map[string]any)

	// ============================================================
	// 3) Execute each step
	// ============================================================
	for index, step := range pipe.Steps {

		ctx.Vars = engine.AutoCastVars(ctx.Vars)

		logx.Info("Verifier", "[%s] Vars before analyst: %#v", id, ctx.Vars)

		// ---------------------------------------------------------
		// CONDITIONAL STEP
		// ---------------------------------------------------------
		if !engine.StepShouldRun(step, ctx) {
			// HUMAN STEP skipped → NO dejar rastro en stepResults
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

		// ---------------------------------------------------------
		// ANALYST STEP
		// ---------------------------------------------------------
		if step.Analyst {

			v.bus.Send("analyst", bus.Message{
				Type: "summarize",
				Payload: map[string]any{
					"id":        id,
					"intent":    intentType,
					"rawResult": stepResults,
					"language":  language,
				},
			})
			return
		}

		// ---------------------------------------------------------
		// HUMAN GATE (approval step)
		// ---------------------------------------------------------
		if step.HumanGate != "" {
			// Registrar en contexto d
			ctx.Vars[step.HumanGate+"_executed"] = true
			ctx.Vars[step.HumanGate+"_decision"] = "pending"

			execState := &state.ExecutionState{
				ID:          id,
				Intent:      intentType,
				Pipeline:    pipe.Name,
				StepIndex:   index,
				Params:      convertVarsToStringMap(ctx.Vars),
				StepResults: stepResults,
				Gate:        step.HumanGate,
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

		// ---------------------------------------------------------
		// TOOL STEP
		// ---------------------------------------------------------
		toolName := step.Tool
		t, ok := v.cfg.Tools[toolName]
		if !ok {
			storeResult(id, Result{
				Status: "error",
				Err:    fmt.Sprintf("tool %s not found", toolName),
			})
			return
		}

		logx.Info("Verifier", "executing tool=%s id=%s", toolName, id)

		// Build call parameters = ctx.Vars + WithParams (defaults)
		callParams := buildCallParams(ctx.Vars, step.WithParams)

		// Prepare tool execution context
		taskCtx, _ := GetTaskContext(id)
		if taskCtx == nil {
			taskCtx = context.Background()
			NewTaskContext(taskCtx, id, 0)
		}

		timer := logx.Start(id, "Verifier", "tool_"+t.Name)
		start := time.Now()

		out, err := tools.ExecuteToolCtx(taskCtx, t, convertVarsToStringMap(callParams))
		timer.End()

		if err != nil {
			logx.Error("Verifier", "error executing tool=%s: %v", toolName, err)
			storeResult(id, Result{
				Status: "error",
				Err:    err.Error(),
			})
			return
		}

		duration := time.Since(start).String()
		v.uiStore.AddEvent(id, "Verifier", "tool "+t.Name, "ok", duration)

		// ---------------------------------------------------------
		// Update pipeline context
		// ---------------------------------------------------------
		ctx.RecordToolOutput(toolName, out)

		stepResults[toolName] = map[string]any{
			"status":   "executed",
			"executed": true,
			"output":   out,
		}

	}
	//end of pipeline
	ctx.Vars["_pipeline"] = map[string]any{
		"status":   "success", // success | error | need_more_info | rejected
		"endedAt":  time.Now().UTC().Format(time.RFC3339),
		"pipeline": pipe.Name,
		"intent":   intentType,
		"steps":    len(pipe.Steps),
	}

	// ============================================================
	// 4) End of pipeline → send results to Analyst
	// ============================================================
	v.bus.Send("analyst", bus.Message{
		Type: "summarize",
		Payload: map[string]any{
			"id":        id,
			"intent":    intentType,
			"rawResult": stepResults,
			"language":  language,
		},
	})
	logx.Debug("Verifier", "[%s] Final Vars: %#v", id, ctx.Vars)
	logx.Debug("Verifier", "[%s] ToolOutputs: %#v", id, ctx.ToolOutputs)

}

// -------------------------------------------------------------
// OLD HANDLER
// -------------------------------------------------------------
func (v *Verifier) oldhandleRunPipeline(msg bus.Message) {
	id, ok := payload.GetString(msg.Payload, "id")
	if !ok {
		logx.Error("Verifier", "invalid payload: missing id")
		return
	}
	intentType, ok := payload.GetString(msg.Payload, "intent")
	if !ok {
		logx.Error("Verifier", "invalid payload: missing intent")
		return
	}

	pipeAny := msg.Payload["pipeline"]
	paramsAny := msg.Payload["params"]

	pipe, ok := pipeAny.(config.Pipeline)
	if !ok {
		storeResult(id, Result{
			Status: "error",
			Err:    "invalid pipeline",
		})
		return
	}

	// extracted params from the Planner agent
	baseParams := make(map[string]string)
	if paramsAny != nil {
		if mp, ok := paramsAny.(map[string]string); ok {
			for k, vv := range mp {
				baseParams[k] = vv
			}
		} else if mp2, ok := paramsAny.(map[string]any); ok {
			// Por compatibilidad si la decodificación viene en any
			for k, vv := range mp2 {
				if sv, ok := vv.(string); ok {
					baseParams[k] = sv
				}
			}
		}
	}

	logx.Info("Verifier", "executing pipeline=%s id=%s intent=%s params=%#v",
		pipe.Name, id, intentType, baseParams)

	//logx.Debug("Verifier", "PIPE STEPS = %#v", pipe.Steps)

	stepResults := make(map[string]any)

	// ---------------------------------------------------------
	// execute pipeline steps
	// ---------------------------------------------------------
	for index, step := range pipe.Steps {

		// Step ANALYST → forward to the Analyst agent
		if step.Analyst {
			//logx.Debug("Verifier", "analyst=true id=%s -> calling Analyst", id)
			v.bus.Send("analyst", bus.Message{
				Type: "summarize",
				Payload: map[string]any{
					"id":        id,
					"intent":    intentType,
					"rawResult": stepResults,
				},
			})
			return
		}

		if step.HumanGate != "" {
			execState := &state.ExecutionState{
				ID:          id,
				Intent:      intentType,
				Pipeline:    pipe.Name,
				StepIndex:   index,
				Params:      baseParams,
				StepResults: stepResults,
				Gate:        step.HumanGate,
			}

			if err := v.stateManager.Save(context.Background(), execState); err != nil {
				v.storeError(id, "cannot persist execution state")
				return
			}

			v.uiStore.AddEvent(id, "Verifier", "await_human", step.HumanGate, "")

			// signal API agent
			v.bus.Send("api", bus.Message{
				Type: "await_human",
				Payload: map[string]any{
					"id":   id,
					"gate": step.HumanGate,
				},
			})

			return // PAUSA PIPELINE
		}

		// Step TOOL
		toolName := step.Tool
		t, ok := v.cfg.Tools[toolName]
		if !ok {
			storeResult(id, Result{
				Status: "error",
				Err:    fmt.Sprintf("tool %s no encontrada", toolName),
			})
			return
		}

		logx.Info("Verifier", "executing tool=%s id=%s", toolName, id)
		// Combinar parámetros → baseParams + WithParams (sin pisar los del Planner)
		callParams := make(map[string]string)

		// 1. Copiar params del Planner
		for k, v := range baseParams {
			callParams[k] = v
		}

		// 2. Rellenar defaults del pipeline SIN sobreescribir valores existentes
		for k, v := range step.WithParams {
			if _, exists := callParams[k]; !exists || callParams[k] == "" {
				if v != "" { // evitamos meter valores vacíos
					callParams[k] = v
				}
			}
		}

		// Ejecutar tool con cuerpo renderizado
		timer := logx.Start(id, "Verifier", "tool_"+toolName)
		start := time.Now()

		logx.Debug("Verifier", "params for the tool=%s id=%s params=%#v",
			toolName, id, callParams)

		// obtain task context if present
		taskCtx, _ := GetTaskContext(id)
		if taskCtx == nil {
			taskCtx = context.Background()
			NewTaskContext(taskCtx, id, 0)
		}

		out, err := tools.ExecuteToolCtx(taskCtx, t, callParams)
		timer.End()
		duration := time.Since(start).String()

		v.uiStore.AddEvent(id, "Verifier", "tool "+t.Name, "ok", duration)

		if err != nil {
			logx.Error("Verifier", "error executing tool=%s: %v", toolName, err)
			storeResult(id, Result{
				Status: "error",
				Err:    err.Error(),
			})
			return
		}

		stepResults[toolName] = out
	}

	// send the result and the intent type to the Analyst agent
	v.bus.Send("analyst", bus.Message{
		Type: "summarize",
		Payload: map[string]any{
			"id":        id,
			"intent":    intentType,
			"rawResult": stepResults,
		},
	})
}

func (v *Verifier) resumeFromState(st *state.ExecutionState) {
	id := st.ID
	intentType := st.Intent
	pipe, ok := v.cfg.Pipelines[st.Pipeline]
	if !ok {
		v.storeError(id, "pipeline not found on resume")
		return
	}

	logx.Info("Verifier", "resuming pipeline=%s id=%s from step=%d", pipe.Name, id, st.StepIndex)

	stepResults := st.StepResults
	if stepResults == nil {
		stepResults = make(map[string]any)
	}

	params := st.Params
	if params == nil {
		params = make(map[string]string)
	}

	// CONTINUAMOS EN EL SIGUIENTE STEP
	startAt := st.StepIndex + 1

	// Always restore a *fresh* execution context for resumed pipelines
	taskCtx, _ := GetTaskContext(id)
	if taskCtx == nil || taskCtx.Err() != nil {
		// regenerate context to avoid "deadline exceeded"
		taskCtx = context.Background()
		// store new context without TTL (0 = no expiration)
		_ = NewTaskContext(taskCtx, id, 0)
		logx.Info("Verifier", "[%s] regenerated fresh LLM context (resume)", id)
	}

	for index := startAt; index < len(pipe.Steps); index++ {
		step := pipe.Steps[index]

		// 1️⃣ Paso humano → PAUSA OTRA VEZ
		if step.HumanGate != "" {
			nextState := &state.ExecutionState{
				ID:          id,
				Intent:      intentType,
				Pipeline:    pipe.Name,
				StepIndex:   index,
				Params:      params,
				StepResults: stepResults,
				Gate:        step.HumanGate,
			}
			if err := v.stateManager.Save(context.Background(), nextState); err != nil {
				v.storeError(id, "error saving new human gate state")
				return
			}

			logx.Info("Verifier", "[%s] awaiting human at gate=%s (resume)", id, step.HumanGate)
			v.uiStore.AddEvent(id, "Verifier", "await_human", step.HumanGate, "resume")

			v.bus.Send("api", bus.Message{
				Type: "await_human",
				Payload: map[string]any{
					"id":   id,
					"gate": step.HumanGate,
				},
			})
			return
		}

		// 2️⃣ Paso analyst → pasa rawResults al Analyst y termina
		if step.Analyst {
			logx.Debug("Verifier", "analyst step on resume → calling Analyst")
			v.bus.Send("analyst", bus.Message{
				Type: "summarize",
				Payload: map[string]any{
					"id":        id,
					"intent":    intentType,
					"rawResult": stepResults,
				},
			})
			return
		}

		// 3️⃣ Paso tool
		if step.Tool == "" {
			v.storeError(id, "invalid empty tool in resume")
			return
		}

		t, ok := v.cfg.Tools[step.Tool]
		if !ok {
			v.storeError(id, "tool not found: "+step.Tool)
			return
		}

		logx.Info("Verifier", "resuming tool=%s id=%s", step.Tool, id)

		// merge params (igual que en ejecución normal)
		callParams := make(map[string]string)
		for k, v := range params {
			callParams[k] = v
		}
		for k, v := range step.WithParams {
			if callParams[k] == "" {
				callParams[k] = v
			}
		}

		// ejecutar tool
		timer := logx.Start(id, "Verifier", "tool_"+t.Name)
		out, err := tools.ExecuteToolCtx(taskCtx, t, callParams)
		timer.End()

		if err != nil {
			v.storeError(id, "tool error: "+err.Error())
			return
		}

		stepResults[step.Tool] = out
		v.uiStore.AddEvent(id, "Verifier", "tool "+step.Tool, "ok (resume)", "")

		// actualizar ExecutionState en RAM por si hay otro pause después
		params = callParams
	}

	// 4️⃣ Si no hubo analyst explícito → manda al Analyst para resumen final
	v.bus.Send("analyst", bus.Message{
		Type: "summarize",
		Payload: map[string]any{
			"id":        id,
			"intent":    intentType,
			"rawResult": stepResults,
		},
	})
}

func (v *Verifier) handleContinuePipeline(msg bus.Message) {
	id := msg.Payload["id"].(string)

	st, err := v.stateManager.Load(context.Background(), id)
	if err != nil || st == nil {
		v.storeError(id, "no execution state found")
		return
	}

	// limpiamos estado viejo
	v.stateManager.Delete(context.Background(), id)

	v.resumeFromState(st)
}

func (v *Verifier) handleHumanDecision(msg bus.Message) {
	id, ok := payload.GetString(msg.Payload, "id")
	if !ok {
		logx.Error("Verifier", "invalid human_decision payload: missing id")
		return
	}
	decision, ok := payload.GetString(msg.Payload, "decision")
	if !ok {
		logx.Error("Verifier", "invalid human_decision payload: missing decision")
		return
	}

	st, err := v.stateManager.Load(context.Background(), id)
	if err != nil || st == nil {
		v.storeError(id, "no execution state found")
		return
	}

	if decision != "approved" {
		_ = v.stateManager.Delete(context.Background(), id)
		v.storeError(id, "human rejected review at gate: "+st.Gate)
		return
	}

	// limpiar estado y reanudar
	_ = v.stateManager.Delete(context.Background(), id)
	v.resumeFromState(st)
}

func (v *Verifier) storeError(id, errMsg string) {
	logx.Error("Verifier", "[%s] %s", id, errMsg)

	if v.stateManager != nil {
		_ = v.stateManager.Delete(context.Background(), id)
	}

	storeResult(id, Result{
		Status: "error",
		Err:    errMsg,
	})

	if v.uiStore != nil {
		v.uiStore.AddEvent(id, "Verifier", "error", errMsg, "")
	}
}

func buildCallParams(vars map[string]interface{}, defaults map[string]string) map[string]interface{} {
	out := make(map[string]interface{})

	// copy existing context vars first
	for k, v := range vars {
		out[k] = v
	}

	// add defaults only if missing
	for k, v := range defaults {
		if _, exists := out[k]; !exists {
			out[k] = v
		}
	}
	return out
}

func convertVarsToStringMap(vars map[string]interface{}) map[string]string {
	out := make(map[string]string)
	for k, v := range vars {
		switch vv := v.(type) {
		case string:
			out[k] = vv
		default:
			out[k] = fmt.Sprintf("%v", vv)
		}
	}
	return out
}
