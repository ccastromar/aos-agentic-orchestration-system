package agent

import (
	"context"
	"strings"

	"github.com/ccastromar/aos-agent-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/guard"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/llm"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/logx"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/payload"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/state"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/ui"
)

type Planner struct {
	bus          *bus.Bus
	cfg          *config.Config
	inbox        chan bus.Message
	llmClient    llm.LLMClient
	uiStore      *ui.UIStore
	stateManager *state.StateManager
}

func NewPlanner(b *bus.Bus, cfg *config.Config, llmClient llm.LLMClient, ui *ui.UIStore, smOpt ...*state.StateManager) *Planner {
	var sm *state.StateManager
	if len(smOpt) > 0 {
		sm = smOpt[0]
	} else {
		// Fallback for tests
		sm = state.NewStateManager(state.NewMemoryStore())
	}
	return &Planner{
		bus:          b,
		cfg:          cfg,
		inbox:        make(chan bus.Message, 16),
		llmClient:    llmClient,
		uiStore:      ui,
		stateManager: sm,
	}
}

func (p *Planner) Inbox() chan bus.Message {
	return p.inbox
}
func (p *Planner) Start(ctx context.Context) error {
	defer func() {
		if r := recover(); r != nil {
			logx.Error("Planner", "panic recovered in Start: %v", r)
		}
	}()
	for {
		select {
		case msg := <-p.inbox:
			func() {
				defer func() {
					if r := recover(); r != nil {
						logx.Error("Planner", "panic recovered in dispatch: %v", r)
					}
				}()
				p.dispatch(msg)
			}()

		case <-ctx.Done():
			return nil
		}
	}
}

func (p *Planner) dispatch(msg bus.Message) {
	switch msg.Type {
	case "detect_intent":
		p.handleDetectIntent(msg)

	case "clarify_intent":
		p.handleClarifyIntent(msg)

	case "new_task":
		// From Inspector
		id, ok := payload.GetString(msg.Payload, "id")
		if !ok {
			logx.Error("Planner", "invalid payload: missing id")
			return
		}
		logx.Debug("Planner", "got new_task id=%s -> forward to detect_intent", id)

		p.bus.Send("planner", bus.Message{
			Type: "detect_intent",
			Payload: map[string]any{
				"id":      id,
				"message": msg.Payload["message"],
				"mode":    msg.Payload["mode"],
			},
		})

	default:
		logx.Warn("Planner", "unknown message: %#v", msg)
	}

}

