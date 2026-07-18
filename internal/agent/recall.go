package agent

import (
	"context"
	"strings"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
)

// maxRecallExecutions caps how many past executions are handed to the Analyst
// when answering a recall question.
const maxRecallExecutions = 10

// buildRecallResult collects the recent executions of a session so the
// Analyst can answer questions like "what have I done today?". The current
// execution (the recall pipeline itself) is excluded. Executions come back
// newest first.
func buildRecallResult(ctx context.Context, sm *state.StateManager, sessionID, currentID string) map[string]any {
	executions := []map[string]any{}
	if sm == nil {
		return map[string]any{"executions": executions, "count": 0}
	}

	states, err := sm.ListExecutionStates(ctx)
	if err != nil {
		return map[string]any{"executions": executions, "count": 0, "error": err.Error()}
	}

	for _, st := range states {
		if st.SessionID != sessionID || st.ID == currentID {
			continue
		}
		// System intents (past recalls, preference saves) are meta-operations,
		// not something the user "did".
		if strings.HasPrefix(st.Intent, "system.") {
			continue
		}
		record := map[string]any{
			"intent":     st.Intent,
			"pipeline":   st.Pipeline,
			"created_at": st.CreatedAt.Format(time.RFC3339),
		}
		// _pipeline holds the Analyst wrap-up; the rest are per-tool results
		toolResults := make(map[string]any, len(st.StepResults))
		for k, v := range st.StepResults {
			if k == "_pipeline" {
				if wrap, ok := v.(map[string]any); ok {
					if summary, ok := wrap["summary"].(string); ok && summary != "" {
						record["summary"] = summary
					}
				}
				continue
			}
			toolResults[k] = v
		}
		if facts := extractFacts(toolResults); len(facts) > 0 {
			record["facts"] = facts
		}
		if st.Error != "" {
			record["error"] = st.Error
		}
		executions = append(executions, record)
		if len(executions) >= maxRecallExecutions {
			break
		}
	}

	return map[string]any{"executions": executions, "count": len(executions)}
}
