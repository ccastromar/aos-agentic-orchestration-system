package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/auth"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/logx"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/payload"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
)

type APIAgent struct {
	bus      *bus.Bus
	cfg      *config.Config
	state    *state.StateManager
	inbox    chan bus.Message
	eventBus *bus.Bus // <-- nuevo
	// minimal auth and rate limiting
	apiKey        string
	authChain     *auth.Chain
	allowDevLogin bool
	// naive fixed-window rate limiter per client key
	rl struct {
		Window  time.Duration
		Limit   int
		mu      chan struct{} // lightweight mutex using channel
		buckets map[string]*rateBucket
	}
}

// NewAPIAgent creates the API agent. Backward-compatible signature:
// - NewAPIAgent(b, env, uiStore, cfg, stateManager)
// - NewAPIAgent(b, uiStore)
func NewAPIAgent(b *bus.Bus, args ...any) *APIAgent {
	var (
		env *config.EnvVars
		uiS *bus.Bus
		cfg *config.Config
		sm  *state.StateManager
	)
	for _, arg := range args {
		switch v := arg.(type) {
		case *config.EnvVars:
			env = v
		case *bus.Bus:
			uiS = v
		case *config.Config:
			cfg = v
		case *state.StateManager:
			sm = v
		}
	}
	if uiS == nil {
		uiS = bus.New()
	}

	a := &APIAgent{
		bus:           b,
		cfg:           cfg,
		state:         sm,
		inbox:         make(chan bus.Message, 16),
		eventBus:      uiS,
		apiKey:        strings.TrimSpace(os.Getenv("API_KEY")),
		allowDevLogin: env == nil || auth.IsDevelopmentEnvironment(env.AppEnv),
	}

	// Initialize auth chain
	a.authChain = &auth.Chain{Authenticators: []auth.Authenticator{}}

	// Only configure authenticators if env is provided
	if env != nil {
		jwtCfg := auth.JWTConfig{
			Issuer:   env.JWTIssuer,
			Audience: env.JWTAudience,
			JWKURL:   env.JWKURL,
		}
		apiKeyCfg := auth.APIKeyConfig{
			ResolveURL: env.IAMURL,
			Timeout:    1 * time.Second,
		}
		if jwtCfg.Issuer != "" && jwtCfg.JWKURL != "" {
			a.authChain.Authenticators = append(a.authChain.Authenticators, auth.NewJWTAuthenticator(jwtCfg))
		}
		if apiKeyCfg.ResolveURL != "" && apiKeyCfg.ResolveURL != "disabled" {
			a.authChain.Authenticators = append(a.authChain.Authenticators, auth.NewAPIKeyAuthenticator(apiKeyCfg))
		}
	}

	// Rate limiter init...
	a.rl.Window = 1 * time.Minute
	a.rl.Limit = 60
	a.rl.mu = make(chan struct{}, 1)
	a.rl.buckets = make(map[string]*rateBucket)

	return a
}

// Max request size for POST /ask to protect the server (1MB)
const maxAskBodyBytes int64 = 1 << 20

