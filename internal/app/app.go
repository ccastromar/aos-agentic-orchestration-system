package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/audit"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/logx"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/runtime"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/agent"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/auth"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/llm"
)

type App struct {
	cfg         *config.Config
	env         *config.EnvVars
	bus         *bus.Bus
	eventBus    *bus.Bus
	agents      []agent.Agent
	llm         llm.LLMClient
	http        *HTTPServer
	auditCloser interface{ Close() error }
	stateCloser interface{ Close() error }
	evidence    *audit.KafkaShipper // nil unless Kafka evidence is configured
}

// New loads environment variables if available and delegates to NewWithEnv.
// It is tolerant to missing env during tests (e.g., required vars not set).
func New() (*App, error) {
	env, err := config.LoadEnv()
	if err != nil {
		return nil, err
	}
	return NewWithEnv(env)
}

// NewWithEnv builds the App wiring using the provided environment variables.
func NewWithEnv(env *config.EnvVars) (*App, error) {
	if env == nil {
		return nil, fmt.Errorf("env cannot be nil")
	}
	cfg, err := config.LoadFromDir("definitions")
	if err != nil {
		return nil, err
	}

	messageBus := bus.New()

	stateStore, stateCloser, err := newStateStore(env)
	if err != nil {
		return nil, err
	}
	var vectorStore state.VectorStore
	if env.QdrantURL == "memory" || env.QdrantURL == "" {
		vectorStore = state.NewMemoryVectorStore()
	} else {
		vectorStore = state.NewQdrantVectorStore(env.QdrantURL)
	}
	stateManager := state.NewStateManager(stateStore, vectorStore)
	aiSystems := governedSystems(cfg)
	auditLogger, auditCloser, err := newAuditLoggerFor(env, aiSystems)
	if err != nil {
		if stateCloser != nil {
			_ = stateCloser.Close()
		}
		return nil, err
	}

	// Select an LLM client based on env. Ensure we always return a non-nil client for tests.
	var llmClient llm.LLMClient

	if env.LLMEngine == "gemini" {
		llmClient, err = llm.NewGeminiClient(context.Background(), env.LLMModel)
		if err != nil {
			if stateCloser != nil {
				_ = stateCloser.Close()
			}
			if auditCloser != nil {
				_ = auditCloser.Close()
			}
			return nil, err
		}
	} else if env.LLMEngine == "ollama" {
		llmClient = llm.NewOllamaClient(env.OllamaBaseURL, env.LLMModel, env.OllamaEmbedModel)
	} else {
		// Default to OpenAI client to keep a.llm non-nil in tests even if API key is missing.
		// The tests don't call the LLM, so an empty key is acceptable.
		llmClient = llm.NewOpenAIClient(env.LLMBaseURL, env.LLMAPIKey, env.LLMModel)
	}

	// Mark specs as loaded only if we actually loaded non-empty specs
	specsLoaded := cfg != nil && len(cfg.Tools) > 0 && len(cfg.Pipelines) > 0 && len(cfg.Intents) > 0

	r := &runtime.Runtime{
		SpecsLoaded: specsLoaded,
		LLMClient:   llmClient,
	}

	// Creates all agents
	apiAgent := agent.NewAPIAgent(messageBus, env, messageBus, cfg, stateManager)
	inspector := agent.NewInspector(messageBus, messageBus)
	planner := agent.NewPlanner(messageBus, cfg, llmClient, messageBus, stateManager)
	verifier := agent.NewVerifier(messageBus, cfg, llmClient, messageBus, stateManager)
	planner.SetAuditLogger(auditLogger)
	verifier.SetAuditLogger(auditLogger)
	analyst := agent.NewAnalyst(messageBus, llmClient, messageBus, stateManager)

	messageBus.Subscribe("api", apiAgent.Inbox())
	messageBus.Subscribe("inspector", inspector.Inbox())
	messageBus.Subscribe("planner", planner.Inbox())
	messageBus.Subscribe("verifier", verifier.Inbox())
	messageBus.Subscribe("analyst", analyst.Inbox())

	evidence, err := newEvidenceShipper(env, aiSystems)
	if err != nil {
		if auditCloser != nil {
			_ = auditCloser.Close()
		}
		if stateCloser != nil {
			_ = stateCloser.Close()
		}
		return nil, fmt.Errorf("initialize Kafka evidence: %w", err)
	}

	app := &App{
		cfg:         cfg,
		env:         env,
		bus:         messageBus,
		agents:      []agent.Agent{apiAgent, inspector, planner, verifier, analyst},
		llm:         llmClient,
		auditCloser: auditCloser,
		stateCloser: stateCloser,
		evidence:    evidence,
	}

	httpServer := NewHTTPServer(app, apiAgent, messageBus, r)
	app.http = httpServer

	return app, nil
}

// governedSystems maps each pipeline that declares ai_system to that Prudence system id.
func governedSystems(cfg *config.Config) map[string]string {
	out := map[string]string{}
	if cfg == nil {
		return out
	}
	for name, p := range cfg.Pipelines {
		if id := strings.TrimSpace(p.AiSystem); id != "" {
			out[name] = id
		}
	}
	return out
}

