package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/logx"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/runtime"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/agent"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/llm"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/ui"
)

type App struct {
	cfg    *config.Config
	env    *config.EnvVars
	bus    *bus.Bus
	ui     *ui.UIStore
	agents []agent.Agent
	llm    llm.LLMClient
	http   *HTTPServer
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

	uiStore := ui.NewUIStore(nil)
	messageBus := bus.New()

	var stateStore state.StateStore
	if env.RedisAddr != "" {
		rdb := redis.NewClient(&redis.Options{
			Addr:     env.RedisAddr,
			Password: env.RedisPassword,
			DB:       0,
		})
		if err := rdb.Ping(context.Background()).Err(); err != nil {
			return nil, fmt.Errorf("redis connection failed: %w", err)
		}
		stateStore = state.NewRedisStore(rdb, 24*time.Hour)
	} else {
		stateStore = state.NewMemoryStore()
	}

	var vectorStore state.VectorStore
	if env.QdrantURL == "memory" || env.QdrantURL == "" {
		vectorStore = state.NewMemoryVectorStore()
	} else {
		vectorStore = state.NewQdrantVectorStore(env.QdrantURL)
	}
	stateManager := state.NewStateManager(stateStore, vectorStore)

	// Select an LLM client based on env. Ensure we always return a non-nil client for tests.
	var llmClient llm.LLMClient

	if env.LLMEngine == "gemini" {
		llmClient, err = llm.NewGeminiClient(context.Background(), env.LLMModel)
		if err != nil {
			return nil, err
		}
	} else if env.LLMEngine == "ollama" {
		llmClient = llm.NewOllamaClient(env.OllamaBaseURL, env.LLMModel, env.OllamaEmbedModel)
	} else {
		// Default to OpenAI client to keep a.llm non-nil in tests even if API key is missing.
		// The tests don't call the LLM, so an empty key is acceptable.
		llmClient = llm.NewOpenAIClient(env.LLMBaseURL, "", env.LLMModel)
	}

	// Mark specs as loaded only if we actually loaded non-empty specs
	specsLoaded := cfg != nil && len(cfg.Tools) > 0 && len(cfg.Pipelines) > 0 && len(cfg.Intents) > 0

	r := &runtime.Runtime{
		SpecsLoaded: specsLoaded,
		LLMClient:   llmClient,
	}

	// Creates all agents
	apiAgent := agent.NewAPIAgent(messageBus, env, uiStore, cfg)
	inspector := agent.NewInspector(messageBus, uiStore)
	planner := agent.NewPlanner(messageBus, cfg, llmClient, uiStore, stateManager)
	verifier := agent.NewVerifier(messageBus, cfg, llmClient, uiStore, stateManager)
	analyst := agent.NewAnalyst(messageBus, llmClient, uiStore, stateManager)

	uiStore.SetDispatcher(apiAgent)

	messageBus.Subscribe("api", apiAgent.Inbox())
	messageBus.Subscribe("inspector", inspector.Inbox())
	messageBus.Subscribe("planner", planner.Inbox())
	messageBus.Subscribe("verifier", verifier.Inbox())
	messageBus.Subscribe("analyst", analyst.Inbox())

	app := &App{
		cfg:    cfg,
		env:    env,
		bus:    messageBus,
		ui:     uiStore,
		agents: []agent.Agent{apiAgent, inspector, planner, verifier, analyst},
		llm:    llmClient,
	}

	httpServer := NewHTTPServer(app, apiAgent, uiStore, r)
	app.http = httpServer

	return app, nil
}

func (a *App) Run(ctx context.Context) error {
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

	if a.env != nil {
		logx.Info("App", "AOS v0.3.0 started (env=%s)", a.env.AppEnv)
	} else {
		logx.Info("App", "AOS v0.3.0 started")
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
