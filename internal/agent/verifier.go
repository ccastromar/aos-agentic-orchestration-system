package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/engine"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/logx"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/payload"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/llm"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/ui"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/tools"
)

type Verifier struct {
	bus          *bus.Bus
	cfg          *config.Config
	llmClient    llm.LLMClient
	llmMutex     sync.RWMutex
	inbox        chan bus.Message
	uiStore      *ui.UIStore
	stateManager *state.StateManager
}

func (v *Verifier) SetLLMClient(c llm.LLMClient) {
	v.llmMutex.Lock()
	defer v.llmMutex.Unlock()
	v.llmClient = c
}

func (v *Verifier) getLLMClient() llm.LLMClient {
	v.llmMutex.RLock()
	defer v.llmMutex.RUnlock()
	return v.llmClient
}

func NewVerifier(b *bus.Bus, cfg *config.Config, llmClient llm.LLMClient, ui *ui.UIStore, smOpt ...*state.StateManager) *Verifier {
	var sm *state.StateManager
	if len(smOpt) > 0 && smOpt[0] != nil {
		sm = smOpt[0]
	} else {
		// default to in-memory store
		mem := state.NewMemoryStore()
		sm = state.NewStateManager(mem)
	}
	return &Verifier{
		bus:          b,
		cfg:          cfg,
		llmClient:    llmClient,
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
	if !ok || language == "" {
		language = "es"
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
	sessionID, _ := payload.GetString(msg.Payload, "session_id")

	taskCtx, _ := GetTaskContext(id)
	if taskCtx == nil {
		taskCtx = context.Background()
		NewTaskContext(taskCtx, id, 0)
	}

	switch pipe.Mode {
	case "dag":
		executeDAG(v, pipe, id, sessionID, intentType, language, taskCtx, stepResults, baseParams)
	case "react":
		executeReAct(v, pipe, id, sessionID, intentType, language, taskCtx, stepResults, baseParams)
	case "linear":
		fallthrough
	default:
		executeLinear(v, pipe, id, sessionID, intentType, language, taskCtx, stepResults, baseParams)
	}

	logx.Debug("Verifier", "[%s] Final Vars: %#v", id, ctx.Vars)
	logx.Debug("Verifier", "[%s] ToolOutputs: %#v", id, ctx.ToolOutputs)
}

func (v *Verifier) resumeFromState(st *state.ExecutionState) {
	id := st.ID
	intentType := st.Intent
	pipe, ok := v.cfg.Pipelines[st.Pipeline]
	if !ok {
		v.storeError(id, "pipeline not found on resume")
		return
	}

	logx.Info("Verifier", "resuming pipeline=%s id=%s from completed_steps=%d", pipe.Name, id, len(st.CompletedSteps))

	stepResults := st.StepResults
	if stepResults == nil {
		stepResults = make(map[string]any)
	}

	baseParams := make(map[string]any)
	for k, val := range st.Params {
		baseParams[k] = val
	}

	taskCtx, _ := GetTaskContext(id)
	if taskCtx == nil || taskCtx.Err() != nil {
		taskCtx = context.Background()
		_ = NewTaskContext(taskCtx, id, 0)
		logx.Info("Verifier", "[%s] regenerated fresh task context (resume)", id)
	}

	switch pipe.Mode {
	case "dag":
		executeDAG(v, pipe, id, st.SessionID, intentType, "es", taskCtx, stepResults, baseParams)
	case "react":
		executeReAct(v, pipe, id, st.SessionID, intentType, "es", taskCtx, stepResults, baseParams)
	case "linear":
		fallthrough
	default:
		executeLinear(v, pipe, id, st.SessionID, intentType, "es", taskCtx, stepResults, baseParams)
	}
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

	// Do NOT delete the state here! executeDAG/executeLinear need to load it from the store
	// to properly restore the pipeline's progress instead of starting from scratch!
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

	// add defaults only if missing, but render them first
	stringVars := convertVarsToStringMap(vars)
	for k, v := range defaults {
		if _, exists := out[k]; !exists {
			rendered, err := tools.RenderTemplateString(v, stringVars)
			if err == nil {
				out[k] = rendered
			} else {
				out[k] = v // fallback
			}
		}
	}
	return out
}

func flattenVars(prefix string, v interface{}, out map[string]string) {
	switch vv := v.(type) {
	case string:
		out[prefix] = vv
	case map[string]interface{}:
		for k, val := range vv {
			newKey := k
			if prefix != "" {
				newKey = prefix + "." + k
			}
			flattenVars(newKey, val, out)
		}
	default:
		out[prefix] = fmt.Sprintf("%v", vv)
	}
}

func convertVarsToStringMap(vars map[string]interface{}) map[string]string {
	out := make(map[string]string)
	for k, v := range vars {
		flattenVars(k, v, out)
	}
	return out
}
