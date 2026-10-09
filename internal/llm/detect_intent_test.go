package llm

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
)

// promptCapturingLLM records the prompt and returns a canned response.
type promptCapturingLLM struct {
	prompt   string
	response string
}

func (f *promptCapturingLLM) Ping(ctx context.Context) error { return nil }
func (f *promptCapturingLLM) Chat(ctx context.Context, prompt string) (string, error) {
	f.prompt = prompt
	return f.response, nil
}
func (f *promptCapturingLLM) Embed(ctx context.Context, text string) ([]float32, error) {
	return []float32{1}, nil
}

func testIntents() map[string]config.Intent {
	return map[string]config.Intent{
		"system.recall_history": {
			Type:        "system.recall_history",
			Description: "Answer questions about what the user has done",
			Examples:    []string{"What operations have I done today?", "¿Qué operaciones he hecho hoy?"},
		},
		"": {
			// Simulates a malformed definition that registered an empty type
			Description: "broken",
		},
	}
}

func TestDetectIntentAndParams_PromptIncludesExamples(t *testing.T) {
	c := &promptCapturingLLM{response: `{"intent":"system.recall_history","confidence":0.9,"language":"es","parameters":{},"errors":[]}`}

	out, err := DetectIntentAndParams(context.Background(), c, "¿qué he hecho hoy?", testIntents(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Type != "system.recall_history" {
		t.Fatalf("expected recall intent, got %q", out.Type)
	}
	if !strings.Contains(c.prompt, "¿Qué operaciones he hecho hoy?") {
		t.Errorf("expected intent examples to be present in the prompt")
	}
}

func TestDetectIntentAndParams_EmptyIntentIsInvalid(t *testing.T) {
	c := &promptCapturingLLM{response: `{"intent":"","confidence":0.5,"language":"es","parameters":{},"errors":[]}`}

	out, err := DetectIntentAndParams(context.Background(), c, "blah", testIntents(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Type != "" {
		t.Fatalf("expected empty type, got %q", out.Type)
	}
	if len(out.Errors) == 0 || out.Errors[0] != "invalid_intent" {
		t.Fatalf("expected invalid_intent error even though a malformed empty-type intent exists, got %#v", out.Errors)
	}
}

func TestIntentCatalogExcludesFallbackAndKeepsMemoryActions(t *testing.T) {
	c := &promptCapturingLLM{response: `{"intent":"memory.add","parameters":{"fact":"My dog's name is Rex"},"language":"en"}`}
	intents := map[string]config.Intent{
		"memory.add":      {Type: "memory.add", RequiredParams: []string{"fact"}, Description: "Remember a fact"},
		"system.fallback": {Type: "system.fallback", Description: "FALLBACK_CATALOG_SENTINEL"},
	}
	out, err := DetectIntentAndParams(context.Background(), c, "Remember that my dog's name is Rex", intents, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(c.prompt, "FALLBACK_CATALOG_SENTINEL") {
		t.Fatal("fallback must not compete with concrete actions")
	}
	if out.Type != "memory.add" || out.Params["fact"] != "My dog's name is Rex" {
		t.Fatalf("unexpected result: %+v", out)
	}
}

func TestMemoryFactsMustComeFromUserMessage(t *testing.T) {
	intents := map[string]config.Intent{"memory.add": {Type: "memory.add", RequiredParams: []string{"fact"}}}
	for _, tc := range []struct {
		name, fact string
		valid      bool
	}{
		{"actual fact", "my dog's name is Rex", true},
		{"copied prompt example", "User is allergic to peanuts", false},
		{"invented fact", "my dog's name is Luna", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &promptCapturingLLM{response: fmt.Sprintf(`{"intent":"memory.add","parameters":{"fact":%q},"new_facts":[%q]}`, tc.fact, tc.fact)}
			got, err := DetectIntentAndParams(context.Background(), c, "Remember that my dog's name is Rex", intents, "")
			if err != nil {
				t.Fatal(err)
			}
			if tc.valid {
				if got.Params["fact"] != tc.fact || len(got.Errors) != 0 {
					t.Fatalf("valid fact rejected: %+v", got)
				}
			} else {
				if got.Params["fact"] != "" || len(got.Errors) == 0 || len(got.NewFacts) != 0 {
					t.Fatalf("ungrounded fact accepted: %+v", got)
				}
			}
			if strings.Contains(c.prompt, "allergic to peanuts") {
				t.Fatal("invented personal fact remains in prompt")
			}
		})
	}
}

func TestProjectChangeOverridesStandaloneCodeGeneration(t *testing.T) {
	intents := map[string]config.Intent{
		"workspace.generate_code": {Type: "workspace.generate_code", RequiredParams: []string{"language", "path", "request"}},
		"workspace.agent_code":    {Type: "workspace.agent_code", RequiredParams: []string{"request"}},
	}
	message := "Añade un endpoint GET /api/tasks/count al proyecto, añade un test de controller y ejecuta los tests."
	c := &promptCapturingLLM{response: `{"intent":"workspace.generate_code","parameters":{"language":"python","path":"/api/tasks/count","request":"añade endpoint"},"new_facts":["añade un test de controller"]}`}

	got, err := DetectIntentAndParams(context.Background(), c, message, intents, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != "workspace.agent_code" {
		t.Fatalf("expected project agent intent, got %q", got.Type)
	}
	if got.Params["request"] != message {
		t.Fatalf("expected complete user request, got %q", got.Params["request"])
	}
	if len(got.Errors) != 0 {
		t.Fatalf("unexpected missing params: %#v", got.Errors)
	}
	if len(got.NewFacts) != 0 {
		t.Fatalf("project task was incorrectly treated as a personal fact: %#v", got.NewFacts)
	}
}
