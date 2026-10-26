package agent

import (
	"context"
	"fmt"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"strings"
	"sync"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/audit"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/engine"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/governance"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/llm"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/logx"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/payload"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/tools"
)

type Verifier struct {
	bus          *bus.Bus
	cfg          *config.Config
	llmClient    llm.LLMClient
	llmMutex     sync.RWMutex
	inbox        chan bus.Message
	eventBus     *bus.Bus
	stateManager *state.StateManager
	auditLogger  audit.Logger
}

func (v *Verifier) SetLLMClient(c llm.LLMClient) {
	v.llmMutex.Lock()
	defer v.llmMutex.Unlock()
	v.llmClient = c
}

func (v *Verifier) getLLMClient() llm.LLMClient {
	v.llmMutex.RLock()
	defer v.llmMutex.RUnlock()
	return v.llmClient
}

func (v *Verifier) getLLMClientForPipeline(pipe config.Pipeline) llm.LLMClient {
	client := v.getLLMClient()
	if pipe.LLMModel == "" || client == nil {
		return client
	}
	routable, ok := client.(llm.ModelRoutableClient)
	if !ok {
		logx.Warn("Verifier", "pipeline=%s declares llm_model=%s but the configured LLM provider cannot route models; using its default", pipe.Name, pipe.LLMModel)
		return client
	}
	return routable.WithModel(pipe.LLMModel)
}

func NewVerifier(b *bus.Bus, cfg *config.Config, llmClient llm.LLMClient, eventBus *bus.Bus, smOpt ...*state.StateManager) *Verifier {
	var sm *state.StateManager
	if len(smOpt) > 0 && smOpt[0] != nil {
		sm = smOpt[0]
	} else {
		// default to in-memory store
		mem := state.NewMemoryStore()
		sm = state.NewStateManager(mem)
	}
	return &Verifier{
		bus:          b,
		cfg:          cfg,
		llmClient:    llmClient,
		inbox:        make(chan bus.Message, 16),
		eventBus:     eventBus,
		stateManager: sm,
		auditLogger:  audit.StdLogger{},
	}
}

func (v *Verifier) SetAuditLogger(logger audit.Logger) {
	if logger != nil {
		v.auditLogger = logger
	}
}

func (v *Verifier) Inbox() chan bus.Message {
	return v.inbox
}

func (v *Verifier) Start(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	defer func() {
		if r := recover(); r != nil {
			logx.Error("Verifier", "panic recovered in Start: %v", r)
		}
	}()
	for {
		select {
		case msg := <-v.inbox:
			func() {
				defer func() {
					if r := recover(); r != nil {
						logx.Error("Verifier", "panic recovered in dispatch: %v", r)
					}
				}()
				v.dispatch(msg)
			}()
		case <-ticker.C:
			v.recoverPendingCallbacks(ctx)
			v.expireAwaitingCallbacks(ctx)

		case <-ctx.Done():
			return nil
		}
	}
}

func (v *Verifier) recoverPendingCallbacks(ctx context.Context) {
	if v.stateManager == nil {
		return
	}
	states, err := v.stateManager.ListExecutionStates(ctx)
	if err != nil {
		logx.Error("Verifier", "cannot scan persisted callbacks: %v", err)
		return
	}
	for _, st := range states {
		if st.Gate == "await_callback" && st.CallbackToken != "" && st.CallbackData != nil && (st.AwaitDeadline == nil || (st.CallbackAt != nil && !st.CallbackAt.After(*st.AwaitDeadline))) {
			v.handleCallback(bus.Message{Type: "callback_received", Payload: map[string]any{"token": st.CallbackToken, "data": st.CallbackData}})
		}
	}
}

