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
	if native, ok := client.(interface {
		ReActToolStep(context.Context, string, []config.Tool, []state.ReActStep) (*state.ReActStep, error)
	}); ok {
		return native.ReActToolStep(ctx, task, tools, history)
	}
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

	recoveryInstruction := ""
	if len(history) > 0 && strings.Contains(history[len(history)-1].Observation, "STAGNATION WARNING") {
		if history[len(history)-1].Action == "workspace.read_file" {
			recoveryInstruction = "RECOVERY MODE: rewrite the complete broken file with workspace.write_file using the content you just read and the compiler errors. Do not use workspace.replace_in_file."
		} else {
			recoveryInstruction = "RECOVERY MODE: your next action must be workspace.read_file for the broken file. After reading it, rewrite the complete file with workspace.write_file. Do not use workspace.replace_in_file."
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
%s
Before analyzing a project, call workspace.inspect_project with path "." to determine its build system, versions, dependencies and correct test runner. Do not search for a manifest that inspect_project already reports.
Use relative paths for every workspace tool. Use "." to refer to the project root. Never repeat the absolute workspace root in action_input.
Do not repeat an identical action with identical input. Use each observation to choose the next action.
Use only an exact tool name from AVAILABLE TOOLS. To create or replace a file, the tool name is workspace.write_file; never invent create_file, edit_file or create_or_replace_file.
For read-only analysis, do not write files. Read the relevant manifest and source files, then provide a concrete answer.
When tests fail, inspect the error and relevant files, make a correction, and run tests again.
Before creating or changing a test, read the controller/service/entity signatures it uses. Never invent constructors or method signatures.
Do not claim that you will fix something without executing a tool action that performs the fix.
Use FINISH only when the task is resolved. action_input for FINISH must contain a non-empty final answer summarizing evidence, changes and test outcome.
You must respond ONLY in valid JSON format with exactly the following fields:
{
  "thought": "Your reasoning about what to do next based on the task and observation history.",
  "action": "The exact name of the tool to use. If you have finished the task, use 'FINISH'.",
  "action_input": "A JSON object containing parameters for the tool. If FINISH, put your final answer as a string here."
}

Do not include any text outside the JSON object. Do not wrap it in Markdown code blocks (like `+"`"+`json). Just output the raw JSON.`,
		task, toolsDesc.String(), historyDesc.String(), recoveryInstruction)

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
		// Small local models sometimes encode multiline source using JavaScript
		// template literals. Convert paired backticks to proper JSON strings.
		repaired := repairBacktickStrings(cleanJSON)
		if retryErr := json.Unmarshal([]byte(repaired), &step); retryErr != nil {
			writeRepaired, ok := repairMalformedWriteAction(cleanJSON)
			if !ok || json.Unmarshal([]byte(writeRepaired), &step) != nil {
				return nil, fmt.Errorf("failed to parse ReAct JSON output (err: %v): %s", err, cleanJSON)
			}
		}
	}
	step.Action = canonicalReActAction(step.Action)

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

func repairBacktickStrings(input string) string {
	for {
		start := strings.IndexByte(input, '`')
		if start < 0 {
			return input
		}
		endOffset := strings.IndexByte(input[start+1:], '`')
		if endOffset < 0 {
			return input
		}
		end := start + 1 + endOffset
		encoded, _ := json.Marshal(input[start+1 : end])
		input = input[:start] + string(encoded) + input[end+1:]
	}
}

func repairMalformedWriteAction(input string) (string, bool) {
	action, ok := extractJSONLikeString(input, "action")
	if !ok || canonicalReActAction(action) != "workspace.write_file" {
		return "", false
	}
	action = "workspace.write_file"
	thought, ok := extractJSONLikeString(input, "thought")
	if !ok {
		return "", false
	}
	path, ok := extractJSONLikeString(input, "path")
	if !ok {
		return "", false
	}
	marker := `"content"`
	markerAt := strings.Index(input, marker)
	if markerAt < 0 {
		return "", false
	}
	colonAt := strings.IndexByte(input[markerAt+len(marker):], ':')
	if colonAt < 0 {
		return "", false
	}
	valueAt := markerAt + len(marker) + colonAt + 1
	for valueAt < len(input) && (input[valueAt] == ' ' || input[valueAt] == '\n' || input[valueAt] == '\r' || input[valueAt] == '\t') {
		valueAt++
	}
	if valueAt >= len(input) || input[valueAt] != '"' {
		return "", false
	}
	valueAt++
	lastBrace := strings.LastIndexByte(input, '}')
	if lastBrace < valueAt {
		return "", false
	}
	contentEnd := strings.LastIndexByte(input[:lastBrace], '"')
	if contentEnd < valueAt {
		return "", false
	}
	content := decodeLooseEscapes(input[valueAt:contentEnd])
	rebuilt, err := json.Marshal(map[string]any{
		"thought": thought,
		"action":  action,
		"action_input": map[string]string{
			"path":    path,
			"content": content,
		},
	})
	if err != nil {
		return "", false
	}
	return string(rebuilt), true
}

func canonicalReActAction(action string) string {
	switch strings.TrimSpace(action) {
	case "workspace.create_or_replace_file", "workspace.create_file", "workspace.edit_file":
		return "workspace.write_file"
	case "workspace.list_files", "workspace.list_tree":
		return "workspace.tree"
	case "workspace.test", "workspace.run_project_tests":
		return "workspace.run_tests"
	default:
		return strings.TrimSpace(action)
	}
}

func extractJSONLikeString(input, field string) (string, bool) {
	marker := `"` + field + `"`
	start := strings.Index(input, marker)
	if start < 0 {
		return "", false
	}
	colon := strings.IndexByte(input[start+len(marker):], ':')
	if colon < 0 {
		return "", false
	}
	i := start + len(marker) + colon + 1
	for i < len(input) && (input[i] == ' ' || input[i] == '\n' || input[i] == '\r' || input[i] == '\t') {
		i++
	}
	if i >= len(input) || input[i] != '"' {
		return "", false
	}
	i++
	var value strings.Builder
	for i < len(input) {
		if input[i] == '"' {
			return value.String(), true
		}
		if input[i] == '\\' && i+1 < len(input) {
			switch input[i+1] {
			case 'n':
				value.WriteByte('\n')
			case 'r':
				value.WriteByte('\r')
			case 't':
				value.WriteByte('\t')
			case '"':
				value.WriteByte('"')
			case '\\':
				value.WriteByte('\\')
			default:
				value.WriteByte(input[i+1])
			}
			i += 2
			continue
		}
		value.WriteByte(input[i])
		i++
	}
	return "", false
}

func decodeLooseEscapes(value string) string {
	return strings.NewReplacer(`\n`, "\n", `\r`, "\r", `\t`, "\t", `\"`, `"`, `\\`, `\`).Replace(value)
}
