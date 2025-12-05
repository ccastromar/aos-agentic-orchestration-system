package agent

import (
	"context"

	"github.com/ccastromar/aos-agent-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/llm"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/logx"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/payload"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/ui"
)

type Analyst struct {
	bus       *bus.Bus
	inbox     chan bus.Message
	llmClient llm.LLMClient
	uiStore   *ui.UIStore
}

func NewAnalyst(b *bus.Bus, llmClient llm.LLMClient, ui *ui.UIStore) *Analyst {
	return &Analyst{
		bus:       b,
		inbox:     make(chan bus.Message, 16),
		llmClient: llmClient,
		uiStore:   ui,
	}
}

func (a *Analyst) Inbox() chan bus.Message {
	return a.inbox
}
func (a *Analyst) Start(ctx context.Context) error {
	defer func() {
		if r := recover(); r != nil {
			logx.Error("Analyst", "panic recovered in Start: %v", r)
		}
	}()
	for {
		select {
		case msg := <-a.inbox:
			func() {
				defer func() {
					if r := recover(); r != nil {
						logx.Error("Analyst", "panic recovered in dispatch: %v", r)
					}
				}()
				a.dispatch(msg)
			}()

		case <-ctx.Done():
			return nil
		}
	}
}

func (a *Analyst) dispatch(msg bus.Message) {

	switch msg.Type {
	case "summarize":
		a.handleSummarize(msg)
	default:
		logx.Warn("Analyst", "unknown message: %#v", msg)
	}

}

func (a *Analyst) handleSummarize(msg bus.Message) {
	id, ok := payload.GetString(msg.Payload, "id")
	if !ok {
		logx.Error("Planner", "invalid payload: missing id")
		return
	}
	intentType, _ := msg.Payload["intent"].(string)
	rawAny := msg.Payload["rawResult"]

	raw, ok := rawAny.(map[string]any)
	if !ok {
		logx.Error("Analyst", "rawResult invalid for id=%s", id)
		storeResult(id, Result{
			Status: "error",
			Err:    "invalid rawResult: expected map[string]any, got",
		})
		return
	}

	logx.Info("Analyst", "requesting summary to the LLM...")
	logx.Debug("Analyst", "rawResult: %#v", raw)

	// obtain task context if present
	taskCtx, _ := GetTaskContext(id)
	if taskCtx == nil {
		taskCtx = context.Background()
		NewTaskContext(taskCtx, id, 0)
	}

	timer := logx.Start(id, "Analyst", "SummarizeLLM")
	summary, err := llm.SummarizeResult(taskCtx, a.llmClient, intentType, raw)
	timer.End()

	if err != nil {
		logx.Error("Analyst", "error calling to the LLM: %v", err)
		storeResult(id, Result{
			Status: "ok",
			Data: map[string]any{
				"raw": raw,
			},
		})
		return
	}
	logx.Info("Analyst", "summary generated: %s", summary)
	//	a.uiStore.AddEvent(id, "Analyst", "summary", "summary LLM has been generated", "")
	a.uiStore.AddEvent(id, "Analyst", "summary", summary, "")

	storeResult(id, Result{
		Status: "ok",
		Data: map[string]any{
			"raw":     raw,
			"summary": summary,
		},
	})
}
