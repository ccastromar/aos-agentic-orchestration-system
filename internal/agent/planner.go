package agent

import (
	"context"

	"github.com/ccastromar/aos-agent-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/guard"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/llm"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/logx"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/payload"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/ui"
)

type Planner struct {
	bus       *bus.Bus
	cfg       *config.Config
	inbox     chan bus.Message
	llmClient llm.LLMClient
	uiStore   *ui.UIStore
}

func NewPlanner(b *bus.Bus, cfg *config.Config, llmClient llm.LLMClient, ui *ui.UIStore) *Planner {
	return &Planner{
		bus:       b,
		cfg:       cfg,
		inbox:     make(chan bus.Message, 16),
		llmClient: llmClient,
		uiStore:   ui,
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
	userMsg, _ := payload.GetString(msg.Payload, "message")

	logx.Debug("Planner", "detect_intent id=%s msg='%s'", id, userMsg)

	// obtain task context if present
	taskCtx, _ := GetTaskContext(id)
	logx.Warn("Planner", "GetTaskContext(%s) → %v", id, taskCtx)

	if taskCtx == nil {
		taskCtx = context.Background()
		NewTaskContext(taskCtx, id, 0)
	}
	logx.Warn("Planner", "CTX STATUS for %s → %v", id, taskCtx.Err())

	// Fast path: if operation is provided, skip LLM detection
	var detectedType string
	if op, ok := msg.Payload["operation"].(string); ok && op != "" {
		detectedType = op
	} else {
		intentKeys := make(map[string]any)
		for k := range p.cfg.Intents {
			intentKeys[k] = true
		}

		timer := logx.Start(id, "Planner", "DetectIntentLLM")
		di, err := llm.DetectIntent(taskCtx, p.llmClient, userMsg, intentKeys)
		timer.End()
		if err != nil {
			logx.Error("Planner", "[%s] ERROR detecting intent: %v", id, err)
			storeResult(id, Result{Status: "error", Err: err.Error()})
			return
		}
		logx.Debug("Planner", "raw intent LLM='%s'", di.Type)
		detectedType = di.Type
	}

	intentCfg, ok := p.cfg.Intents[detectedType]
	if !ok {
		p.storeError(id, "intent unknown")
		return
	}

	// select pipeline
	pipeName := intentCfg.Pipeline
	pipe, ok := p.cfg.Pipelines[pipeName]
	if !ok {
		p.storeError(id, "pipeline inexistent for that intent")
		return
	}

	params := map[string]string{}

	// Get required params from YAML
	required := intentCfg.RequiredParams

	if op, ok := msg.Payload["operation"].(string); ok && op != "" {
		// Structured path: use provided params (map[string]any -> map[string]string)
		if mp, ok := msg.Payload["params"].(map[string]any); ok && mp != nil {
			for k, v := range mp {
				if s, ok := v.(string); ok {
					params[k] = s
				}
			}
		}
	} else {
		if len(required) > 0 {
			timer := logx.Start(id, "Planner", "ExtractParams")
			extracted, err := llm.ExtractParams(taskCtx, p.llmClient, userMsg, required)
			timer.End()

			if err != nil {
				logx.Error("Planner", "[%s] ERROR extracting params: %v", id, err)
				p.storeError(id, "error extracting params")
				return
			}

			params = extracted
		}
	}
	// Validate params
	if err := guard.ValidateAll(intentCfg, pipe, params, p.cfg.Tools); err != nil {
		logx.L(id, "Guard", "validation failed: %v", err)
		storeResult(id, Result{
			Status: "error",
			Err:    err.Error(),
		})
		return
	}

	logx.Info("Planner", "id=%s intent=%s pipeline=%s params=%v",
		id, detectedType, pipeName, params)
	p.uiStore.AddEvent(id, "Planner", "intent", detectedType, "")

	timer2 := logx.Start(id, "Planner", "DispatchPipeline")

	// Send the message to Verifier
	p.bus.Send("verifier", bus.Message{
		Type: "run_pipeline",
		Payload: map[string]any{
			"id":       id,
			"intent":   detectedType,
			"pipeline": pipe,
			"params":   params,
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