// handleLogin issues a development JWT. Credentials are NOT validated yet:
// this is a stub to exercise user-scoped flows until authentication is
// delegated to Keycloak (AOS_JWK_URL / AOS_JWT_ISSUER). The optional roles
// field is honored for local RBAC testing only.
func (a *APIAgent) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !a.allowDevLogin {
		http.Error(w, "development login is disabled", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Username string   `json:"username"`
		Password string   `json:"password"` // ignored by the dev stub
		Roles    []string `json:"roles,omitempty"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" {
		http.Error(w, "username is required", http.StatusBadRequest)
		return
	}

	const ttl = 8 * time.Hour
	token, err := auth.IssueToken(req.Username, req.Roles, ttl)
	if err != nil {
		http.Error(w, "cannot issue token", http.StatusInternalServerError)
		return
	}

	roles := req.Roles
	if len(roles) == 0 {
		roles = []string{"user"}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"token":      token,
		"token_type": "Bearer",
		"user_id":    req.Username,
		"roles":      roles,
		"expires_in": int(ttl.Seconds()),
	})
}

// rateBucket tracks hits in a fixed window
type rateBucket struct {
	start time.Time
	hits  int
}

// acquireRL returns error if rate limit exceeded
func (a *APIAgent) acquireRL(key string) error {
	if key == "" {
		key = "anon"
	}
	// lock
	a.rl.mu <- struct{}{}
	defer func() { <-a.rl.mu }()

	b, ok := a.rl.buckets[key]
	now := time.Now()
	if !ok || now.Sub(b.start) >= a.rl.Window {
		a.rl.buckets[key] = &rateBucket{start: now, hits: 1}
		return nil
	}
	if b.hits >= a.rl.Limit {
		return errors.New("rate limit exceeded")
	}
	b.hits++
	return nil
}

// getClientKey picks an identifier for auth/rate limit: API key if present, else IP
func getClientKey(r *http.Request) string {
	// prefer provided API key to segregate limits per token
	if k := r.Header.Get("X-API-Key"); k != "" {
		return "key:" + k
	}
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return "key:" + strings.TrimSpace(auth[7:])
	}
	// fallback to remote addr (strip port)
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return "ip:" + host
}

// checkAuth enforces API key when configured via API_KEY env var
func (a *APIAgent) checkAuth(r *http.Request) bool {
	if a.apiKey == "" {
		return true // auth disabled
	}
	if k := r.Header.Get("X-API-Key"); k != "" && k == a.apiKey {
		return true
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		token := strings.TrimSpace(auth[7:])
		return token == a.apiKey
	}
	return false
}

var idRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func (a *APIAgent) Inbox() chan bus.Message {
	return a.inbox
}

func (a *APIAgent) Start(ctx context.Context) error {
	defer func() {
		if r := recover(); r != nil {
			logx.Error("API", "panic recovered in Start: %v", r)
		}
	}()
	for {
		select {
		case msg := <-a.inbox:
			func() {
				defer func() {
					if r := recover(); r != nil {
						logx.Error("API", "panic recovered in dispatch: %v", r)
					}
				}()
				a.dispatch(msg)
			}()

		case <-ctx.Done():
			return nil
		}
	}
}

func (a *APIAgent) dispatch(msg bus.Message) {
	switch msg.Type {
	case "await_human":
		id, ok := payload.GetString(msg.Payload, "id")
		if !ok {
			logx.Error("Api", "invalid payload: missing id")
			return
		}
		gate, ok := payload.GetString(msg.Payload, "gate")
		if !ok {
			logx.Error("Api", "invalid payload: missing gate")
			return
		}
		logx.Info("API", "task %s awaiting human review [%s]", id, gate)
		// Verifier already emitted the await_human event with context vars to eventBus.
		// We do not emit it again here to avoid overwriting the context.

		// If sync request is waiting, release it
		storeResult(id, Result{
			Status: "await_human",
			Data:   map[string]any{"gate": gate},
		})
	default:
		// ignore silently
	}
}

type askRequest struct {
	Operation string         `json:"operation,omitempty"`
	Params    map[string]any `json:"params,omitempty"`
	Message   string         `json:"message"`
	Async     bool           `json:"async,omitempty"`
}

type askResponse struct {
	ID     string      `json:"id"`
	Status string      `json:"status"`
	Result interface{} `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

// RegisterHTTP adds routes for this agent's API
func (a *APIAgent) RegisterHTTP(mux *http.ServeMux) {
	mux.HandleFunc("/auth/login", a.handleLogin)    // dev login: issues a JWT (no credential validation yet)
	mux.HandleFunc("/ask", a.handleAsk)             // async NLP-like mode (message)
	mux.HandleFunc("/ask_structured", a.handleAsk2) // sync: operation + params
	mux.HandleFunc("/task", a.handleTask)           // fetch task status/result
	mux.HandleFunc("/task/approve", a.handleHumanApprove)
	mux.HandleFunc("/task/reject", a.handleHumanReject)
	mux.HandleFunc("/task/reply", a.handleClarifyReply)
	mux.HandleFunc("/intents", a.handleIntents)
	mux.HandleFunc("/api/v1/tasks", a.HandleListTasks)
	mux.HandleFunc("/api/v1/memories", a.handleMemories)
	mux.HandleFunc("/api/v1/events/stream", a.handleEventStream)
	mux.HandleFunc("/api/v1/callbacks/", a.handleCallback)
}

func (a *APIAgent) handleEventStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("task_id")
	if id == "" || !idRe.MatchString(id) {
		http.Error(w, "valid task_id query parameter required", http.StatusBadRequest)
		return
	}
	if _, ok := a.requireOwnedTask(w, r, id); !ok {
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ch := make(chan bus.Message, 16)
	a.eventBus.Subscribe("system_events", ch)
	defer a.eventBus.Unsubscribe("system_events", ch)
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			if msg.Type != "agent_event" || msg.Payload["task_id"] != id {
				continue
			}
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (a *APIAgent) requireOwnedTask(w http.ResponseWriter, r *http.Request, id string) (*state.ExecutionState, bool) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok || identity == nil || identity.UserID == "" {
		http.Error(w, "identity required", http.StatusUnauthorized)
		return nil, false
	}
	if a.state == nil {
		http.Error(w, "state store not configured", http.StatusServiceUnavailable)
		return nil, false
	}
	st, err := a.state.Load(r.Context(), id)
	if err != nil {
		http.Error(w, "state unavailable", http.StatusServiceUnavailable)
		return nil, false
	}
	if st == nil || st.UserID == "" || st.UserID != identity.UserID {
		http.Error(w, "task not found", http.StatusNotFound)
		return nil, false
	}
	return st, true
}

// handleCallback receives an asynchronous provider result and routes it to the
// verifier. The token is single-use because the verifier clears it on resume.
func (a *APIAgent) handleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimPrefix(r.URL.Path, "/api/v1/callbacks/")
	if token == "" || len(token) > 200 {
		http.Error(w, "invalid callback token", http.StatusBadRequest)
		return
	}
	if a.state == nil {
		http.Error(w, "state unavailable", http.StatusServiceUnavailable)
		return
	}
	var data map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&data); err != nil {
		http.Error(w, "invalid callback body", 400)
		return
	}
	states, err := a.state.ListExecutionStates(r.Context())
	if err != nil {
		http.Error(w, "state unavailable", 503)
		return
	}
	for _, st := range states {
		if st.CallbackToken == token && st.Gate == "await_callback" && (st.AwaitDeadline == nil || st.AwaitDeadline.After(time.Now().UTC())) {
			duplicate := st.CallbackData != nil
			if !duplicate {
				receivedAt := time.Now().UTC()
				st.CallbackData = data
				st.CallbackAt = &receivedAt
				st.Status = "callback_received"
				if err := a.state.Save(r.Context(), st); err != nil {
					http.Error(w, "could not persist callback", http.StatusServiceUnavailable)
					return
				}
			}
			callbackData := st.CallbackData
			if !duplicate && a.eventBus != nil {
				a.eventBus.PublishEvent(st.ID, "API", "callback_received", "Callback received", "")
			}
			a.bus.Send("verifier", bus.Message{Type: "callback_received", Payload: map[string]any{"token": token, "data": callbackData}})
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "accepted", "id": st.ID, "duplicate": duplicate})
			return
		}
	}
	http.Error(w, "callback not found or already consumed", http.StatusNotFound)
}

