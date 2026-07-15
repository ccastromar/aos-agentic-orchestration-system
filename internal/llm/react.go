package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
)

// AskReActStep requests the next ReAct action from the LLM.
// It returns a partially populated state.ReActStep containing Thought, Action, and ActionInput.
func AskReActStep(ctx context.Context, client LLMClient, task string, tools []config.Tool, history []state.ReActStep) (*state.ReActStep, error) {
	// 1. Construct Tools Description
	var toolsDesc strings.Builder
	for _, t := range tools {
		toolsDesc.WriteString(fmt.Sprintf("- %s: %s\n", t.Name, t.Description))
		if len(t.Method) > 0 {
			toolsDesc.WriteString("  Params should be passed as a JSON object matching tool needs.\n")
		}
	}

	// 2. Construct History
	var historyDesc strings.Builder
	if len(history) == 0 {
		historyDesc.WriteString("(No actions taken yet)\n")
	} else {
		for i, h := range history {
			historyDesc.WriteString(fmt.Sprintf("\nStep %d:\n", i+1))
			historyDesc.WriteString(fmt.Sprintf("Thought: %s\n", h.Thought))
			historyDesc.WriteString(fmt.Sprintf("Action: %s\n", h.Action))
			historyDesc.WriteString(fmt.Sprintf("ActionInput: %s\n", h.ActionInput))
			historyDesc.WriteString(fmt.Sprintf("Observation: %s\n", h.Observation))
		}
	}

	prompt := fmt.Sprintf(`You are an autonomous ReAct agent. Your goal is to solve the task by reasoning and calling tools.

TASK:
%s

AVAILABLE TOOLS:
%s

CONVERSATION HISTORY (Previous Steps):
%s

INSTRUCTIONS:
You must respond ONLY in valid JSON format with exactly the following fields:
{
  "thought": "Your reasoning about what to do next based on the task and observation history.",
  "action": "The exact name of the tool to use. If you have finished the task, use 'FINISH'.",
  "action_input": "A JSON object containing parameters for the tool. If FINISH, put your final answer as a string here."
}

Do not include any text outside the JSON object. Do not wrap it in Markdown code blocks (like `+"`"+`json). Just output the raw JSON.`,
		task, toolsDesc.String(), historyDesc.String())

	resp, err := client.Chat(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("failed to call LLM for ReAct: %w", err)
	}

	// Clean up markdown formatting if the model still included it
	cleanJSON := strings.TrimSpace(resp)
	cleanJSON = strings.TrimPrefix(cleanJSON, "```json")
	cleanJSON = strings.TrimPrefix(cleanJSON, "```")
	cleanJSON = strings.TrimSuffix(cleanJSON, "```")
	cleanJSON = strings.TrimSpace(cleanJSON)

	var step struct {
		Thought     string `json:"thought"`
		Action      string `json:"action"`
		ActionInput any    `json:"action_input"`
	}

	if err := json.Unmarshal([]byte(cleanJSON), &step); err != nil {
		return nil, fmt.Errorf("failed to parse ReAct JSON output (err: %v): %s", err, cleanJSON)
	}

	// Convert ActionInput to string regardless of whether it's an object or string
	inputBytes, _ := json.Marshal(step.ActionInput)
	inputStr := string(inputBytes)
	
	// If it's a raw string like "final answer", unmarshal stringified version
	if strInput, ok := step.ActionInput.(string); ok {
		inputStr = strInput
	}

	return &state.ReActStep{
		Thought:     step.Thought,
		Action:      step.Action,
		ActionInput: inputStr,
	}, nil
}