// expireAwaitingCallbacks enforces persisted callback deadlines. A scan after
// restart also expires waits whose deadline elapsed while AOS was offline.
func (v *Verifier) expireAwaitingCallbacks(ctx context.Context) {
	if v.stateManager == nil {
		return
	}
	states, err := v.stateManager.ListExecutionStates(ctx)
	if err != nil {
		logx.Error("Verifier", "cannot scan callback deadlines: %v", err)
		return
	}
	now := time.Now().UTC()
	for _, st := range states {
		if st.Gate != "await_callback" || st.AwaitDeadline == nil || st.AwaitDeadline.After(now) {
			continue
		}
		if st.CallbackData != nil && st.CallbackAt != nil && !st.CallbackAt.After(*st.AwaitDeadline) {
			continue
		}
		deadline := st.AwaitDeadline.UTC()
		st.Gate = ""
		st.CallbackToken = ""
		st.Status = "timed_out"
		st.Error = "callback deadline exceeded"
		st.AwaitDeadline = nil
		if err := v.stateManager.Save(ctx, st); err != nil {
			logx.Error("Verifier", "cannot persist callback timeout for task %s: %v", st.ID, err)
			continue
		}
		storeResult(st.ID, Result{Status: "error", Err: st.Error})
		if err := v.logLifecycleAudit(st, "callback.timed_out", "error", map[string]any{"deadline": deadline}); err != nil {
			st.Status = "error"
			st.Error = "audit journal unavailable: " + err.Error()
			_ = v.stateManager.Save(ctx, st)
			storeResult(st.ID, Result{Status: "error", Err: st.Error})
		}
		if v.eventBus != nil {
			v.eventBus.PublishEvent(st.ID, "Verifier", "callback_timeout", "Callback deadline exceeded", "")
		}
	}
}

func (v *Verifier) dispatch(msg bus.Message) {
	//message from the planner agent
	switch msg.Type {
	case "run_pipeline":
		v.handleRunPipeline(msg)
	case "human_decision":
		v.handleHumanDecision(msg)
	case "callback_received":
		v.handleCallback(msg)
	default:
		logx.Warn("Verifier", "unknown message: %T", msg)
	}
}

func (v *Verifier) handleCallback(msg bus.Message) {
	token, ok := payload.GetString(msg.Payload, "token")
	if !ok || token == "" {
		return
	}
	states, err := v.stateManager.ListExecutionStates(context.Background())
	if err != nil {
		return
	}
	for _, st := range states {
		if st.CallbackToken != token || st.Gate != "await_callback" {
			continue
		}
		if st.AwaitDeadline != nil && !st.AwaitDeadline.After(time.Now().UTC()) && (st.CallbackData == nil || st.CallbackAt == nil || st.CallbackAt.After(*st.AwaitDeadline)) {
			continue
		}
		if st.StepResults == nil {
			st.StepResults = map[string]any{}
		}
		callbackData := msg.Payload["data"]
		if st.CallbackData != nil {
			callbackData = st.CallbackData
		}
		st.StepResults[st.AwaitStep] = callbackData
		st.CompletedSteps = append(st.CompletedSteps, st.AwaitStep)
		st.Gate = ""
		st.CallbackToken = ""
		st.CallbackData = nil
		st.CallbackAt = nil
		st.AwaitDeadline = nil
		st.Status = "running"
		if err := v.stateManager.Save(context.Background(), st); err != nil {
			return
		}
		if err := v.logLifecycleAudit(st, "callback.received", "success", map[string]any{"step": st.AwaitStep, "data": auditSafeCallback(msg.Payload["data"])}); err != nil {
			st.Status = "error"
			st.Error = "audit journal unavailable: " + err.Error()
			_ = v.stateManager.Save(context.Background(), st)
			storeResult(st.ID, Result{Status: "error", Err: st.Error})
			return
		}
		v.resumeFromState(st)
		return
	}
}

func auditSafeCallback(raw any) map[string]any {
	data, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	return auditSafeToolResult(data)
}

func (v *Verifier) logLifecycleAudit(st *state.ExecutionState, phase, result string, details map[string]any) error {
	if v.auditLogger == nil || st == nil {
		return nil
	}
	traceparent := ""
	if taskCtx, ok := GetTaskContext(st.ID); ok {
		traceparent = telemetry.Traceparent(taskCtx)
	}
	return audit.LogChecked(v.auditLogger, audit.Event{
		Timestamp: time.Now().UTC(), UserID: st.UserID, Intent: st.Intent,
		Pipeline: st.Pipeline, Result: result, TraceID: st.ID, Phase: phase,
		Details: details, Traceparent: traceparent,
	})
}

