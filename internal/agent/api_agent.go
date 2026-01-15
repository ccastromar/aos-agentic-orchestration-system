package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ccastromar/aos-agent-orchestration-system/internal/auth"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/logx"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/payload"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/ui"
)

type APIAgent struct {
	bus     *bus.Bus
	inbox   chan bus.Message
	uiStore *ui.UIStore // <-- nuevo
	// minimal auth and rate limiting
	apiKey    string
	authChain *auth.Chain
	// naive fixed-window rate limiter per client key
	rl struct {
		Window  time.Duration
		Limit   int
		mu      chan struct{} // lightweight mutex using channel
		buckets map[string]*rateBucket
	}
}

// NewAPIAgent creates the API agent. Backward-compatible signature:
// - NewAPIAgent(b, env, uiStore)
// - NewAPIAgent(b, uiStore)
func NewAPIAgent(b *bus.Bus, args ...any) *APIAgent {
    var (
        env    *config.EnvVars
        uiS    *ui.UIStore
    )
    for _, arg := range args {
        switch v := arg.(type) {
        case *config.EnvVars:
            env = v
        case *ui.UIStore:
            uiS = v
        }
    }
    if uiS == nil {
        uiS = ui.NewUIStore()
    }

    a := &APIAgent{
        bus:     b,
        inbox:   make(chan bus.Message, 16),
        uiStore: uiS,
        apiKey:  strings.TrimSpace(os.Getenv("API_KEY")),
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
		a.uiStore.AddEvent(id, "API", "await_human", gate, "")
	default:
		// ignore silently
	}
}

type askRequest struct {
	Operation string         `json:"operation,omitempty"`
	Params    map[string]any `json:"params,omitempty"`
	Message   string         `json:"message"`
}

type askNLPRequest struct {
	Message string `json:"message"`
}

type askResponse struct {
	ID     string      `json:"id"`
	Status string      `json:"status"`
	Result interface{} `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

// RegisterHTTP registra endpoints HTTP
func (a *APIAgent) RegisterHTTP(mux *http.ServeMux) {
	mux.HandleFunc("/ask", a.handleAsk)             // async NLP-like mode (message)
	mux.HandleFunc("/ask_structured", a.handleAsk2) // sync: operation + params
	mux.HandleFunc("/task", a.handleTask)           // fetch task status/result
	mux.HandleFunc("/task/approve", a.handleHumanApprove)
	mux.HandleFunc("/task/reject", a.handleHumanReject)

	//mux.HandleFunc("/ask_nlp", a.handleAskNLP) // modo lenguaje natural
}

func (a *APIAgent) handleAsk(w http.ResponseWriter, r *http.Request) {
	// Method check
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	// Auth check (optional)
	//if !a.checkAuth(r) {
	//	w.Header().Set("WWW-Authenticate", "Bearer, X-API-Key")
	//	http.Error(w, "unauthorized", http.StatusUnauthorized)
	//	return
	//}
 // Chain auth only if we have authenticators configured
 if len(a.authChain.Authenticators) > 0 {
     identity, err := a.authChain.Authenticate(r)
     if err != nil {
         if ae, ok := err.(*auth.AuthError); ok {
             http.Error(w, ae.Message, ae.Code)
         } else {
             http.Error(w, "auth failed", http.StatusUnauthorized)
         }
         return
     }
     r = r.WithContext(auth.WithIdentity(r.Context(), identity))
 }

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
		Lang    string `json:"lang"`
		Message string `json:"message"`
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

	id := randomID()

	logx.Info("Api", "new request lang=%s id=%s message='%s'", req.Lang, id, req.Message)
	a.uiStore.AddEvent(id, "Api", "request", req.Message, "")

	_ = NewTaskContext(context.Background(), id, 0)
	logx.Debug("Api", "Created NEW TaskContext for %s", id)

	// Enviar al inspector con el message correcto
	a.bus.Send("inspector", bus.Message{
		Type: "new_task",
		Payload: map[string]any{
			"id":      id,
			"mode":    "structured",
			"message": req.Message,
			"lang":    req.Lang,
		},
	})

	// Respuesta asíncrona inmediata
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":     id,
		"status": "accepted",
	})
}

// /ask → operation + params (como v1)
func (a *APIAgent) handleAsk2(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Auth check (igual que /ask)
	if !a.checkAuth(r) {
		w.Header().Set("WWW-Authenticate", "Bearer, X-API-Key")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// Rate limit (igual que /ask)
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
		log.Println("[API] error parseando request:", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"json inválido"}`))
		return
	}

	if req.Operation == "" && req.Message == "" {
		http.Error(w, "operation o message requerido", 400)
		return
	}

	id := randomID()

	a.bus.Send("inspector", bus.Message{
		Type: "new_task",
		Payload: map[string]any{
			"id":        id,
			"mode":      "structured",
			"operation": req.Operation,
			"params":    req.Params,
		},
	})

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

