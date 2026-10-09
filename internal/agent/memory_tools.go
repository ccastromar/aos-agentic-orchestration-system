package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
	"github.com/google/uuid"
)

// executeMemoryTool reports success only after the store has accepted the operation.
func (v *Verifier) executeMemoryTool(ctx context.Context, tool, sessionID string, params map[string]any) (map[string]any, error) {
	if v.stateManager == nil || v.stateManager.Vector() == nil {
		return nil, fmt.Errorf("vector store not configured")
	}
	if sessionID == "" {
		return nil, fmt.Errorf("memory owner required")
	}
	vs := v.stateManager.Vector()
	out := map[string]any{"executed": true}
	switch tool {
	case "system.memory.add":
		fact, _ := params["fact"].(string)
		if strings.TrimSpace(fact) == "" {
			return nil, fmt.Errorf("fact required")
		}
		if v.getLLMClient() == nil {
			return nil, fmt.Errorf("embedding client not configured")
		}
		emb, err := v.getLLMClient().Embed(ctx, fact)
		if err != nil {
			return nil, err
		}
		err = vs.AddMemory(ctx, state.Memory{ID: uuid.NewString(), SessionID: sessionID, Text: fact, Embedding: emb, Timestamp: time.Now(), Metadata: map[string]any{"kind": "personal_fact"}})
		if err != nil {
			return nil, err
		}
		out["status"] = "fact added to memory"
		out["fact"] = fact
	case "system.memory.list":
		memories, err := vs.ListMemories(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		facts := []string{}
		for _, m := range memories {
			if m.IsPersonalFact() {
				facts = append(facts, m.Text)
			}
		}
		out["memories"] = facts
	case "system.memory.delete":
		target, _ := params["fact_to_forget"].(string)
		if strings.TrimSpace(target) == "" {
			return nil, fmt.Errorf("fact_to_forget required")
		}
		memories, err := vs.ListMemories(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		deleted := 0
		for _, m := range memories {
			if m.ID == target || strings.EqualFold(strings.TrimSpace(m.Text), strings.TrimSpace(target)) {
				if err := vs.DeleteMemory(ctx, sessionID, m.ID); err != nil {
					return nil, err
				}
				deleted++
			}
		}
		out["deleted"] = deleted
		out["status"] = fmt.Sprintf("deleted %d matching memories; use the memory ID or exact text", deleted)
	case "system.memory.clear":
		if err := vs.ClearMemories(ctx, sessionID); err != nil {
			return nil, err
		}
		out["status"] = "all memories cleared"
	default:
		return nil, fmt.Errorf("unknown memory tool: %s", tool)
	}
	return out, nil
}
