package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/logx"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/tools"
)

func (v *Verifier) runAgentStep(ctx context.Context, executionID string, definition config.AgentDefinition, params map[string]string, step config.PipelineStep) (map[string]any, error) {
	if step.TimeoutMs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(step.TimeoutMs)*time.Millisecond)
		defer cancel()
	}
	return tools.ExecuteAgent(ctx, definition, params, func(event tools.AgentEvent) {
		if v.eventBus == nil {
			return
		}
		data, _ := json.Marshal(event)
		message := event.Type
		if event.SessionID != "" {
			message += " session=" + event.SessionID
		}
		v.eventBus.PublishEventWithData(executionID, "Agent", event.Type, message, "", string(data))
	})
}

func (v *Verifier) publishAgentFailure(executionID string, agentName string, err error) {
	if v.eventBus == nil || err == nil {
		return
	}
	logx.Warn("Verifier", "agent=%s execution=%s failed: %v", agentName, executionID, err)
	data, _ := json.Marshal(map[string]any{"type": "agent.failed", "agent": agentName, "error": err.Error()})
	v.eventBus.PublishEventWithData(executionID, "Agent", "agent.failed", err.Error(), "", string(data))
}
