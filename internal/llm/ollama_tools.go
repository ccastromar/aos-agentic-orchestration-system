package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
)

type ollamaToolCall struct {
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"function"`
}

type ollamaNativeMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content,omitempty"`
	Thinking  string           `json:"thinking,omitempty"`
	ToolName  string           `json:"tool_name,omitempty"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaToolDefinition struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

func (c *OllamaClient) ReActToolStep(ctx context.Context, task string, tools []config.Tool, history []state.ReActStep) (*state.ReActStep, error) {
	messages := []ollamaNativeMessage{
		{Role: "system", Content: nativeReActSystemPrompt},
		{Role: "user", Content: task},
	}
	for _, previous := range history {
		if previous.Action == "FINISH" {
			if previous.Observation != "" {
				messages = append(messages, ollamaNativeMessage{
					Role:    "user",
					Content: "The previous final answer was rejected by the verifier: " + previous.Observation,
				})
			}
			continue
		}
		args := map[string]any{}
		_ = json.Unmarshal([]byte(previous.ActionInput), &args)
		call := ollamaToolCall{Type: "function"}
		call.Function.Name = canonicalReActAction(previous.Action)
		call.Function.Arguments = args
		messages = append(messages,
			// The prior assistant message only needs to preserve the tool call. Replaying
			// chain-of-thought here bloats every subsequent prompt without helping tool execution.
			ollamaNativeMessage{Role: "assistant", ToolCalls: []ollamaToolCall{call}},
			ollamaNativeMessage{Role: "tool", ToolName: call.Function.Name, Content: previous.Observation},
		)
	}

	payload := map[string]any{
		"model": c.Model, "messages": messages, "tools": buildOllamaTools(tools),
		"stream": false,
		// Qwen3 otherwise spends a large output budget on hidden reasoning. ReAct
		// relies on tool observations and short actions, so request direct responses.
		"think":   false,
		"options": map[string]any{"num_ctx": c.NumCtx, "num_predict": c.ReactNumPredict},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama native tool chat failed: status %d, body: %s", resp.StatusCode, string(body))
	}
	var result struct {
		Message    ollamaNativeMessage `json:"message"`
		DoneReason string              `json:"done_reason"`
		ollamaRuntimeStats
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	logOllamaRuntimeStats(c.Model, result.ollamaRuntimeStats)
	uiStats := runtimeStatsForUI(c.Model, result.ollamaRuntimeStats)
	truncated := result.DoneReason == "length"
	thought := strings.TrimSpace(result.Message.Thinking)
	if thought == "" {
		thought = strings.TrimSpace(result.Message.Content)
	}
	if len(result.Message.ToolCalls) == 0 {
		if contentCall, ok := parseToolCallContent(result.Message.Content); ok {
			args, _ := json.Marshal(contentCall.Function.Arguments)
			return &state.ReActStep{
				Thought:     "Calling " + contentCall.Function.Name,
				Action:      canonicalReActAction(contentCall.Function.Name),
				ActionInput: string(args),
				LLMStats:    uiStats,
			}, nil
		}
		if contentCall, ok := parseNaturalToolIntent(result.Message.Content, tools); ok {
			args, _ := json.Marshal(contentCall.Function.Arguments)
			return &state.ReActStep{
				Thought:     strings.TrimSpace(result.Message.Content),
				Action:      canonicalReActAction(contentCall.Function.Name),
				ActionInput: string(args),
				LLMStats:    uiStats,
			}, nil
		}
		if truncated {
			return nil, fmt.Errorf("ollama ReAct response reached OLLAMA_REACT_NUM_PREDICT=%d tokens without a complete tool call (thinking_chars=%d content_chars=%d); increase the limit or verify that this Ollama version honors think=false", c.ReactNumPredict, len(result.Message.Thinking), len(result.Message.Content))
		}
		if thought == "" {
			return nil, fmt.Errorf("ollama returned neither a tool call nor a final answer")
		}
		return &state.ReActStep{Thought: thought, Action: "FINISH", ActionInput: thought, LLMStats: uiStats}, nil
	}
	call := result.Message.ToolCalls[0]
	if call.Function.Name == "" {
		return nil, fmt.Errorf("ollama ReAct response reached OLLAMA_REACT_NUM_PREDICT=%d tokens with an incomplete tool call", c.ReactNumPredict)
	}
	args, _ := json.Marshal(call.Function.Arguments)
	if thought == "" {
		thought = "Calling " + call.Function.Name
	}
	return &state.ReActStep{Thought: thought, Action: canonicalReActAction(call.Function.Name), ActionInput: string(args), LLMStats: uiStats}, nil
}

func parseNaturalToolIntent(content string, tools []config.Tool) (ollamaToolCall, bool) {
	trimmed := strings.TrimSpace(content)
	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, "calling ") && !strings.HasPrefix(lower, "call ") && !strings.HasPrefix(lower, "i will call ") {
		return ollamaToolCall{}, false
	}
	for _, tool := range tools {
		if !strings.Contains(trimmed, tool.Name) {
			continue
		}
		call := ollamaToolCall{Type: "function"}
		call.Function.Name = tool.Name
		call.Function.Arguments = map[string]any{}
		switch tool.Name {
		case "workspace.inspect_project", "workspace.tree":
			call.Function.Arguments["path"] = "."
		case "workspace.search":
			query := quotedValueAfter(trimmed, "query")
			if query == "" {
				query = plainValueAfter(trimmed, "query")
			}
			if query == "" {
				return ollamaToolCall{}, false
			}
			call.Function.Arguments["query"] = query
		case "workspace.read_file", "workspace.git_diff":
			path := quotedValueAfter(trimmed, "path")
			if path == "" {
				path = firstQuotedValue(trimmed)
			}
			if path == "" {
				return ollamaToolCall{}, false
			}
			call.Function.Arguments["path"] = path
		case "workspace.run_tests":
			path := quotedValueAfter(trimmed, "path")
			if path == "" {
				path = "."
			}
			call.Function.Arguments["path"] = path
		default:
			return ollamaToolCall{}, false
		}
		return call, true
	}
	return ollamaToolCall{}, false
}

func quotedValueAfter(input, marker string) string {
	idx := strings.Index(strings.ToLower(input), strings.ToLower(marker))
	if idx < 0 {
		return ""
	}
	return firstQuotedValue(input[idx+len(marker):])
}

func plainValueAfter(input, marker string) string {
	idx := strings.Index(strings.ToLower(input), strings.ToLower(marker))
	if idx < 0 {
		return ""
	}
	value := strings.TrimSpace(input[idx+len(marker):])
	value = strings.TrimSpace(strings.TrimPrefix(value, ":"))
	if value == "" {
		return ""
	}
	if end := strings.IndexAny(value, "\r\n"); end >= 0 {
		value = value[:end]
	}
	return strings.TrimSpace(strings.TrimRight(value, "."))
}

func firstQuotedValue(input string) string {
	start := strings.IndexByte(input, '"')
	if start < 0 {
		return ""
	}
	end := strings.IndexByte(input[start+1:], '"')
	if end < 0 {
		return ""
	}
	return input[start+1 : start+1+end]
}

func parseToolCallContent(content string) (ollamaToolCall, bool) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	if call, ok := parseToolCallJSON([]byte(content)); ok {
		return call, true
	}

	// Some Ollama model templates emit an otherwise valid tool call in message.content,
	// preceded by a short explanation. Decode each JSON object boundary so the fallback
	// remains structured and never has to infer write arguments from prose.
	for offset := 0; offset < len(content); offset++ {
		if content[offset] != '{' {
			continue
		}
		decoder := json.NewDecoder(strings.NewReader(content[offset:]))
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			continue
		}
		if call, ok := parseToolCallJSON(raw); ok {
			return call, true
		}
	}
	return ollamaToolCall{}, false
}

func parseToolCallJSON(data []byte) (ollamaToolCall, bool) {
	var direct struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if json.Unmarshal(data, &direct) == nil && direct.Name != "" {
		call := ollamaToolCall{Type: "function"}
		call.Function.Name = direct.Name
		call.Function.Arguments = direct.Arguments
		return call, true
	}
	var wrapped struct {
		Function struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"function"`
	}
	if json.Unmarshal(data, &wrapped) == nil && wrapped.Function.Name != "" {
		call := ollamaToolCall{Type: "function"}
		call.Function.Name = wrapped.Function.Name
		call.Function.Arguments = wrapped.Function.Arguments
		return call, true
	}
	return ollamaToolCall{}, false
}

