package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/logx"
)

// validatePipelineSafety refuses pipelines that run a dangerous tool before any human approval.
// In a linear pipeline the approval must be an earlier step; in a DAG, an ancestor through
// depends_on. A step may skip it only by stating why in no_approval_reason, which then travels
// in every run's pipeline snapshot. The check is structural: it cannot tell whether an approval
// asks the right question, only that one is in the way.
func validatePipelineSafety(cfg *Config) error {
	var problems []string
	names := make([]string, 0, len(cfg.Pipelines))
	for name := range cfg.Pipelines {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		pipe := cfg.Pipelines[name]
		for i, step := range pipe.Steps {
			tool, ok := cfg.Tools[step.Tool]
			if !ok || tool.Mode != "dangerous" {
				continue
			}
			if approvedBefore(pipe, i) {
				continue
			}
			label := stepLabel(step, i)
			if reason := strings.TrimSpace(step.NoApprovalReason); reason != "" {
				logx.Warn("Config", "pipeline %s step %s runs dangerous tool %s without human approval: %s", name, label, step.Tool, reason)
				continue
			}
			problems = append(problems, fmt.Sprintf("pipeline %s step %s runs dangerous tool %s with no human approval before it (add one, or state no_approval_reason)", name, label, step.Tool))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("unsafe pipelines:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}

// approvedBefore reports whether a human approval precedes step i.
func approvedBefore(pipe Pipeline, i int) bool {
	if pipe.Mode != "dag" {
		for _, earlier := range pipe.Steps[:i] {
			if earlier.HumanGate != "" {
				return true
			}
		}
		return false
	}

	byID := make(map[string]PipelineStep, len(pipe.Steps))
	for j, s := range pipe.Steps {
		byID[stepLabel(s, j)] = s
	}
	seen := map[string]bool{}
	pending := append([]string(nil), pipe.Steps[i].DependsOn...)
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		ancestor, ok := byID[id]
		if !ok {
			continue
		}
		if ancestor.HumanGate != "" {
			return true
		}
		pending = append(pending, ancestor.DependsOn...)
	}
	return false
}

func stepLabel(step PipelineStep, i int) string {
	switch {
	case step.ID != "":
		return step.ID
	case step.Tool != "":
		return step.Tool
	default:
		return fmt.Sprintf("step_%d", i)
	}
}