func (v *Verifier) ensurePipelineManifest(id, sessionID, intent string, pipe config.Pipeline, params map[string]any, existing *state.ExecutionState) (*state.ExecutionState, error) {
	snapshot, version, err := audit.BuildPipelineSnapshot(pipe, v.cfg)
	if err != nil {
		return nil, fmt.Errorf("build pipeline snapshot: %w", err)
	}
	st := existing
	if st == nil {
		st = &state.ExecutionState{ID: id, SessionID: sessionID, Intent: intent, Pipeline: pipe.Name}
	}
	if st.PipelineVersion != "" && st.PipelineVersion != version {
		return nil, fmt.Errorf("pipeline definition changed during execution (recorded %s, current %s); refusing non-reproducible resume", st.PipelineVersion, version)
	}
	if st.PipelineVersion != "" {
		return st, nil
	}
	cleanInputs := audit.SanitizeMap(params)
	st.Pipeline = pipe.Name
	st.PipelineVersion = version
	st.PipelineSnapshot = snapshot
	st.InputDigest = audit.Digest(cleanInputs)
	if st.Params == nil {
		st.Params = convertVarsToStringMap(params)
	}
	if err := v.stateManager.Save(context.Background(), st); err != nil {
		return nil, fmt.Errorf("persist pipeline manifest: %w", err)
	}
	if err := v.logLifecycleAudit(st, "pipeline.started", "success", map[string]any{
		"pipeline_version":  version,
		"pipeline_snapshot": snapshot,
		"input_digest":      st.InputDigest,
		"inputs":            cleanInputs,
	}); err != nil {
		return nil, err
	}
	return st, nil
}

func copyExecutionManifest(dst, src *state.ExecutionState) {
	if dst == nil || src == nil {
		return
	}
	dst.PipelineVersion = src.PipelineVersion
	dst.PipelineSnapshot = src.PipelineSnapshot
	dst.InputDigest = src.InputDigest
}

func executionPipelineVersion(st *state.ExecutionState) string {
	if st == nil {
		return ""
	}
	return st.PipelineVersion
}

func (v *Verifier) failAuditExecution(st *state.ExecutionState, id string, err error) {
	if err == nil {
		return
	}
	if st == nil && v.stateManager != nil {
		st, _ = v.stateManager.Load(context.Background(), id)
	}
	message := "audit journal unavailable: " + err.Error()
	if st != nil {
		st.Status = "error"
		st.Error = message
		st.Gate = ""
		st.CallbackToken = ""
		st.AwaitDeadline = nil
		_ = v.stateManager.Save(context.Background(), st)
	}
	storeResult(id, Result{Status: "error", Err: message})
}

