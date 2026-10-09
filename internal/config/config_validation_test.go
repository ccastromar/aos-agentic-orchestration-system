package config

import (
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

var paramRegex = regexp.MustCompile(`\{\{\s*\.([a-zA-Z0-9_]+)\s*\}\}`)

func TestYAMLDefinitionsIntegrity(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))

	// Load the entire configuration
	cfg, err := LoadFromDir(filepath.Join(root, "definitions"))
	if err != nil {
		t.Fatalf("Failed to load configuration: %v", err)
	}

	// Build lookup maps
	pipelinesMap := make(map[string]Pipeline)
	for _, p := range cfg.Pipelines {
		pipelinesMap[p.Name] = p
	}

	toolsMap := make(map[string]Tool)
	for _, tl := range cfg.Tools {
		toolsMap[tl.Name] = tl
	}

	// 1. Verify every Intent has a valid Pipeline
	for _, intent := range cfg.Intents {
		t.Run("Intent:"+intent.Type, func(t *testing.T) {
			if intent.Pipeline == "" {
				t.Logf("Intent %s has no pipeline defined", intent.Type)
				return
			}

			pipeline, ok := pipelinesMap[intent.Pipeline]
			if !ok {
				t.Errorf("Intent %s references non-existent pipeline: %s", intent.Type, intent.Pipeline)
				return
			}

			// Intent declared parameters
			declaredParams := make(map[string]bool)
			for _, p := range intent.RequiredParams {
				declaredParams[p] = true
			}

			// If it's a linear pipeline, we must verify each step
			if pipeline.Mode == "" || pipeline.Mode == "linear" {
				for i, step := range pipeline.Steps {
					if step.Agent != "" {
						if _, ok := cfg.Agents[step.Agent]; !ok {
							t.Errorf("Pipeline %s (Step %d) references non-existent agent: %s", pipeline.Name, i, step.Agent)
						}
						if step.WithParams["task"] == "" {
							t.Errorf("Pipeline %s (Step %d) agent %s is missing with_params.task", pipeline.Name, i, step.Agent)
						}
						validateStepParams(t, step, declaredParams, intent.Type)
						continue
					}
					if step.Tool == "" {
						continue // It might be an analyst step or human gate
					}
					validateStepParams(t, step, declaredParams, intent.Type)
					if strings.HasPrefix(step.Tool, "system.") {
						continue // Internal tools are handled by the Verifier, not the tools registry
					}

					tl, ok := toolsMap[step.Tool]
					if !ok {
						t.Errorf("Pipeline %s (Step %d) references non-existent tool: %s", pipeline.Name, i, step.Tool)
						continue
					}

					validateToolParams(t, tl, declaredParams, step.WithParams, intent.Type)
				}
			} else if pipeline.Mode == "dag" {
				// Validate DAG steps
				for _, step := range pipeline.Steps {
					if step.Agent != "" {
						if _, ok := cfg.Agents[step.Agent]; !ok {
							t.Errorf("DAG Pipeline %s references non-existent agent: %s", pipeline.Name, step.Agent)
						}
						if step.WithParams["task"] == "" {
							t.Errorf("DAG Pipeline %s agent %s is missing with_params.task", pipeline.Name, step.Agent)
						}
						validateStepParams(t, step, declaredParams, intent.Type)
						continue
					}
					if step.Tool == "" {
						continue
					}
					validateStepParams(t, step, declaredParams, intent.Type)
					if strings.HasPrefix(step.Tool, "system.") {
						continue // Internal tools are handled by the Verifier, not the tools registry
					}

					tl, ok := toolsMap[step.Tool]
					if !ok {
						t.Errorf("DAG Pipeline %s references non-existent tool: %s", pipeline.Name, step.Tool)
						continue
					}

					validateToolParams(t, tl, declaredParams, step.WithParams, intent.Type)
				}
			}
		})
	}
}

func validateStepParams(t *testing.T, step PipelineStep, declaredParams map[string]bool, intentType string) {
	t.Helper()
	for _, value := range step.WithParams {
		for _, match := range paramRegex.FindAllStringSubmatch(value, -1) {
			if len(match) > 1 && !declaredParams[match[1]] && !isBuiltinPipelineParam(match[1]) {
				t.Errorf("Intent %s -> agent %s uses parameter '{{ .%s }}' but it is not listed in intent.required_params", intentType, step.Agent, match[1])
			}
		}
	}
}

func isBuiltinPipelineParam(name string) bool {
	return name == "context" || name == "message" || name == "callbackUrl" || name == "callbackToken" || name == "callback_url" || name == "callback_token" || name == "trace_id" || name == "audit_hash" || strings.HasPrefix(name, "ctx_") || strings.HasPrefix(name, "ctx.")
}

func validateToolParams(t *testing.T, tl Tool, declaredParams map[string]bool, boundParams map[string]string, intentType string) {
	// Extract parameters from URL and Body
	var usedParams []string

	if tl.URL != "" {
		matches := paramRegex.FindAllStringSubmatch(tl.URL, -1)
		for _, m := range matches {
			if len(m) > 1 {
				usedParams = append(usedParams, m[1])
			}
		}
	}

	if tl.Body != nil {
		for _, v := range tl.Body {
			matches := paramRegex.FindAllStringSubmatch(v, -1)
			for _, m := range matches {
				if len(m) > 1 {
					usedParams = append(usedParams, m[1])
				}
			}
		}
	}

	for _, p := range usedParams {
		// Ignore common built-in parameters or context parameters if needed
		// Here we assume ALL template parameters MUST be listed in intent.required_params
		_, bound := boundParams[p]
		if !declaredParams[p] && !bound && !isBuiltinPipelineParam(p) {
			t.Errorf("Intent %s -> Tool %s uses parameter '{{ .%s }}' but it is not listed in intent.required_params", intentType, tl.Name, p)
		}
	}
}
