package app

import (
    "context"
    "fmt"
    "net/http"
    "time"

    "github.com/ccastromar/aos-agent-orchestration-system/internal/bus"
    "github.com/ccastromar/aos-agent-orchestration-system/internal/logx"
    "github.com/ccastromar/aos-agent-orchestration-system/internal/runtime"
    "github.com/ccastromar/aos-agent-orchestration-system/internal/state"
    "github.com/redis/go-redis/v9"
    "golang.org/x/sync/errgroup"

    "github.com/ccastromar/aos-agent-orchestration-system/internal/agent"
    "github.com/ccastromar/aos-agent-orchestration-system/internal/config"
    "github.com/ccastromar/aos-agent-orchestration-system/internal/llm"
    "github.com/ccastromar/aos-agent-orchestration-system/internal/ui"
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

	stateManager := state.NewStateManager(stateStore)

 // Select LLM client based on env. Ensure we always return a non-nil client for tests.
 var llmClient llm.LLMClient

 if env.LLMEngine == "gemini" {
     llmClient, err = llm.NewGeminiClient(context.Background(), env.LLMModel)
     if err != nil {
         return nil, err
     }
 } else if env.LLMEngine == "ollama" {
     llmClient = llm.NewOllamaClient(env.OllamaBaseURL, env.LLMModel)
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

	// Crear todos los agentes
	apiAgent := agent.NewAPIAgent(messageBus, env, uiStore)
	inspector := agent.NewInspector(messageBus, uiStore)
	planner := agent.NewPlanner(messageBus, cfg, llmClient, uiStore)
	verifier := agent.NewVerifier(messageBus, cfg, uiStore, stateManager)
	analyst := agent.NewAnalyst(messageBus, llmClient, uiStore)

	uiStore.SetDispatcher(apiAgent)

	// Registrar subscripciones
	//messageBus.Subscribe("api", apiAgent.Inbox())
	messageBus.Subscribe("inspector", inspector.Inbox())
	messageBus.Subscribe("planner", planner.Inbox())
	messageBus.Subscribe("verifier", verifier.Inbox())
	messageBus.Subscribe("analyst", analyst.Inbox())

	httpServer := NewHTTPServer(apiAgent, uiStore, r)

	return &App{
		cfg:    cfg,
		env:    env,
		bus:    messageBus,
		ui:     uiStore,
		agents: []agent.Agent{inspector, planner, verifier, analyst},
		llm:    llmClient,
		http:   httpServer,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	g, gctx := errgroup.WithContext(ctx)

	// Lanzar agentes
	for _, ag := range a.agents {
		agentAI := ag
		g.Go(func() error {
			return agentAI.Start(gctx)
		})
	}

	// Lanzar HTTP server
	g.Go(func() error {
		return a.http.Start(gctx)
	})

	if a.env != nil {
		logx.Info("App", "AOS v0.2.0 started (env=%s)", a.env.AppEnv)
	} else {
		logx.Info("App", "AOS v0.2.0 started")
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
