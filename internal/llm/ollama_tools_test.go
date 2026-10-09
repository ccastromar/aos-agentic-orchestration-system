package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOllamaNativeToolStepReturnsStructuredArguments(t *testing.T) {
	c := NewOllamaClient("http://ollama.test", "qwen2.5-coder:7b", "embed")
	c.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"message":{"role":"assistant","content":"","tool_calls":[{"type":"function","function":{"name":"workspace.read_file","arguments":{"path":"pom.xml"}}}]},"done":true,"total_duration":5000000000,"load_duration":500000000,"prompt_eval_count":100,"prompt_eval_duration":1000000000,"eval_count":20,"eval_duration":2000000000}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(body)), Header: make(http.Header)}, nil
	})}
	step, err := c.ReActToolStep(context.Background(), "inspect project", []config.Tool{{Name: "workspace.read_file", Description: "Read file"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if step.Action != "workspace.read_file" || step.ActionInput != `{"path":"pom.xml"}` {
		t.Fatalf("unexpected native step: %#v", step)
	}
	if step.LLMStats == nil || step.LLMStats.OutputTokensPerSecond != 10 || step.LLMStats.PromptTokensPerSecond != 100 || step.LLMStats.TotalDurationSeconds != 5 {
		t.Fatalf("unexpected Ollama runtime stats: %#v", step.LLMStats)
	}
}

func TestOllamaNativeToolStepKeepsCompleteToolCallWhenResponseReachesTokenLimit(t *testing.T) {
	c := NewOllamaClient("http://ollama.test", "qwen3:4b", "embed")
	c.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"message":{"role":"assistant","content":"","tool_calls":[{"type":"function","function":{"name":"workspace.search","arguments":{"query":"TaskController"}}}]},"done":true,"done_reason":"length"}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(body)), Header: make(http.Header)}, nil
	})}
	step, err := c.ReActToolStep(context.Background(), "find controller", []config.Tool{{Name: "workspace.search"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if step.Action != "workspace.search" || step.ActionInput != `{"query":"TaskController"}` {
		t.Fatalf("complete tool call was discarded after reaching token limit: %#v", step)
	}
}

func TestOllamaNativeToolStepReplaysToolCallAndResult(t *testing.T) {
	c := NewOllamaClient("http://ollama.test", "qwen2.5-coder:7b", "embed")
	c.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Messages []ollamaNativeMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if len(request.Messages) != 4 {
			t.Fatalf("unexpected message count: %d", len(request.Messages))
		}
		assistant := request.Messages[2]
		if assistant.Role != "assistant" || len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].Function.Name != "workspace.inspect_project" {
			t.Fatalf("previous assistant tool call was not replayed: %#v", assistant)
		}
		toolResult := request.Messages[3]
		if toolResult.Role != "tool" || toolResult.ToolName != "workspace.inspect_project" || toolResult.Content != `{"project_type":"maven"}` {
			t.Fatalf("previous tool result was not replayed: %#v", toolResult)
		}
		body := `{"message":{"role":"assistant","content":"Project inspected."},"done":true}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(body)), Header: make(http.Header)}, nil
	})}
	history := []state.ReActStep{{
		Thought:     "Inspecting project",
		Action:      "workspace.inspect_project",
		ActionInput: `{"path":"."}`,
		Observation: `{"project_type":"maven"}`,
	}}
	step, err := c.ReActToolStep(context.Background(), "inspect project", []config.Tool{{Name: "workspace.inspect_project"}}, history)
	if err != nil {
		t.Fatal(err)
	}
	if step.Action != "FINISH" || step.ActionInput != "Project inspected." {
		t.Fatalf("unexpected final step: %#v", step)
	}
}

func TestWorkspaceWriteToolSchemaRequiresPathAndContent(t *testing.T) {
	schema := workspaceToolSchema("workspace.write_file")
	required := schema["required"].([]string)
	if len(required) != 2 || required[0] != "path" || required[1] != "content" {
		t.Fatalf("unexpected required fields: %#v", required)
	}
}

func TestParseToolCallFromMessageContent(t *testing.T) {
	call, ok := parseToolCallContent(`{"name":"workspace.inspect_project","arguments":{"path":"."}}`)
	if !ok {
		t.Fatal("expected content tool call to be detected")
	}
	if call.Function.Name != "workspace.inspect_project" || call.Function.Arguments["path"] != "." {
		t.Fatalf("unexpected call: %#v", call)
	}
}

func TestParseToolCallEmbeddedAfterExplanation(t *testing.T) {
	content := "Creating tests for TaskController using MockMvc. First, I'll search for the `TaskController` class.\n\n" +
		`{"name": "workspace.search", "arguments": {"query": "public class TaskController"}}`
	call, ok := parseToolCallContent(content)
	if !ok {
		t.Fatal("expected embedded content tool call to be detected")
	}
	if call.Function.Name != "workspace.search" || call.Function.Arguments["query"] != "public class TaskController" {
		t.Fatalf("unexpected call: %#v", call)
	}
}

func TestOllamaNativeToolStepUsesEmbeddedContentToolCall(t *testing.T) {
	c := NewOllamaClient("http://ollama.test", "qwen2.5-coder:7b", "embed")
	c.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"message":{"role":"assistant","content":"Creating tests. First I'll inspect the controller.\n\n{\"name\":\"workspace.search\",\"arguments\":{\"query\":\"public class TaskController\"}}"},"done":true}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(body)), Header: make(http.Header)}, nil
	})}
	step, err := c.ReActToolStep(context.Background(), "create tests", []config.Tool{{Name: "workspace.search"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if step.Action != "workspace.search" || step.ActionInput != `{"query":"public class TaskController"}` {
		t.Fatalf("embedded content call was not executed: %#v", step)
	}
}

func TestOllamaNativeToolStepAllowsFinalAnswerThatMentionsTool(t *testing.T) {
	c := NewOllamaClient("http://ollama.test", "qwen3:8b", "embed")
	c.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"message":{"role":"assistant","content":"The DTO is complete. You can run workspace.run_tests to verify it."},"done":true}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(body)), Header: make(http.Header)}, nil
	})}
	step, err := c.ReActToolStep(context.Background(), "create DTO", []config.Tool{{Name: "workspace.run_tests"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if step.Action != "FINISH" {
		t.Fatalf("final answer mentioning a tool was rejected: %#v", step)
	}
}

func TestOllamaNativeToolStepUsesContentToolCallBeforeFinish(t *testing.T) {
	c := NewOllamaClient("http://ollama.test", "qwen2.5-coder:7b", "embed")
	c.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"message":{"role":"assistant","content":"{\"name\":\"workspace.inspect_project\",\"arguments\":{\"path\":\".\"}}"},"done":true}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(body)), Header: make(http.Header)}, nil
	})}
	step, err := c.ReActToolStep(context.Background(), "inspect project", []config.Tool{{Name: "workspace.inspect_project"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if step.Action != "workspace.inspect_project" || step.ActionInput != `{"path":"."}` {
		t.Fatalf("content call was treated as finish: %#v", step)
	}
}

func TestParseNaturalSearchToolIntent(t *testing.T) {
	call, ok := parseNaturalToolIntent(`Calling workspace.search with query "TaskController"`, []config.Tool{{Name: "workspace.search"}})
	if !ok {
		t.Fatal("expected natural-language tool intent to be detected")
	}
	if call.Function.Name != "workspace.search" || call.Function.Arguments["query"] != "TaskController" {
		t.Fatalf("unexpected call: %#v", call)
	}
}

func TestParseNaturalSearchToolIntentWithoutQuotes(t *testing.T) {
	call, ok := parseNaturalToolIntent(`Calling workspace.search with query: TaskController`, []config.Tool{{Name: "workspace.search"}})
	if !ok {
		t.Fatal("expected unquoted natural-language tool intent to be detected")
	}
	if call.Function.Name != "workspace.search" || call.Function.Arguments["query"] != "TaskController" {
		t.Fatalf("unexpected call: %#v", call)
	}
}

func TestOllamaNativeToolStepDoesNotFinishOnNaturalToolIntent(t *testing.T) {
	c := NewOllamaClient("http://ollama.test", "qwen2.5-coder:7b", "embed")
	c.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"message":{"role":"assistant","content":"Calling workspace.search with query \"TaskController\""},"done":true}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(body)), Header: make(http.Header)}, nil
	})}
	step, err := c.ReActToolStep(context.Background(), "find controller", []config.Tool{{Name: "workspace.search"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if step.Action != "workspace.search" || step.ActionInput != `{"query":"TaskController"}` {
		t.Fatalf("natural intent was treated as finish: %#v", step)
	}
}