// handleTask devuelve el estado/resultados de una tarea.
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
		http.Error(w, "id requerido", http.StatusBadRequest)
		return
	}
	if !idRe.MatchString(id) {
		http.Error(w, "id inválido", http.StatusBadRequest)
		return
	}

	// Consultar si ya hay resultado
	if res, ok := getResult(id); ok {
		// Limpiar almacenamiento para evitar fugas
		deleteResult(id)
		w.Header().Set("Content-Type", "application/json")
		// Mapear al formato de respuesta anterior
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     id,
			"status": res.Status,
			"data":   res.Data,
			"error":  res.Err,
		})
		return
	}

	// Aún pendiente
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":     id,
		"status": "pending",
	})
}

// /ask_nlp → message (texto libre)
func (a *APIAgent) handleAskNLP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req askNLPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Println("[API] error parseando request NLP:", err)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"json inválido"}`))
		return
	}

	if req.Message == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"message requerido"}`))
		return
	}

	id := randomID()

	a.bus.Send("inspector", bus.Message{
		Type: "new_task",
		Payload: map[string]any{
			"id":      id,
			"mode":    "nlp",
			"message": req.Message,
		},
	})

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

func (a *APIAgent) handleTaskApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Auth
	if !a.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id requerido", http.StatusBadRequest)
		return
	}

	// Emit message to Verifier
	a.bus.Send("verifier", bus.Message{
		Type: "human_decision",
		Payload: map[string]any{
			"id":       id,
			"decision": "approved",
		},
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id":       id,
		"status":   "accepted",
		"decision": "approved",
	})
}

func (a *APIAgent) handleTaskReject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Auth
	if !a.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id requerido", http.StatusBadRequest)
		return
	}

	// Emit message to Verifier
	a.bus.Send("verifier", bus.Message{
		Type: "human_decision",
		Payload: map[string]any{
			"id":       id,
			"decision": "rejected",
		},
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id":       id,
		"status":   "accepted",
		"decision": "rejected",
	})
}

func (a *APIAgent) handleHumanApprove(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")

	a.bus.Send("verifier", bus.Message{
		Type: "human_decision",
		Payload: map[string]any{
			"id":       id,
			"decision": "approved",
		},
	})

	w.WriteHeader(200)
	w.Write([]byte(`{"status":"ok","id":"` + id + `"}`))
}

func (a *APIAgent) handleHumanReject(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")

	a.bus.Send("verifier", bus.Message{
		Type: "human_decision",
		Payload: map[string]any{
			"id":       id,
			"decision": "rejected",
		},
	})

	w.WriteHeader(200)
	w.Write([]byte(`{"status":"rejected","id":"` + id + `"}`))
}

func (a *APIAgent) DispatchAskInternal(message string, lang string) (string, error) {
	if message == "" {
		return "", errors.New("message requerido")
	}

	id := randomID()

	logx.Info("Api", "internal UI request id=%s message='%s'", id, message)
	a.uiStore.AddEvent(id, "UI", "request", message, "")

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