func (p *Planner) handleDetectIntent(msg bus.Message) {
	id, ok := payload.GetString(msg.Payload, "id")
	if !ok {
		logx.Error("Planner", "invalid payload: missing id")
		return
	}
	sessionID, _ := payload.GetString(msg.Payload, "session_id")
	userMsg, _ := payload.GetString(msg.Payload, "message")
	lang, ok := payload.GetString(msg.Payload, "lang")
	if !ok {
		logx.Error("Planner", "invalid payload: missing lang")
	}
	logx.Debug("Planner", "detect_intent id=%s msg='%s'", id, userMsg)

	// ---------------------------------------------------------
	// Ensure task context exists
	// ---------------------------------------------------------
	taskCtx, _ := GetTaskContext(id)
	if taskCtx == nil {
		taskCtx = context.Background()
		NewTaskContext(taskCtx, id, 0)
	}

	var detectedType string
	var params map[string]string

	// ---------------------------------------------------------
	// 1) STRUCTURED PATH (operation provided)
	// ---------------------------------------------------------
	if op, ok := msg.Payload["operation"].(string); ok && op != "" {
		detectedType = op

		// Convert params any->string
		params = make(map[string]string)
		if mp, ok := msg.Payload["params"].(map[string]any); ok {
			for k, v := range mp {
				if s, ok := v.(string); ok {
					params[k] = s
				}
			}
		}

	} else {

		// ---------------------------------------------------------
		// 2) NATURAL LANGUAGE PATH → One shot intent + params
		// ---------------------------------------------------------
		sessionContextStr := ""
		if sessionID != "" {
			sess, err := p.stateManager.LoadSession(taskCtx, sessionID)
			if err == nil && sess != nil && len(sess.Interactions) > 0 {
				var sb strings.Builder
				sb.WriteString("=== SHORT-TERM MEMORY (Recent History) ===\n")
				for _, inter := range sess.Interactions {
					sb.WriteString("User: " + inter.UserMessage + "\n")
					sb.WriteString("System: " + inter.Summary + "\n")
				}
				sessionContextStr = sb.String()
			}

			// Vector Memory Injection
			if vs := p.stateManager.Vector(); vs != nil && p.llmClient != nil {
				embedding, err := p.llmClient.Embed(taskCtx, userMsg)
				if err == nil {
					mems, err := vs.SearchMemories(taskCtx, sessionID, embedding, 3)
					if err == nil && len(mems) > 0 {
						var sb strings.Builder
						sb.WriteString("=== LONG-TERM KNOWLEDGE (RAG) ===\n")
						for _, mem := range mems {
							sb.WriteString(mem.Text + "\n---\n")
						}
						sessionContextStr = sb.String() + "\n" + sessionContextStr
					}
				} else {
					logx.Error("Planner", "Failed to embed user message for RAG: %v", err)
				}
			}
		}

		timer := logx.Start(id, "Planner", "DetectIntentAndParamsLLM")

		di, err := llm.DetectIntentAndParams(taskCtx, p.llmClient, userMsg, p.cfg.Intents, sessionContextStr)
		timer.End()

		if err != nil {
			logx.Error("Planner", "[%s] ERROR detecting intent/params: %v", id, err)
			// uiStore might be nil in unit tests; guard to avoid panic so we can store the error
			if p.uiStore != nil {
				p.uiStore.AddEvent(
					id,
					"Planner",
					"failed",
					err.Error(),
					"",
				)
			}
			p.storeError(id, err.Error())
			return
		}

		// functional errors
		if len(di.Errors) > 0 {
			logx.Info("Planner", "[%s] missing params: %v", id, di.Errors)

			if p.uiStore != nil {
				p.uiStore.AddEvent(
					id,
					"Planner",
					"await_clarification",
					strings.Join(di.Errors, ", "),
					"",
				)
			}

			st := &state.ExecutionState{
				ID:            id,
				SessionID:     sessionID,
				Intent:        di.Type,
				Params:        di.Params,
				MissingParams: di.Errors,
				Gate:          "clarification",
			}
			_ = p.stateManager.Save(taskCtx, st)

			p.bus.Send("api", bus.Message{
				Type: "await_human",
				Payload: map[string]any{
					"id":   id,
					"gate": "clarification",
				},
			})

			storeResult(id, Result{
				Status: "await_clarification",
				Err:    "missing required parameters",
				Data: map[string]any{
					"intent":  di.Type,
					"missing": di.Errors,
				},
			})

			return
		}

		logx.Debug("Planner", "LLM returned intent='%s' params=%v errors=%v", di.Type, di.Params, di.Errors)

		// Intent returned by LLM
		detectedType = di.Type
		params = di.Params

		if params == nil {
			params = map[string]string{}
		}
	}

	// ---------------------------------------------------------
	// 3) Intent must exist in config
	// ---------------------------------------------------------
	intentCfg, ok := p.cfg.Intents[detectedType]
	if !ok {
		p.storeError(id, "intent unknown: "+detectedType)
		return
	}

	// ---------------------------------------------------------
	// 4) Resolve pipeline
	// ---------------------------------------------------------
	pipeName := intentCfg.Pipeline
	pipe, ok := p.cfg.Pipelines[pipeName]
	if !ok {
		p.storeError(id, "pipeline not found: "+pipeName)
		return
	}

	// ---------------------------------------------------------
	// 5) Validate params (Guard)
	// ---------------------------------------------------------
	if err := guard.ValidateAll(intentCfg, pipe, params, p.cfg.Tools); err != nil {
		logx.L(id, "Guard", "validation failed: %v", err)
		storeResult(id, Result{
			Status: "error",
			Err:    err.Error(),
		})
		return
	}

	// ---------------------------------------------------------
	// 6) Log + UI event
	// ---------------------------------------------------------
	logx.Info("Planner", "id=%s intent=%s pipeline=%s params=%v", id, detectedType, pipeName, params)
	p.uiStore.AddEvent(id, "Planner", "intent", detectedType, "")

	// ---------------------------------------------------------
	// 7) Dispatch pipeline to Verifier
	// ---------------------------------------------------------
	timer2 := logx.Start(id, "Planner", "DispatchPipeline")
	p.bus.Send("verifier", bus.Message{
		Type: "run_pipeline",
		Payload: map[string]any{
			"id":         id,
			"session_id": sessionID,
			"intent":     detectedType,
			"pipeline":   pipe,
			"params":     params,
			"language":   lang,
			"message":    userMsg,
		},
	})
	timer2.End()
}

