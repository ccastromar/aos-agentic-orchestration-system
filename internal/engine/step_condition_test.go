package engine

import (
	"testing"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
)

func TestStepConditions(t *testing.T) {
	gate := config.PipelineStep{HumanGate: "manager_review", When: "${amount > 1000}"}
	action := config.PipelineStep{Tool: "pay", When: "${amount > 1000}"}
	cases := []struct {
		name   string
		amount any
		step   config.PipelineStep
		want   bool
	}{
		{"small amount skips the approval", "300", gate, false},
		{"threshold itself skips it", "1000", gate, false},
		{"large amount needs it", "1500", gate, true},
		{"unreadable amount keeps the approval", "a lot", gate, true},
		{"unreadable amount skips an action", "a lot", action, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := NewPipelineContext(map[string]any{"amount": tc.amount})
			ctx.Vars = AutoCastVars(ctx.Vars)
			if got := StepShouldRun(tc.step, ctx); got != tc.want {
				t.Fatalf("StepShouldRun = %v, want %v", got, tc.want)
			}
		})
	}
}