func (a *APIAgent) handleIntents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	type IntentExample struct {
		Type        string `json:"type"`
		Description string `json:"description"`
		Example     string `json:"example,omitempty"`
	}
	var examples []IntentExample

	for _, it := range a.cfg.Intents {
		ex := ""
		if len(it.Examples) > 0 {
			ex = it.Examples[0]
		}
		examples = append(examples, IntentExample{
			Type:        it.Type,
			Description: it.Description,
			Example:     ex,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(examples)
}

func (a *APIAgent) handleMemories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	identity, _ := auth.IdentityFromContext(r.Context())
	if identity == nil || identity.UserID == "" {
		http.Error(w, "identity required", http.StatusUnauthorized)
		return
	}
	sessionID := identity.UserID

	if a.state == nil || a.state.Vector() == nil {
		http.Error(w, "vector store not configured", http.StatusServiceUnavailable)
		return
	}

	vs := a.state.Vector()

	if r.Method == http.MethodGet {
		memories, err := vs.ListMemories(r.Context(), sessionID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Filter out preferences and return only facts for UI
		facts := []state.Memory{}
		for _, m := range memories {
			if m.IsPersonalFact() {
				facts = append(facts, m)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(facts)
		return
	}

	if r.Method == http.MethodDelete {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "missing id parameter", http.StatusBadRequest)
			return
		}

		err := vs.DeleteMemory(r.Context(), sessionID, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	}
}

func (a *APIAgent) handleAsk(w http.ResponseWriter, r *http.Request) {
	// Method check
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	// Extract Identity from context (populated by AuthMiddleware)
	identity, _ := auth.IdentityFromContext(r.Context())

	// Rate limit
	if err := a.acquireRL(getClientKey(r)); err != nil {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	// Enforce content type
	ct := r.Header.Get("Content-Type")
	if ct == "" || !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}
	type Req struct {
		SessionID string `json:"session_id,omitempty"`
		Lang      string `json:"lang"`
		Message   string `json:"message"`
	}

	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, maxAskBodyBytes)
	var req Req
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// If body too large, return 413; otherwise 400
		httpErr := http.StatusBadRequest
		if err != nil && err.Error() == "http: request body too large" {
			httpErr = http.StatusRequestEntityTooLarge
		}
		http.Error(w, "invalid request body", httpErr)
		return
	}

	if req.Message == "" {
		http.Error(w, "message and lang are required", http.StatusBadRequest)
		return
	}
	if req.Lang == "" {
		req.Lang = "es"
	}
	// Memory and conversation context are scoped to the authenticated user.
	if identity != nil && identity.UserID != "" {
		req.SessionID = identity.UserID
	} else {
		req.SessionID = randomID()
	}

	id := randomID()
	if a.state != nil {
		userID := ""
		if identity != nil {
			userID = identity.UserID
		}
		if err := a.state.Save(r.Context(), &state.ExecutionState{ID: id, SessionID: req.SessionID, UserID: userID, Status: "processing"}); err != nil {
			http.Error(w, "could not persist task owner", http.StatusServiceUnavailable)
			return
		}
	}

	logx.Info("Api", "new request lang=%s session=%s id=%s message='%s'", req.Lang, req.SessionID, id, req.Message)
	a.eventBus.PublishEvent(id, "Api", "request", req.Message, "")

	_ = NewTaskContext(context.Background(), id, 0)
	logx.Debug("Api", "Created NEW TaskContext for %s", id)

	// Enviar al inspector con el message correcto
	a.bus.Send("inspector", bus.Message{
		Type: "new_task",
		Payload: map[string]any{
			"id":         id,
			"session_id": req.SessionID,
			"mode":       "structured",
			"message":    req.Message,
			"lang":       req.Lang,
			"identity":   identity,
		},
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":         id,
		"session_id": req.SessionID,
		"status":     "accepted",
	})
}

// HandleListTasks returns a list of execution states (historical tasks)
func (a *APIAgent) HandleListTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	identity, _ := auth.IdentityFromContext(r.Context())
	if identity == nil || identity.UserID == "" {
		http.Error(w, "identity required", http.StatusUnauthorized)
		return
	}
	if a.state == nil {
		http.Error(w, "state store not configured", http.StatusServiceUnavailable)
		return
	}
	states, err := a.state.ListExecutionStates(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	owned := []*state.ExecutionState{}
	for _, st := range states {
		if st.UserID == identity.UserID {
			owned = append(owned, st)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(owned); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// /ask → operation + params
func (a *APIAgent) handleAsk2(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Extract Identity from context (populated by AuthMiddleware)
	identity, _ := auth.IdentityFromContext(r.Context())
	// Rate limit
	if err := a.acquireRL(getClientKey(r)); err != nil {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	// Enforce content type
	ct := r.Header.Get("Content-Type")
	if ct == "" || !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}

	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, maxAskBodyBytes)

	var req askRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Println("[API] error parsing request:", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"json is not valid"}`))
		return
	}

	if req.Operation == "" && req.Message == "" {
		http.Error(w, "operation required", 400)
		return
	}

	id := randomID()
	sessionID := randomID()
	userID := ""
	if identity != nil && identity.UserID != "" {
		sessionID = identity.UserID
		userID = identity.UserID
	}
	if a.state != nil {
		if err := a.state.Save(r.Context(), &state.ExecutionState{ID: id, SessionID: sessionID, UserID: userID, Status: "processing"}); err != nil {
			http.Error(w, "could not persist task owner", http.StatusServiceUnavailable)
			return
		}
	}

	a.bus.Send("inspector", bus.Message{
		Type: "new_task",
		Payload: map[string]any{
			"id":         id,
			"mode":       "structured",
			"operation":  req.Operation,
			"session_id": sessionID,
			"lang":       "en",
			"params":     req.Params,
			"identity":   identity,
		},
	})

	if req.Async {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(askResponse{
			ID:     id,
			Status: "processing",
		})
		return
	}

	res := waitForResult(id, 30*time.Second)

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	if res.Err != "" {
		w.WriteHeader(http.StatusInternalServerError)
	}
	enc.Encode(askResponse{
		ID:     id,
		Status: res.Status,
		Result: res.Data,
		Error:  res.Err,
	})
}

// GET /task?id=...
func (a *APIAgent) handleTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	// Auth check (optional)
	if !a.checkAuth(r) {
		w.Header().Set("WWW-Authenticate", "Bearer, X-API-Key")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// Rate limit
	if err := a.acquireRL(getClientKey(r)); err != nil {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	if !idRe.MatchString(id) {
		http.Error(w, "id is not valid", http.StatusBadRequest)
		return
	}

	// Check ownership before exposing or consuming a persisted task result.
	if a.state != nil {
		st, err := a.state.Load(r.Context(), id)
		if err != nil {
			http.Error(w, "state unavailable", http.StatusServiceUnavailable)
			return
		}
		if st != nil {
			identity, _ := auth.IdentityFromContext(r.Context())
			if identity == nil || identity.UserID == "" || st.UserID != identity.UserID {
				http.Error(w, "task not found", http.StatusNotFound)
				return
			}
		}
	}

	if res, ok := getResult(id); ok {
		deleteResult(id)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     id,
			"status": res.Status,
			"data":   res.Data,
			"error":  res.Err,
		})
		return
	}

	// Fallback to StateManager for historical tasks
	if a.state != nil {
		st, err := a.state.Load(r.Context(), id)
		if err == nil && st != nil {
			status := "pending"
			var finalData any
			var errStr string

			if st.Status == "timed_out" {
				status = "timed_out"
				errStr = st.Error
			} else if st.Error != "" {
				status = "error"
				errStr = st.Error
			} else if st.Status != "" {
				status = st.Status
				switch st.Status {
				case "completed":
					finalData = st.StepResults
				case "governance_blocked":
					finalData = st.StepResults["_governance"]
				}
			} else if st.Gate != "" {
				status = "await_" + st.Gate
				if st.Gate == "clarification" {
					status = "await_clarification"
				} else {
					status = "await_human"
				}
			} else if pipeline, ok := st.StepResults["_pipeline"].(map[string]any); ok && pipeline["status"] == "success" {
				status = "success"
				finalData = st.StepResults
			} else if len(st.StepResults) > 0 {
				// Partial results are still running (for example while the
				// Analyst is producing the final summary).
				status = "pending"
				finalData = st.StepResults
			} else if len(st.CompletedSteps) > 0 {
				// Fallback for older tasks that didn't save StepResults
				status = "success"
				finalData = map[string]any{"info": "Task completed (no step results saved)"}
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":              id,
				"status":          status,
				"data":            finalData,
				"error":           errStr,
				"react_history":   st.ReActHistory,
				"completed_steps": st.CompletedSteps,
				"pipeline":        st.Pipeline,
			})
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":     id,
		"status": "pending",
	})
}

func (a *APIAgent) handleHumanApprove(w http.ResponseWriter, r *http.Request) {
	a.handleHumanDecision(w, r, "approved")
}

func (a *APIAgent) handleHumanReject(w http.ResponseWriter, r *http.Request) {
	a.handleHumanDecision(w, r, "rejected")
}

func (a *APIAgent) handleHumanDecision(w http.ResponseWriter, r *http.Request, decision string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	gate := r.URL.Query().Get("gate")
	if !idRe.MatchString(id) || gate == "" || len(gate) > 128 || gate == "clarification" {
		http.Error(w, "valid task id and human gate required", http.StatusBadRequest)
		return
	}
	st, ok := a.requireOwnedTask(w, r, id)
	if !ok {
		return
	}
	if st.Gate != gate {
		http.Error(w, "task is not waiting at this gate", http.StatusConflict)
		return
	}
	if a.eventBus != nil {
		a.eventBus.PublishEvent(id, "UI", "human_decision", decision, gate)
	}

	a.bus.Send("verifier", bus.Message{
		Type: "human_decision",
		Payload: map[string]any{
			"id":       id,
			"gate":     gate,
			"decision": decision,
		},
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": decision, "id": id})
}

func (a *APIAgent) DispatchAskInternal(message string, lang string) (string, error) {
	if message == "" {
		return "", errors.New("message required")
	}

	id := randomID()

	logx.Info("Api", "internal UI request id=%s message='%s'", id, message)
	a.eventBus.PublishEvent(id, "UI", "request", message, "")

	_ = NewTaskContext(context.Background(), id, 0)

	a.bus.Send("inspector", bus.Message{
		Type: "new_task",
		Payload: map[string]any{
			"id":      id,
			"mode":    "structured",
			"message": message,
			"lang":    lang,
		},
	})

	return id, nil
}

func (a *APIAgent) handleClarifyReply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	// Auth check
	if !a.checkAuth(r) {
		w.Header().Set("WWW-Authenticate", "Bearer, X-API-Key")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// Rate limit
	if err := a.acquireRL(getClientKey(r)); err != nil {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}

	id := r.URL.Query().Get("id")
	if !idRe.MatchString(id) {
		http.Error(w, "valid id query param required", http.StatusBadRequest)
		return
	}
	st, ok := a.requireOwnedTask(w, r, id)
	if !ok {
		return
	}
	if st.Gate != "clarification" {
		http.Error(w, "task is not waiting for clarification", http.StatusConflict)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAskBodyBytes)
	var req askRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		status := http.StatusBadRequest
		if err.Error() == "http: request body too large" {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, "invalid request body", status)
		return
	}

	if req.Message == "" {
		http.Error(w, "message required", http.StatusBadRequest)
		return
	}

	logx.Info("Api", "clarification reply for id=%s message='%s'", id, req.Message)
	if a.eventBus != nil {
		a.eventBus.PublishEvent(id, "Api", "reply", req.Message, "")
	}

	a.bus.Send("planner", bus.Message{
		Type: "clarify_intent",
		Payload: map[string]any{
			"id":      id,
			"message": req.Message,
		},
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":     id,
		"status": "accepted",
	})
}