func (p *Planner) storeError(id string, errMsg string) {
	storeResult(id, Result{
		Status: "error",
		Err:    errMsg,
	})
}

func (p *Planner) handleClarifyIntent(msg bus.Message) {
	id, _ := payload.GetString(msg.Payload, "id")
	userMsg, _ := payload.GetString(msg.Payload, "message")

	taskCtx, _ := GetTaskContext(id)
	if taskCtx == nil {
		p.storeError(id, "task context not found")
		return
	}

	st, err := p.stateManager.Load(taskCtx, id)
	if err != nil {
		p.storeError(id, "state load error: "+err.Error())
		return
	}
	if st == nil || st.Gate != "clarification" {
		p.storeError(id, "task not awaiting clarification")
		return
	}

	newParams, err := llm.ExtractParams(taskCtx, p.llmClient, userMsg, st.MissingParams)
	if err != nil {
		p.storeError(id, "clarification param extraction failed: "+err.Error())
		return
	}

	if st.Params == nil {
		st.Params = make(map[string]string)
	}
	for k, v := range newParams {
		if v != "" {
			st.Params[k] = v
		}
	}

	intentCfg, ok := p.cfg.Intents[st.Intent]
	if !ok {
		p.storeError(id, "unknown intent from state: "+st.Intent)
		return
	}

	pipeName := intentCfg.Pipeline
	pipe, ok := p.cfg.Pipelines[pipeName]
	if !ok {
		p.storeError(id, "pipeline not found: "+pipeName)
		return
	}

	var missing []string
	for _, req := range intentCfg.RequiredParams {
		if val, exists := st.Params[req]; !exists || val == "" {
			missing = append(missing, req)
		}
	}

	if len(missing) > 0 {
		st.MissingParams = missing
		_ = p.stateManager.Save(taskCtx, st)

		if p.uiStore != nil {
			p.uiStore.AddEvent(id, "Planner", "await_clarification", strings.Join(missing, ", "), "")
		}
		p.bus.Send("api", bus.Message{
			Type: "await_human",
			Payload: map[string]any{"id": id, "gate": "clarification"},
		})
		storeResult(id, Result{
			Status: "await_clarification",
			Err:    "missing required parameters",
			Data:   map[string]any{"intent": st.Intent, "missing": missing},
		})
		return
	}

	st.Gate = ""
	st.MissingParams = nil
	_ = p.stateManager.Save(taskCtx, st)

	if err := guard.ValidateAll(intentCfg, pipe, st.Params, p.cfg.Tools); err != nil {
		storeResult(id, Result{Status: "error", Err: err.Error()})
		return
	}

	p.uiStore.AddEvent(id, "Planner", "intent", st.Intent, "")
	p.bus.Send("verifier", bus.Message{
		Type: "run_pipeline",
		Payload: map[string]any{
			"id":         id,
			"session_id": st.SessionID,
			"intent":     st.Intent,
			"pipeline":   pipe,
			"params":     st.Params,
			"language":   "es",
			"message":    userMsg,
		},
	})
}
