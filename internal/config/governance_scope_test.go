package config

import (
	"strings"
	"testing"
)

func scopeConfig(pipe Pipeline) *Config {
	return &Config{
		Tools: map[string]Tool{
			"lookup":    {Name: "lookup", Type: "http", Mode: "read"},
			"summarize": {Name: "summarize", Type: "llm", Mode: "read"},
			"remote":    {Name: "remote", Type: "a2a", Mode: "read"},
		},
		Pipelines: map[string]Pipeline{pipe.Name: pipe},
	}
}

func TestAIUses(t *testing.T) {
	cases := []struct {
		name string
		pipe Pipeline
		want string // a fragment of the reason; empty: not an AI system
	}{
		{"plain lookup with no summary", Pipeline{Name: "p", SkipSummary: true, Steps: []PipelineStep{{Tool: "lookup"}}}, ""},
		{"model-written summary", Pipeline{Name: "p", Steps: []PipelineStep{{Tool: "lookup"}}}, "skip_summary"},
		{"dag with no summary", Pipeline{Name: "p", Mode: "dag", SkipSummary: true, Steps: []PipelineStep{{ID: "a", Tool: "lookup"}}}, ""},
		{"dag summary", Pipeline{Name: "p", Mode: "dag", Steps: []PipelineStep{{ID: "a", Tool: "lookup"}}}, "skip_summary"},
		{"agent step", Pipeline{Name: "p", SkipSummary: true, Steps: []PipelineStep{{ID: "code", Agent: "claude_code"}}}, "agent claude_code"},
		{"analyst step", Pipeline{Name: "p", SkipSummary: true, Steps: []PipelineStep{{Analyst: true}}}, "analyst"},
		{"llm tool", Pipeline{Name: "p", SkipSummary: true, Steps: []PipelineStep{{Tool: "summarize"}}}, "model-backed tool summarize"},
		{"built-in model tool", Pipeline{Name: "p", SkipSummary: true, Steps: []PipelineStep{{Tool: "system.rag_query"}}}, "model-backed tool system.rag_query"},
		{"remote agent over A2A", Pipeline{Name: "p", SkipSummary: true, Steps: []PipelineStep{{Tool: "remote"}}}, "remote agent remote"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := scopeConfig(tc.pipe)
			uses := strings.Join(cfg.AIUses(tc.pipe), "; ")
			if tc.want == "" && uses != "" {
				t.Fatalf("expected no AI use, got %q", uses)
			}
			if tc.want != "" && !strings.Contains(uses, tc.want) {
				t.Fatalf("expected an AI use mentioning %q, got %q", tc.want, uses)
			}
		})
	}
}

func flag(b bool) *bool { return &b }

func TestGovernanceScope(t *testing.T) {
	plain := []PipelineStep{{Tool: "lookup"}}
	cases := []struct {
		name    string
		pipe    Pipeline
		wantErr string // a fragment of the error; empty: loads
	}{
		{"governed AI pipeline", Pipeline{Name: "p", Governed: flag(true), AiSystem: "sys-1", Steps: []PipelineStep{{Agent: "claude_code"}}}, ""},
		{"governed without an AI system yet (decided at run time)", Pipeline{Name: "p", Governed: flag(true), Steps: plain}, ""},
		{"not governed and not an AI system", Pipeline{Name: "p", Governed: flag(false), UngovernedReason: "deterministic lookup, no AI", SkipSummary: true, Steps: plain}, ""},
		{"governed flag missing", Pipeline{Name: "p", Steps: plain}, "does not say whether it is governed"},
		{"not governed without a reason", Pipeline{Name: "p", Governed: flag(false), SkipSummary: true, Steps: plain}, "without ungoverned_reason"},
		{"blank reason is no reason", Pipeline{Name: "p", Governed: flag(false), UngovernedReason: "  ", SkipSummary: true, Steps: plain}, "without ungoverned_reason"},
		{"not governed but uses AI", Pipeline{Name: "p", Governed: flag(false), UngovernedReason: "just a helper", SkipSummary: true, Steps: []PipelineStep{{Agent: "claude_code"}}}, "uses AI"},
		{"not governed but the model writes the summary", Pipeline{Name: "p", Governed: flag(false), UngovernedReason: "lookup", Steps: plain}, "uses AI"},
		{"not governed and names an AI system", Pipeline{Name: "p", Governed: flag(false), UngovernedReason: "lookup", AiSystem: "sys-1", SkipSummary: true, Steps: plain}, "names ai_system"},
		{"governed with a reason", Pipeline{Name: "p", Governed: flag(true), UngovernedReason: "lookup", Steps: plain}, "remove one"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateGovernanceScope(scopeConfig(tc.pipe))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected the pipeline to load, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected an error mentioning %q, got %v", tc.wantErr, err)
			}
		})
	}
}