// newEvidenceShipper returns nil when no Kafka broker is configured. Kafka evidence needs the
// local audit journal (it is the outbox), and outside development an encrypted connection.
func newEvidenceShipper(env *config.EnvVars, aiSystems map[string]string) (*audit.KafkaShipper, error) {
	brokers := splitList(env.EvidenceKafkaBrokers)
	if len(brokers) == 0 {
		return nil, nil
	}
	protocol := strings.ToUpper(strings.TrimSpace(env.EvidenceKafkaSecurity))
	if !auth.IsDevelopmentEnvironment(env.AppEnv) && protocol != "SSL" && protocol != "SASL_SSL" {
		return nil, fmt.Errorf("AOS_EVIDENCE_KAFKA_SECURITY_PROTOCOL must be SSL or SASL_SSL outside development (got %q)", env.EvidenceKafkaSecurity)
	}
	key, err := evidenceKey(env)
	if err != nil {
		return nil, err
	}
	return audit.NewKafkaShipper(audit.KafkaShipperConfig{
		EvidenceKey: key,
		JournalPath: env.AuditLogPath,
		CursorPath:  env.EvidenceKafkaCursor,
		Brokers:     brokers,
		Topic:       env.EvidenceKafkaTopic,
		Interval:    env.EvidenceKafkaInterval,
		Security: audit.KafkaSecurity{
			Protocol:      env.EvidenceKafkaSecurity,
			CAFile:        env.EvidenceKafkaCAFile,
			SASLMechanism: env.EvidenceKafkaSASLMechanism,
			Username:      env.EvidenceKafkaSASLUsername,
			Password:      env.EvidenceKafkaSASLPassword,
		},
		AISystems: aiSystems,
	})
}

// evidenceKey loads the key audit hashes are digested with before they leave AOS.
func evidenceKey(env *config.EnvVars) ([]byte, error) {
	return audit.LoadEvidenceKey(env.EvidenceKey, env.EvidenceKeyFile, env.AuditLogPath, auth.IsDevelopmentEnvironment(env.AppEnv))
}

func splitList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func newAuditLogger(env *config.EnvVars) (audit.Logger, interface{ Close() error }, error) {
	return newAuditLoggerFor(env, nil)
}

func newAuditLoggerFor(env *config.EnvVars, aiSystems map[string]string) (audit.Logger, interface{ Close() error }, error) {
	var sinks []audit.Logger
	if env.AuditLogPath != "" {
		chain, err := audit.NewFileChain(env.AuditLogPath)
		if err != nil {
			return nil, nil, fmt.Errorf("initialize audit chain: %w", err)
		}
		sinks = append(sinks, chain)
	}
	if strings.TrimSpace(env.ErnestIngestorURL) != "" {
		if strings.TrimSpace(env.ErnestIngestorAPIKey) == "" && !auth.IsDevelopmentEnvironment(env.AppEnv) {
			for _, sink := range sinks {
				if closer, ok := sink.(interface{ Close() error }); ok {
					_ = closer.Close()
				}
			}
			return nil, nil, fmt.Errorf("ERNEST_INGESTOR_API_KEY is required outside development mode")
		}
		key, err := evidenceKey(env)
		if err != nil {
			for _, sink := range sinks {
				if closer, ok := sink.(interface{ Close() error }); ok {
					_ = closer.Close()
				}
			}
			return nil, nil, err
		}
		remote, err := audit.NewErnestLogger(env.ErnestIngestorURL, env.ErnestIngestorAPIKey, env.ErnestIngestorHMACSecret, env.ErnestIngestorTimeout)
		if err != nil {
			for _, sink := range sinks {
				if closer, ok := sink.(interface{ Close() error }); ok {
					_ = closer.Close()
				}
			}
			return nil, nil, fmt.Errorf("initialize Ernest audit integration: %w", err)
		}
		sinks = append(sinks, remote.WithAISystems(aiSystems).WithEvidenceKey(key))
	}
	if len(sinks) == 0 {
		return audit.StdLogger{}, nil, nil
	}
	combined := audit.NewMultiLogger(sinks...)
	return combined, combined, nil
}

func newStateStore(env *config.EnvVars) (state.StateStore, interface{ Close() error }, error) {
	backend := strings.ToLower(strings.TrimSpace(env.StateStore))
	if backend == "" {
		if env.RedisAddr != "" {
			backend = "redis" // preserve the existing REDIS_ADDR opt-in behavior
		} else {
			backend = "memory"
		}
	}
	switch backend {
	case "memory":
		return state.NewMemoryStore(), nil, nil
	case "sqlite":
		store, err := state.NewSQLiteStore(env.SQLitePath)
		if err != nil {
			return nil, nil, err
		}
		return store, store, nil
	case "postgres":
		if env.PostgresURL == "" {
			return nil, nil, fmt.Errorf("AOS_POSTGRES_URL is required when AOS_STATE_STORE=postgres")
		}
		store, err := state.NewPostgresStore(env.PostgresURL)
		if err != nil {
			return nil, nil, err
		}
		return store, store, nil
	case "redis":
		if env.RedisAddr == "" {
			return nil, nil, fmt.Errorf("REDIS_ADDR is required when AOS_STATE_STORE=redis")
		}
		rdb := redis.NewClient(&redis.Options{
			Addr:     env.RedisAddr,
			Password: env.RedisPassword,
			DB:       0,
		})
		if err := rdb.Ping(context.Background()).Err(); err != nil {
			_ = rdb.Close()
			return nil, nil, fmt.Errorf("redis connection failed: %w", err)
		}
		store := state.NewRedisStore(rdb, 24*time.Hour)
		return store, rdb, nil
	default:
		return nil, nil, fmt.Errorf("unsupported AOS_STATE_STORE %q (expected memory, sqlite, postgres, or redis)", backend)
	}
}

