package config

import (
	"fmt"
	"sort"
	"strings"
)

// AIUses lists what makes a pipeline an AI system, in the sense that matters for governance: a
// model or an agent produces part of what it does. Empty means the pipeline is deterministic (a
// lookup, a transformation, a call to a plain API), so Prudence has nothing to decide about it.
func (c *Config) AIUses(pipe Pipeline) []string {
	var uses []string
	for i, step := range pipe.Steps {
		label := stepLabel(step, i)
		switch {
		case step.Agent != "":
			uses = append(uses, fmt.Sprintf("step %s runs agent %s", label, step.Agent))
		case step.Analyst:
			uses = append(uses, fmt.Sprintf("step %s is an analyst (model) step", label))
		case step.Tool != "" && c.toolRequiresLLM(step.Tool):
			uses = append(uses, fmt.Sprintf("step %s calls model-backed tool %s", label, step.Tool))
		case step.Tool != "" && c.Tools[step.Tool].Type == "a2a":
			uses = append(uses, fmt.Sprintf("step %s calls remote agent %s", label, step.Tool))
		}
	}
	// The closing summary is written by the model unless the pipeline sets skip_summary.
	if !pipe.SkipSummary {
		uses = append(uses, "it ends with a model-written summary (set skip_summary)")
	}
	return uses
}

// validateGovernanceScope makes every pipeline say whether it is an AI system: governed: true
// (it answers to Prudence) or governed: false with ungoverned_reason (it is not one). A missing
// answer, a reason on a governed pipeline, or a governed: false pipeline that names an AI system
// or uses AI is a configuration error, so AOS does not start.
func validateGovernanceScope(cfg *Config) error {
	names := make([]string, 0, len(cfg.Pipelines))
	for name := range cfg.Pipelines {
		names = append(names, name)
	}
	sort.Strings(names)

	var problems []string
	for _, name := range names {
		pipe := cfg.Pipelines[name]
		reason := strings.TrimSpace(pipe.UngovernedReason)
		switch {
		case pipe.Governed == nil:
			problems = append(problems, fmt.Sprintf("pipeline %s does not say whether it is governed: set governed: true (an AI system, decided by Prudence) or governed: false with ungoverned_reason (not an AI system)", name))
		case pipe.IsGoverned():
			if reason != "" {
				problems = append(problems, fmt.Sprintf("pipeline %s is governed but states ungoverned_reason: remove one", name))
			}
		default: // governed: false
			if reason == "" {
				problems = append(problems, fmt.Sprintf("pipeline %s is governed: false without ungoverned_reason: say why it is not an AI system", name))
			}
			if strings.TrimSpace(pipe.AiSystem) != "" {
				problems = append(problems, fmt.Sprintf("pipeline %s is governed: false but names ai_system %s: remove one", name, pipe.AiSystem))
			}
			if uses := cfg.AIUses(pipe); len(uses) > 0 {
				problems = append(problems, fmt.Sprintf("pipeline %s is governed: false but uses AI (%s): set governed: true", name, strings.Join(uses, "; ")))
			}
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("pipelines must declare their governance:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}
