package llm

import (
	"encoding/json"
	"testing"
)

func TestRepairBacktickStringsMakesMultilineContentValidJSON(t *testing.T) {
	input := "{\"action\":\"workspace.write_file\",\"action_input\":{\"path\":\"Example.java\",\"content\":`line one\\nline two`}}"
	repaired := repairBacktickStrings(input)
	var value map[string]any
	if err := json.Unmarshal([]byte(repaired), &value); err != nil {
		t.Fatalf("repaired JSON is invalid: %v\n%s", err, repaired)
	}
	params := value["action_input"].(map[string]any)
	if params["content"] != "line one\\nline two" {
		t.Fatalf("unexpected content: %q", params["content"])
	}
}

func TestRepairMalformedWriteActionPreservesUnescapedSourceQuotes(t *testing.T) {
	input := `{
  "thought": "Create the test",
  "action": "workspace.write_file",
  "action_input": {
    "path": "src/test/ExampleTest.java",
    "content": "mockMvc.perform(get(\"/tasks\"))\n    .andExpect(content().json("[{\"id\":1}]"));\n"
  }
}`
	repaired, ok := repairMalformedWriteAction(input)
	if !ok {
		t.Fatal("expected malformed write action to be repaired")
	}
	var value struct {
		Action string            `json:"action"`
		Input  map[string]string `json:"action_input"`
	}
	if err := json.Unmarshal([]byte(repaired), &value); err != nil {
		t.Fatalf("repaired action is invalid: %v\n%s", err, repaired)
	}
	if value.Action != "workspace.write_file" || value.Input["path"] != "src/test/ExampleTest.java" {
		t.Fatalf("unexpected repaired action: %#v", value)
	}
	if value.Input["content"] == "" {
		t.Fatal("source content was lost")
	}
}

func TestRepairMalformedWriteActionCanonicalizesInventedWriteAlias(t *testing.T) {
	input := `{"thought":"write test","action":"workspace.create_or_replace_file","action_input":{"path":"Example.java","content":"assertJson("{\"ok\":true}");\n"}}`
	repaired, ok := repairMalformedWriteAction(input)
	if !ok {
		t.Fatal("expected alias action to be repaired")
	}
	var value struct {
		Action string            `json:"action"`
		Input  map[string]string `json:"action_input"`
	}
	if err := json.Unmarshal([]byte(repaired), &value); err != nil {
		t.Fatalf("invalid repaired action: %v", err)
	}
	if value.Action != "workspace.write_file" || value.Input["content"] == "" {
		t.Fatalf("unexpected repaired action: %#v", value)
	}
}

func TestCanonicalReActAction(t *testing.T) {
	if got := canonicalReActAction("workspace.create_or_replace_file"); got != "workspace.write_file" {
		t.Fatalf("unexpected canonical action: %s", got)
	}
}
