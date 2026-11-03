package config

import (
	"strings"
	"testing"
)

func safetyConfig(pipe Pipeline) *Config {
	return &Config{
		Tools: map[string]Tool{
			"pay":    {Name: "pay", Mode: "dangerous"},
			"check":  {Name: "check", Mode: "read"},
			"notify": {Name: "notify", Mode: "write"},
		},
		Pipelines: map[string]Pipeline{pipe.Name: pipe},
	}
}

func TestPipelineSafety(t *testing.T) {
	cases := []struct {
		name    string
		pipe    Pipeline
		wantErr bool
	}{
		{"approval before the payment", Pipeline{Name: "p", Steps: []PipelineStep{
			{Tool: "check"}, {HumanGate: "manager"}, {Tool: "pay"}}}, false},
		{"approval after the payment", Pipeline{Name: "p", Steps: []PipelineStep{
			{Tool: "check"}, {Tool: "pay"}, {HumanGate: "manager"}}}, true},
		{"no approval, no reason", Pipeline{Name: "p", Steps: []PipelineStep{{Tool: "pay"}}}, true},
		{"no approval, stated reason", Pipeline{Name: "p", Steps: []PipelineStep{
			{Tool: "pay", NoApprovalReason: "capped amount"}}}, false},
		{"blank reason does not count", Pipeline{Name: "p", Steps: []PipelineStep{
			{Tool: "pay", NoApprovalReason: "  "}}}, true},
		{"writes need no approval", Pipeline{Name: "p", Steps: []PipelineStep{{Tool: "notify"}}}, false},
		{"dag: approval is an ancestor", Pipeline{Name: "p", Mode: "dag", Steps: []PipelineStep{
			{ID: "risk", Tool: "check"},
			{ID: "gate", HumanGate: "manager", DependsOn: []string{"risk"}},
			{ID: "transfer", Tool: "pay", DependsOn: []string{"gate"}}}}, false},
		{"dag: approval only on a parallel branch", Pipeline{Name: "p", Mode: "dag", Steps: []PipelineStep{
			{ID: "risk", Tool: "check"},
			{ID: "gate", HumanGate: "manager", DependsOn: []string{"risk"}},
			{ID: "transfer", Tool: "pay", DependsOn: []string{"risk"}}}}, true},
		{"dag: listed earlier but not depended on", Pipeline{Name: "p", Mode: "dag", Steps: []PipelineStep{
			{ID: "gate", HumanGate: "manager"},
			{ID: "transfer", Tool: "pay"}}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePipelineSafety(safetyConfig(tc.pipe))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "no_approval_reason") {
				t.Fatalf("the error should say how to fix it: %v", err)
			}
		})
	}
}
