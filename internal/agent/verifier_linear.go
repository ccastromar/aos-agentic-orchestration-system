package agent

import (
	"context"
	"fmt"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/metrics"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/telemetry"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"os"
	"strings"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/audit"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/engine"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/logx"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/tools"
)

// executeLinear runs the pipeline steps in strict sequential order.
func executeLinear(v *Verifier, pipe config.Pipeline, id, sessionID, intentType, language string, taskCtx context.Context, stepResults map[string]any, baseParams map[string]any) {
	ctx := engine.NewPipelineContext(baseParams)
	ctx.Vars["trace_id"] = id
	callbackIndex := -1
	callbackStepID := ""
	var callbackDeadline *time.Time
	// Pre-allocate callback credentials so an earlier provider step can include
	// callback_url/callback_token in its outbound request.
	for candidateIndex, candidate := range pipe.Steps {
		if candidate.Type == "await_callback" {
			callbackIndex = candidateIndex
			callbackStepID = candidate.ID
			if callbackStepID == "" {
				callbackStepID = fmt.Sprintf("callback_%d", candidateIndex)
			}
			token := uuid.NewString()
			key := candidate.ID
			if key == "" {
				key = "callback"
			}
			ctx.Vars[key+".callback_token"] = token
			if _, exists := ctx.Vars["callback_token"]; !exists {
				ctx.Vars["callback_token"] = token
			}
			base := os.Getenv("AOS_CALLBACK_BASE_URL")
			if base != "" {
				ctx.Vars["callback_url"] = strings.TrimRight(base, "/") + "/api/v1/callbacks/" + token
			}
			if candidate.TimeoutMs > 0 {
				deadline := time.Now().UTC().Add(time.Duration(candidate.TimeoutMs) * time.Millisecond)
				callbackDeadline = &deadline
			}
		}
	}

	// Flatten params and stepResults into ctx.Vars for evaluation
	execState, _ := v.stateManager.Load(context.Background(), id)
	var params map[string]string
	if execState != nil {
		params = execState.Params
	} else {
		params = map[string]string{}
	}

	for k, val := range params {
		ctx.Vars[k] = val
	}
	for k, val := range stepResults {
		if resMap, ok := val.(map[string]any); ok {
			for mk, mv := range resMap {
				ctx.Vars[k+"."+mk] = mv
			}
		}
	}

	startAt := 0
	if execState != nil {
		startAt = len(execState.CompletedSteps)
	}

	for index := startAt; index < len(pipe.Steps); index++ {
		step := pipe.Steps[index]
		ctx.Vars["audit_hash"] = ""
		if provider, ok := v.auditLogger.(audit.HashProvider); ok {
			ctx.Vars["audit_hash"] = provider.HashForTrace(id)
		}
		ctx.Vars = engine.AutoCastVars(ctx.Vars)

		logx.Info("Verifier", "[%s] Vars before analyst: %#v", id, ctx.Vars)

		if !engine.StepShouldRun(step, ctx) {
			if step.HumanGate != "" {
				logx.Debug("Verifier", "human gate '%s' skipped by condition", step.HumanGate)
				continue
			}
			logx.Debug("Verifier", "skipping step=%s id=%s due to condition", step.Tool, id)

			resultKey := step.Tool
			if step.Agent != "" {
				resultKey = "agent." + step.Agent
			}
			stepResults[resultKey] = map[string]any{
				"status":   "skipped",
				"effect":   "not_executed",
				"executed": false,
				"reason":   "when=false",
			}
			if err := v.logLifecycleAudit(execState, "step.decision", "skipped", map[string]any{"step": step.ID, "tool": step.Tool, "agent": step.Agent, "when": step.When, "decision": "skip"}); err != nil {
				v.failAuditExecution(execState, id, err)
				return
			}
			continue
		}

		if step.Analyst {
			v.bus.Send("analyst", bus.Message{
				Type: "summarize",
				Payload: map[string]any{
					"id":         id,
					"session_id": sessionID,
					"intent":     intentType,
					"pipeline":   pipe.Name,
					"llm_model":  pipe.LLMModel,
					"rawResult":  cloneMap(stepResults),
					"language":   language,
				},
			})
			return
		}

		if step.HumanGate != "" {
			if execState != nil && execState.Gate == step.HumanGate {
				logx.Info("Verifier", "[%s] resuming after human gate: %s", id, step.HumanGate)
				continue
			}

			ctx.Vars[step.HumanGate+"_executed"] = true
			ctx.Vars[step.HumanGate+"_decision"] = "pending"

			nextState := &state.ExecutionState{
				ID:             id,
				SessionID:      sessionID,
				Intent:         intentType,
				Pipeline:       pipe.Name,
				CompletedSteps: linearStepIDs(pipe.Steps[:index]),
				Params:         convertVarsToStringMap(ctx.Vars),
				StepResults:    stepResults,
				Gate:           step.HumanGate,
			}

			if execState != nil {
				nextState.UserID = execState.UserID
				nextState.CreatedAt = execState.CreatedAt
				copyExecutionManifest(nextState, execState)
			}
			if err := v.stateManager.Save(context.Background(), nextState); err != nil {
				v.storeError(id, "cannot persist execution state")
				return
			}
			execState = nextState
			if err := v.logLifecycleAudit(nextState, "human.approval_requested", "pending", map[string]any{"gate": step.HumanGate}); err != nil {
				v.failAuditExecution(nextState, id, err)
				return
			}

			v.eventBus.PublishEvent(id, "Verifier", "await_human", step.HumanGate, "")

			v.bus.Send("api", bus.Message{
				Type: "await_human",
				Payload: map[string]any{
					"id":   id,
					"gate": step.HumanGate,
				},
			})
			return // pipeline pauses here
		}

		toolName := step.Tool
		if step.Agent != "" {
			agentDef, exists := v.cfg.Agents[step.Agent]
			if !exists {
				storeResult(id, Result{Status: "error", Err: fmt.Sprintf("agent %s not found", step.Agent)})
				return
			}
			callParams := convertVarsToStringMap(buildCallParams(ctx.Vars, step.WithParams))
			cleanInputs := audit.SanitizeMap(stringMapToAny(callParams))
			if err := v.logLifecycleAudit(execState, "tool.started", "pending", map[string]any{"step": step.ID, "agent": step.Agent, "decision": "invoke", "input_digest": audit.Digest(cleanInputs), "inputs": cleanInputs}); err != nil {
				v.failAuditExecution(execState, id, err)
				return
			}
			logx.Info("Verifier", "executing agent=%s id=%s", step.Agent, id)
			timer := logx.Start(id, "Verifier", "agent_"+step.Agent)
			out, err := v.runAgentStep(taskCtx, id, agentDef, callParams, step)
			timer.End()
			if err != nil {
				if auditErr := v.logLifecycleAudit(execState, "tool.failed", "error", map[string]any{"step": step.ID, "agent": step.Agent, "error": err.Error()}); auditErr != nil {
					v.failAuditExecution(execState, id, auditErr)
					return
				}
				v.publishAgentFailure(id, step.Agent, err)
				storeResult(id, Result{Status: "error", Err: fmt.Sprintf("agent %s failed: %v", step.Agent, err), Data: map[string]any{"agent." + step.Agent: out}})
				return
			}
			safeOutput := auditSafeToolResult(out)
			if err := v.logLifecycleAudit(execState, "tool.completed", "success", map[string]any{"step": step.ID, "agent": step.Agent, "result": safeOutput, "output_digest": audit.Digest(safeOutput)}); err != nil {
				v.failAuditExecution(execState, id, err)
				return
			}
			resultKey := "agent." + step.Agent
			stepResults[resultKey] = out
			for key, value := range out {
				ctx.Vars[resultKey+"."+key] = value
			}
			continue
		}
		if step.Type == "await_callback" {
			params := buildCallParams(ctx.Vars, step.WithParams)
			token, _ := params["callback_token"].(string)
			if token == "" {
				token = uuid.NewString()
			}
			stepID := step.ID
			if stepID == "" {
				stepID = fmt.Sprintf("callback_%d", index)
			}
			nextState := &state.ExecutionState{ID: id, SessionID: sessionID, Intent: intentType, Pipeline: pipe.Name, CompletedSteps: linearStepIDs(pipe.Steps[:index]), Params: convertVarsToStringMap(ctx.Vars), StepResults: stepResults, Gate: "await_callback", CallbackToken: token, AwaitStep: stepID, Status: "await_callback", AwaitDeadline: callbackDeadline}
			if execState != nil {
				nextState.UserID = execState.UserID
				nextState.CreatedAt = execState.CreatedAt
				copyExecutionManifest(nextState, execState)
			}
			if err := v.stateManager.Save(context.Background(), nextState); err != nil {
				v.storeError(id, err.Error())
				return
			}
			if err := v.logLifecycleAudit(nextState, "callback.waiting", "pending", map[string]any{"step": stepID, "deadline": nextState.AwaitDeadline}); err != nil {
				v.failAuditExecution(nextState, id, err)
				return
			}
			if v.eventBus != nil {
				v.eventBus.PublishEvent(id, "Verifier", "await_callback", token, "")
			}
			return
		}
		resolvedInputs := buildCallParams(ctx.Vars, step.WithParams)
		if err := v.logLifecycleAudit(execState, "tool.started", "pending", map[string]any{
			"step": step.ID, "tool": toolName, "when": step.When, "decision": "invoke",
			"input_digest": audit.Digest(audit.SanitizeMap(resolvedInputs)),
			"inputs":       audit.SanitizeMap(resolvedInputs),
		}); err != nil {
			v.failAuditExecution(execState, id, err)
			return
		}

		if toolName == "system.save_preference" {
			logx.Info("Verifier", "executing internal tool=%s id=%s", toolName, id)
			callParams := buildCallParams(ctx.Vars, step.WithParams)
			pref, _ := callParams["preference"].(string)
			if pref != "" && v.stateManager != nil && v.stateManager.Vector() != nil && v.getLLMClient() != nil {
				emb, err := v.getLLMClient().Embed(context.Background(), pref)
				if err == nil {
					v.stateManager.Vector().AddMemory(context.Background(), state.Memory{
						ID:        id + "_pref",
						SessionID: sessionID,
						Text:      "Preference: " + pref,
						Embedding: emb,
						Timestamp: time.Now(),
					})
				}
			}
			stepResults[toolName] = map[string]any{"status": "saved"}
			safeOutput := auditSafeToolResult(stepResults[toolName].(map[string]any))
			if err := v.logLifecycleAudit(execState, "tool.completed", "success", map[string]any{"step": step.ID, "tool": toolName, "result": safeOutput, "output_digest": audit.Digest(safeOutput)}); err != nil {
				v.failAuditExecution(execState, id, err)
				return
			}
			if v.eventBus != nil {
				v.eventBus.PublishEvent(id, "Verifier", "tool "+toolName, "ok", "")
			}
			continue
		}

		if toolName == "system.recall" {
			logx.Info("Verifier", "executing internal tool=%s id=%s", toolName, id)
			// The Planner persists the execution (with its user) before
			// dispatching, so the state loaded at the top carries the UserID.
			recallUserID := ""
			if execState != nil {
				recallUserID = execState.UserID
			}
			stepResults[toolName] = buildRecallResult(context.Background(), v.stateManager, recallUserID, sessionID, id)
			if v.eventBus != nil {
				v.eventBus.PublishEvent(id, "Verifier", "tool "+toolName, "ok", "")
			}
			continue
		}

		if strings.HasPrefix(toolName, "system.memory.") {
			out, err := v.executeMemoryTool(taskCtx, toolName, sessionID, buildCallParams(ctx.Vars, step.WithParams))
			if err != nil {
				storeResult(id, Result{Status: "error", Err: err.Error()})
				if v.eventBus != nil {
					v.eventBus.PublishEvent(id, "Verifier", "tool "+toolName, "error", err.Error())
				}
				return
			}
			stepResults[toolName] = out
			if err := v.logLifecycleAudit(execState, "tool.completed", "success", map[string]any{"step": step.ID, "tool": toolName, "result": auditSafeToolResult(out), "output_digest": audit.Digest(auditSafeToolResult(out))}); err != nil {
				v.failAuditExecution(execState, id, err)
				return
			}
			if v.eventBus != nil {
				v.eventBus.PublishEvent(id, "Verifier", "tool "+toolName, "ok", "")
			}
			continue
		}

		t, ok := v.cfg.Tools[toolName]
		if !ok {
			storeResult(id, Result{
				Status: "error",
				Err:    fmt.Sprintf("tool %s not found", toolName),
			})
			return
		}

		logx.Info("Verifier", "executing tool=%s id=%s", toolName, id)

		callParams := buildCallParams(ctx.Vars, step.WithParams)
		if callbackIndex > index && stepUsesCallbackCredentials(step) {
			callbackToken, _ := ctx.Vars["callback_token"].(string)
			prepared := &state.ExecutionState{
				ID: id, SessionID: sessionID, Intent: intentType, Pipeline: pipe.Name,
				CompletedSteps: linearStepIDs(pipe.Steps[:index+1]),
				Params:         convertVarsToStringMap(ctx.Vars), StepResults: stepResults,
				Gate: "await_callback", CallbackToken: callbackToken,
				AwaitStep: callbackStepID, AwaitDeadline: callbackDeadline, Status: "submitting_callback",
			}
			if execState != nil {
				prepared.UserID = execState.UserID
				prepared.CreatedAt = execState.CreatedAt
				copyExecutionManifest(prepared, execState)
			}
			if err := v.stateManager.Save(context.Background(), prepared); err != nil {
				storeResult(id, Result{Status: "error", Err: "cannot persist callback wait before submission: " + err.Error()})
				return
			}
			execState = prepared
		}
		if toolName == "workspace.generate_code" {
			pipelineLLM := v.getLLMClientForPipeline(pipe)
			if pipelineLLM == nil {
				storeResult(id, Result{Status: "error", Err: "LLM client unavailable"})
				return
			}
			prompt := fmt.Sprintf("Generate production-ready %s code for file %s. User request: %s. Return only the complete source code, without markdown fences.", callParams["language"], callParams["path"], callParams["request"])
			code, err := pipelineLLM.Chat(taskCtx, prompt)
			if err != nil {
				storeResult(id, Result{Status: "error", Err: err.Error()})
				return
			}
			code = strings.TrimSpace(code)
			// If the model returned a fenced block, keep only the first block and
			// discard any explanation before or after it.
			if strings.HasPrefix(code, "```") {
				if nl := strings.IndexByte(code, '\n'); nl >= 0 {
					code = code[nl+1:]
				}
				if end := strings.Index(code, "\n```"); end >= 0 {
					code = code[:end]
				}
			} else if end := strings.Index(code, "\n```"); end >= 0 {
				// Some models omit the opening fence but still emit a closing one.
				code = code[:end]
			}
			code = strings.TrimSpace(code)
			// Remove common explanatory prose accidentally appended after source.
			lines := strings.Split(code, "\n")
			for i, line := range lines {
				trimmed := strings.TrimSpace(line)
				if i > 0 && (strings.HasPrefix(trimmed, "Please note:") || strings.HasPrefix(trimmed, "Please note that") || strings.HasPrefix(trimmed, "Explanation:") || strings.HasPrefix(trimmed, "Here is an explanation") || strings.HasPrefix(trimmed, "This code ") || strings.HasPrefix(trimmed, "This implementation ") || strings.HasPrefix(trimmed, "The code ")) {
					code = strings.TrimSpace(strings.Join(lines[:i], "\n"))
					break
				}
			}
			out := map[string]any{"executed": true, "language": callParams["language"], "path": callParams["path"], "content": code}
			stepResults[toolName] = out
			safeOutput := auditSafeToolResult(out)
			if err := v.logLifecycleAudit(execState, "tool.completed", "success", map[string]any{"step": step.ID, "tool": toolName, "result": safeOutput, "output_digest": audit.Digest(safeOutput)}); err != nil {
				v.failAuditExecution(execState, id, err)
				return
			}
			for k, val := range out {
				ctx.Vars[toolName+"."+k] = val
			}
			v.eventBus.PublishEvent(id, "Verifier", "tool "+toolName, "ok", "")
			continue
		}

		timer := logx.Start(id, "Verifier", "tool_"+t.Name)
		out, err := executeToolWithResiliency(taskCtx, t, convertVarsToStringMap(callParams), step)
		timer.End()

		if err != nil {
			logx.Error("Verifier", "tool %s failed: %v", toolName, err)
			v.logLifecycleAudit(execState, "tool.failed", "error", map[string]any{"step": step.ID, "tool": toolName, "error": err.Error()})
			storeResult(id, Result{
				Status: "error",
				Err:    fmt.Sprintf("tool error: %v", err),
			})
			clearPreparedCallbackWait(v, execState, id, err)
			return
		}

		// Inject executed flag to satisfy Analyst prompt rules
		if _, exists := out["executed"]; !exists {
			out["executed"] = true
		}

		stepResults[toolName] = out
		if err := v.logLifecycleAudit(execState, "tool.completed", "success", map[string]any{"step": step.ID, "tool": toolName, "result": auditSafeToolResult(out), "output_digest": audit.Digest(auditSafeToolResult(out))}); err != nil {
			v.failAuditExecution(execState, id, err)
			return
		}
		v.eventBus.PublishEvent(id, "Verifier", "tool "+toolName, "ok", "")
		for k, val := range out {
			ctx.Vars[toolName+"."+k] = val
		}
	}

	stepResults["_pipeline"] = map[string]any{
		"pipeline":         pipe.Name,
		"pipeline_version": executionPipelineVersion(execState),
		"intent":           intentType,
		"status":           "success",
		"steps":            len(pipe.Steps),
		"endedAt":          time.Now().Format(time.RFC3339),
	}

	logx.Debug("Verifier", "[%s] Final Vars: %#v", id, ctx.Vars)
	logx.Debug("Verifier", "[%s] ToolOutputs: %#v", id, stepResults)

	if pipe.SkipSummary {
		storeResult(id, Result{Status: "completed", Data: map[string]any{"raw": cloneMap(stepResults)}})
		return
	}
	v.bus.Send("analyst", bus.Message{
		Type: "summarize",
		Payload: map[string]any{
			"id":         id,
			"session_id": sessionID,
			"intent":     intentType,
			"pipeline":   pipe.Name,
			"llm_model":  pipe.LLMModel,
			"rawResult":  cloneMap(stepResults),
			"language":   language,
		},
	})
}