func (v *Verifier) handleRunPipeline(msg bus.Message) {
	id, ok := payload.GetString(msg.Payload, "id")
	if !ok {
		logx.Error("Verifier", "invalid payload: missing id")
		return
	}
	intentType, ok := payload.GetString(msg.Payload, "intent")
	if !ok {
		logx.Error("Verifier", "invalid payload: missing intent")
		return
	}
	language, ok := payload.GetString(msg.Payload, "language")
	if !ok || language == "" {
		language = "es"
	}

	pipeAny := msg.Payload["pipeline"]
	paramsAny := msg.Payload["params"]

	pipe, ok := pipeAny.(config.Pipeline)
	if !ok {
		storeResult(id, Result{
			Status: "error",
			Err:    "invalid pipeline",
		})
		return
	}

	// ============================================================
	// 1) Extract initial params (Planner → Verifier)
	// ============================================================
	baseParams := make(map[string]interface{})
	if paramsAny != nil {
		switch t := paramsAny.(type) {
		case map[string]string:
			for k, v := range t {
				baseParams[k] = v
			}
		case map[string]any:
			for k, v := range t {
				baseParams[k] = v
			}
		}
	}

	logx.Info("Verifier", "executing pipeline=%s id=%s intent=%s params=%v",
		pipe.Name, id, intentType, logx.Keys(baseParams))

	// ============================================================
	// 2) Create PipelineContext
	// ============================================================
	ctx := engine.NewPipelineContext(baseParams)
	// ctx.Vars now contains all user + planner + intent params
	// ctx.ToolOutputs will be filled automatically

	stepResults := make(map[string]any)
	sessionID, _ := payload.GetString(msg.Payload, "session_id")
	manifestState, loadErr := v.stateManager.Load(context.Background(), id)
	if loadErr != nil {
		storeResult(id, Result{Status: "error", Err: "cannot load execution state: " + loadErr.Error()})
		return
	}
	// A manifest already on record means this run started before (a resume): it was reported then.
	freshStart := manifestState == nil || manifestState.PipelineVersion == ""
	if _, err := v.ensurePipelineManifest(id, sessionID, intentType, pipe, baseParams, manifestState); err != nil {
		v.failAuditExecution(nil, id, err)
		return
	}
	taskCtx, _ := GetTaskContext(id)
	if taskCtx == nil {
		taskCtx = NewTaskContext(context.Background(), id, 0)
	}
	taskCtx, span := telemetry.Tracer().Start(taskCtx, "aos.run", trace.WithAttributes(
		attribute.String(telemetry.CorrelationKey, id), attribute.String("aos.pipeline", pipe.Name)))
	defer span.End()

	if freshStart && strings.TrimSpace(pipe.AiSystem) != "" && governance.ReportsEnabled() {
		decisionID, _ := payload.GetString(msg.Payload, "decision_id")
		governance.ReportExecutionAsync(taskCtx, governance.ExecutionReport{
			Runtime:     "aos",
			ExecutionID: id,
			DecisionID:  decisionID,
			AiSystemID:  pipe.AiSystem,
			Pipeline:    pipe.Name,
			StartedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		})
	}

	switch pipe.Mode {
	case "dag":
		executeDAG(v, pipe, id, sessionID, intentType, language, taskCtx, stepResults, baseParams)
	case "react":
		executeReAct(v, pipe, id, sessionID, intentType, language, taskCtx, stepResults, baseParams)
	case "linear":
		fallthrough
	default:
		executeLinear(v, pipe, id, sessionID, intentType, language, taskCtx, stepResults, baseParams)
	}

	logx.Debug("Verifier", "[%s] Final Vars: %#v", id, ctx.Vars)
	logx.Debug("Verifier", "[%s] ToolOutputs: %#v", id, ctx.ToolOutputs)
}

func (v *Verifier) resumeFromState(st *state.ExecutionState) {
	id := st.ID
	intentType := st.Intent
	pipe, ok := v.cfg.Pipelines[st.Pipeline]
	if !ok {
		v.storeError(id, "pipeline not found on resume")
		return
	}
	params := make(map[string]any, len(st.Params))
	for key, value := range st.Params {
		params[key] = value
	}
	if _, err := v.ensurePipelineManifest(st.ID, st.SessionID, st.Intent, pipe, params, st); err != nil {
		v.failAuditExecution(st, st.ID, err)
		return
	}

	logx.Info("Verifier", "resuming pipeline=%s id=%s from completed_steps=%d", pipe.Name, id, len(st.CompletedSteps))

	stepResults := st.StepResults
	if stepResults == nil {
		stepResults = make(map[string]any)
	}

	baseParams := make(map[string]any)
	for k, val := range st.Params {
		baseParams[k] = val
	}

	taskCtx, _ := GetTaskContext(id)
	if taskCtx == nil || taskCtx.Err() != nil {
		taskCtx = context.Background()
		_ = NewTaskContext(taskCtx, id, 0)
		logx.Info("Verifier", "[%s] regenerated fresh task context (resume)", id)
	}
	// A resume (a human decision, a callback) happens later: a new trace for the same task.
	taskCtx, span := telemetry.Tracer().Start(taskCtx, "aos.run.resume", trace.WithNewRoot(), trace.WithAttributes(
		attribute.String(telemetry.CorrelationKey, id), attribute.String("aos.pipeline", pipe.Name)))
	defer span.End()

	switch pipe.Mode {
	case "dag":
		executeDAG(v, pipe, id, st.SessionID, intentType, "es", taskCtx, stepResults, baseParams)
	case "react":
		executeReAct(v, pipe, id, st.SessionID, intentType, "es", taskCtx, stepResults, baseParams)
	case "linear":
		fallthrough
	default:
		executeLinear(v, pipe, id, st.SessionID, intentType, "es", taskCtx, stepResults, baseParams)
	}
}

