package agent

import (
	"strings"
	"testing"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
)

func TestNormalizeThoughtDetectsEquivalentWhitespaceAndCase(t *testing.T) {
	a := normalizeThought("  The tests are FAILING because imports are missing. ")
	b := normalizeThought("the tests are failing because imports are missing.")
	if a != b {
		t.Fatalf("expected normalized thoughts to match: %q != %q", a, b)
	}
}

func TestValidateReActFinishRequiresPassingTestsAfterCodeChange(t *testing.T) {
	history := []state.ReActStep{
		{Action: "workspace.write_file", ActionInput: `{"path":"src/main/java/TaskDto.java","content":"class TaskDto {}"}`},
		{Action: "workspace.run_tests", ActionInput: `{"path":"src/test/java","language":"java"}`, Observation: `{"status":"failed","output":"FAIL ./..."}`},
	}
	if got := validateReActFinish(history); !strings.Contains(got, "latest test run failed") {
		t.Fatalf("expected failed tests to reject FINISH, got %q", got)
	}

	history[1].Observation = `{"status":"passed","runner":"maven"}`
	if got := validateReActFinish(history); got != "" {
		t.Fatalf("expected passing tests to allow FINISH, got %q", got)
	}
}

func TestValidateReActFinishIgnoresDocumentationChanges(t *testing.T) {
	history := []state.ReActStep{{Action: "workspace.write_file", ActionInput: `{"path":"README.md","content":"docs"}`}}
	if got := validateReActFinish(history); got != "" {
		t.Fatalf("documentation-only change should not require tests, got %q", got)
	}
}

func TestStagnationWarningPreservesObservation(t *testing.T) {
	got := stagnationWarning("compiler output")
	if !strings.Contains(got, "STAGNATION WARNING") || !strings.Contains(got, "compiler output") {
		t.Fatalf("unexpected warning: %q", got)
	}
}