func stepUsesCallbackCredentials(step config.PipelineStep) bool {
	for _, value := range step.WithParams {
		if strings.Contains(value, "callback_token") || strings.Contains(value, "callback_url") || strings.Contains(value, "callbackToken") || strings.Contains(value, "callbackUrl") {
			return true
		}
	}
	return false
}

func linearStepIDs(steps []config.PipelineStep) []string {
	ids := make([]string, 0, len(steps))
	for i, step := range steps {
		id := step.ID
		if id == "" {
			id = step.Tool
		}
		if id == "" {
			id = fmt.Sprintf("step_%d", i)
		}
		ids = append(ids, id)
	}
	return ids
}

func clearPreparedCallbackWait(v *Verifier, st *state.ExecutionState, id string, cause error) {
	if st == nil || st.Gate != "await_callback" || st.CallbackToken == "" {
		return
	}
	st.Gate = ""
	st.CallbackToken = ""
	st.AwaitDeadline = nil
	st.Status = "error"
	st.Error = "callback submission failed: " + cause.Error()
	_ = v.stateManager.Save(context.Background(), st)
}

// toolTypeLabel keeps the step metric's tool_type to a fixed set: a configured type outside
// it would make a new series per name.
func toolTypeLabel(toolType string) string {
	switch t := strings.ToLower(strings.TrimSpace(toolType)); t {
	case "http", "grpc", "mcp", "a2a", "cli":
		return t
	case "":
		return "local"
	default:
		return "other"
	}
}

