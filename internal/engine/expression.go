package engine

import (
	"fmt"
	"strings"

	"github.com/Knetic/govaluate"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
)

// EvalBoolExpression evaluate  "${amount > 1000}" or "amount > 1000"
func EvalBoolExpression(raw string, ctx *PipelineContext) (bool, error) {
	expr := strings.TrimSpace(raw)

	if strings.HasPrefix(expr, "${") && strings.HasSuffix(expr, "}") {
		expr = strings.TrimPrefix(expr, "${")
		expr = strings.TrimSuffix(expr, "}")
	}

	e, err := govaluate.NewEvaluableExpression(expr)
	if err != nil {
		return false, fmt.Errorf("parse error in expression %q: %w", expr, err)
	}

	params := map[string]interface{}{}
	for k, v := range ctx.GetVarsSnapshot() {
		params[k] = v
	}

	res, err := e.Evaluate(params)
	if err != nil {
		return false, fmt.Errorf("eval error in expression %q: %w", expr, err)
	}

	b, ok := res.(bool)
	if !ok {
		return false, fmt.Errorf("expression %q did not evaluate to bool", expr)
	}
	return b, nil
}

func StepShouldRun(step config.PipelineStep, ctx *PipelineContext) bool {
	if step.When == "" {
		return true
	}
	ok, err := EvalBoolExpression(step.When, ctx)
	if err != nil {
		// log.Printf("error evaluating when=%q: %v", step.When, err)
		return false
	}
	return ok
}