func (a *App) Run(ctx context.Context) error {
	if a.stateCloser != nil {
		defer a.stateCloser.Close()
	}
	if a.auditCloser != nil {
		defer a.auditCloser.Close()
	}
	g, gctx := errgroup.WithContext(ctx)

	// start agents
	for _, ag := range a.agents {
		agentAI := ag
		g.Go(func() error {
			return agentAI.Start(gctx)
		})
	}

	// Launch HTTP server
	g.Go(func() error {
		return a.http.Start(gctx)
	})

	// Ship the audit journal to Ernest over Kafka in the background (never blocks pipelines).
	if a.evidence != nil {
		g.Go(func() error {
			return a.evidence.Run(gctx)
		})
	}

	if a.env != nil {
		logx.Info("App", "AOS v0.4.0 started (env=%s)", a.env.AppEnv)
	} else {
		logx.Info("App", "AOS v0.4.0 started")
	}

	return g.Wait()
}

// Handler exposes the HTTP handler for embedding in httptest servers
// during end-to-end and functional tests, avoiding real port binding.
func (a *App) Handler() http.Handler {
	if a == nil || a.http == nil || a.http.srv == nil {
		return http.NewServeMux()
	}
	return a.http.srv.Handler
}

// SetLLMClient dynamically updates the LLMClient for the App and all active agents.
func (a *App) SetLLMClient(engine, model, url string) error {
	var newClient llm.LLMClient
	var err error

	if engine == "gemini" {
		newClient, err = llm.NewGeminiClient(context.Background(), model)
		if err != nil {
			return err
		}
	} else if engine == "ollama" {
		// Use environment defaults or hardcoded defaults for embeddings if not provided
		embedModel := "nomic-embed-text"
		if a.env != nil && a.env.OllamaEmbedModel != "" {
			embedModel = a.env.OllamaEmbedModel
		}
		newClient = llm.NewOllamaClient(url, model, embedModel)
	} else {
		newClient = llm.NewOpenAIClient(url, "", model)
	}

	a.llm = newClient

	// Propagate to all agents that support dynamic LLM updating
	for _, ag := range a.agents {
		if consumer, ok := ag.(interface{ SetLLMClient(llm.LLMClient) }); ok {
			consumer.SetLLMClient(newClient)
		}
	}

	// Update the environment variables so they reflect the new state
	if a.env != nil {
		a.env.LLMEngine = engine
		a.env.LLMModel = model
		if engine == "ollama" {
			a.env.OllamaBaseURL = url
		} else {
			a.env.LLMBaseURL = url
		}
	}

	logx.Info("App", "Dynamically updated LLM client to Engine=%s, Model=%s", engine, model)
	return nil
}

// StartAgents launches the background agent goroutines without starting
// the HTTP server. It returns a cancel function to stop them.
// This is intended for tests that embed the HTTP handler in an httptest server.
func (a *App) StartAgents(parent context.Context) context.CancelFunc {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	for _, ag := range a.agents {
		agentAI := ag
		go func() { _ = agentAI.Start(ctx) }()
	}
	return cancel
}

// HandlePipelines returns the currently loaded pipelines to the frontend
func (a *App) HandlePipelines(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if a.cfg == nil || a.cfg.Pipelines == nil {
		w.Write([]byte(`{}`))
		return
	}

	// Convert config pipelines to JSON
	json.NewEncoder(w).Encode(a.cfg.Pipelines)
}

type llmSettingsRequest struct {
	Engine  string `json:"engine"`
	Model   string `json:"model"`
	BaseURL string `json:"baseUrl"`
}

// HandleSettingsLLM gets or updates the current LLM configuration
func (a *App) HandleSettingsLLM(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodGet {
		baseUrl := a.env.LLMBaseURL
		if a.env.LLMEngine == "ollama" {
			baseUrl = a.env.OllamaBaseURL
		}
		resp := llmSettingsRequest{
			Engine:  a.env.LLMEngine,
			Model:   a.env.LLMModel,
			BaseURL: baseUrl,
		}
		json.NewEncoder(w).Encode(resp)
		return
	}

	if r.Method == http.MethodPost {
		var req llmSettingsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if err := a.SetLLMClient(req.Engine, req.Model, req.BaseURL); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Write([]byte(`{"status": "ok"}`))
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}