func executeToolWithResiliency(ctx context.Context, t config.Tool, params map[string]string, step config.PipelineStep) (map[string]any, error) {
	retries := step.Retries
	if retries < 0 {
		retries = 0
	}
	backoffMs := step.BackoffMs
	if backoffMs <= 0 {
		backoffMs = 1000 // default 1s
	}

	ctx, span := telemetry.Tracer().Start(ctx, "aos.step", trace.WithAttributes(
		attribute.String("aos.tool.name", t.Name), attribute.String("aos.tool.type", t.Type)))
	defer span.End()
	started := time.Now()
	outcome := "failure"
	defer func() {
		metrics.StepDuration.Observe(map[string]string{"tool_type": toolTypeLabel(t.Type), "outcome": outcome}, time.Since(started).Seconds())
	}()

	var lastErr error
	var out map[string]any

	logx.Debug("Verifier", "tool=%s retries=%d backoff_ms=%d timeout_ms=%d", step.Tool, retries, backoffMs, step.TimeoutMs)

	for i := 0; i <= retries; i++ {
		var tryCtx context.Context
		var cancel context.CancelFunc
		if step.TimeoutMs > 0 {
			tryCtx, cancel = context.WithTimeout(ctx, time.Duration(step.TimeoutMs)*time.Millisecond)
		} else {
			tryCtx, cancel = context.WithCancel(ctx)
		}

		if t.Name == "system.rag_query" {
			// For generic rag_query, just return the inputs so the analyst can summarize it
			out = map[string]any{
				"executed": true,
				"query":    params["query"],
				"context":  params["context"],
			}
			lastErr = nil
		} else {
			out, lastErr = tools.ExecuteToolCtx(tryCtx, t, params)
		}
		cancel()

		if lastErr == nil {
			outcome = "success"
			return out, nil
		}
		span.AddEvent("attempt failed", trace.WithAttributes(attribute.Int("aos.attempt", i+1)))

		logx.Warn("Verifier", "tool %s failed (attempt %d/%d): %v", t.Name, i+1, retries+1, lastErr)

		if i < retries {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(backoffMs) * time.Millisecond):
				// Exponential backoff logic
				backoffMs *= 2
			}
		}
	}

	span.SetStatus(codes.Error, "tool failed")
	return nil, lastErr
}