func (v *Verifier) handleHumanDecision(msg bus.Message) {
	id, ok := payload.GetString(msg.Payload, "id")
	if !ok {
		logx.Error("Verifier", "invalid human_decision payload: missing id")
		return
	}
	decision, ok := payload.GetString(msg.Payload, "decision")
	if !ok {
		logx.Error("Verifier", "invalid human_decision payload: missing decision")
		return
	}

	st, err := v.stateManager.Load(context.Background(), id)
	if err != nil || st == nil {
		v.storeError(id, "no execution state found")
		return
	}
	v.logLifecycleAudit(st, "human.decision", decision, map[string]any{"gate": st.Gate})

	if decision != "approved" {
		_ = v.stateManager.Delete(context.Background(), id)
		v.storeError(id, "human rejected review at gate: "+st.Gate)
		return
	}

	// Do NOT delete the state here! executeDAG/executeLinear need to load it from the store
	// to properly restore the pipeline's progress instead of starting from scratch!
	v.resumeFromState(st)
}

func (v *Verifier) storeError(id, errMsg string) {
	logx.Error("Verifier", "[%s] %s", id, errMsg)

	if v.stateManager != nil {
		_ = v.stateManager.Delete(context.Background(), id)
	}

	storeResult(id, Result{
		Status: "error",
		Err:    errMsg,
	})

	if v.eventBus != nil {
		v.eventBus.PublishEvent(id, "Verifier", "error", errMsg, "")
	}
}

func buildCallParams(vars map[string]interface{}, defaults map[string]string) map[string]interface{} {
	out := make(map[string]interface{})

	// copy existing context vars first
	for k, v := range vars {
		out[k] = v
	}

	// add defaults only if missing, but render them first
	stringVars := convertVarsToStringMap(vars)
	for k, v := range defaults {
		if _, exists := out[k]; !exists {
			rendered, err := tools.RenderTemplateString(v, stringVars)
			if err == nil {
				out[k] = rendered
			} else {
				out[k] = v // fallback
			}
		}
	}
	return out
}

func flattenVars(prefix string, v interface{}, out map[string]string) {
	switch vv := v.(type) {
	case string:
		out[prefix] = vv
	case map[string]interface{}:
		for k, val := range vv {
			newKey := k
			if prefix != "" {
				newKey = prefix + "." + k
			}
			flattenVars(newKey, val, out)
		}
	default:
		out[prefix] = fmt.Sprintf("%v", vv)
	}
}

func convertVarsToStringMap(vars map[string]interface{}) map[string]string {
	out := make(map[string]string)
	for k, v := range vars {
		flattenVars(k, v, out)
	}
	return out
}

func cloneMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func stringMapToAny(src map[string]string) map[string]any {
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func auditSafeToolResult(out map[string]any) map[string]any {
	allowed := map[string]struct{}{
		"ok": {}, "status": {}, "statusCode": {}, "operationId": {}, "txHash": {},
		"anchorTxHash": {}, "blockNumber": {}, "chainId": {}, "assetId": {},
		"destination": {}, "amount": {}, "auditHash": {}, "path": {}, "language": {},
	}
	safe := make(map[string]any)
	for key, value := range out {
		if _, ok := allowed[key]; ok {
			safe[key] = value
		}
		if key == "content" {
			if content, ok := value.(string); ok {
				safe["content_digest"] = audit.Digest(content)
			}
		}
	}
	return safe
}