func buildOllamaTools(tools []config.Tool) []ollamaToolDefinition {
	result := make([]ollamaToolDefinition, 0, len(tools))
	for _, tool := range tools {
		definition := ollamaToolDefinition{Type: "function"}
		definition.Function.Name = tool.Name
		definition.Function.Description = tool.Description
		definition.Function.Parameters = workspaceToolSchema(tool.Name)
		result = append(result, definition)
	}
	return result
}

func workspaceToolSchema(name string) map[string]any {
	properties := map[string]any{}
	required := []string{}
	add := func(field, description string, isRequired bool) {
		properties[field] = map[string]any{"type": "string", "description": description}
		if isRequired {
			required = append(required, field)
		}
	}
	switch name {
	case "workspace.inspect_project", "workspace.tree":
		add("path", "Path relative to workspace root; normally '.'", false)
	case "workspace.search":
		add("query", "Exact text to search for", true)
		add("path", "Optional relative directory", false)
	case "workspace.read_file", "workspace.git_diff":
		add("path", "File path relative to workspace root", true)
	case "workspace.write_file":
		add("path", "File path relative to workspace root", true)
		add("content", "Complete plain file content without Markdown fences", true)
	case "workspace.replace_in_file":
		add("path", "File path relative to workspace root", true)
		add("old", "Unique exact text to replace", true)
		add("new", "Replacement text", true)
	case "workspace.run_tests":
		add("path", "Relative file path or '.' for the whole project", true)
		add("language", "Optional language: java, python, or go", false)
	default:
		add("path", "Path relative to workspace root", false)
	}
	return map[string]any{"type": "object", "properties": properties, "required": required}
}

const nativeReActSystemPrompt = `You are an autonomous coding agent. Use only the provided tools.
Inspect the project before editing. Use paths relative to the workspace root and "." for the project root.
Read every class signature used by generated tests. Run project tests after changes and fix failures using tool observations.
Do not repeat identical actions. Call tools directly without narrating your plan. Keep final answers to 2-4 concise sentences.
When writing files, put the complete source only in the tool arguments; do not explain the code before or after the call.`
