package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/agent"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/audit"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
)

// fakeAgent implements agent.Agent for testing App.Run lifecycle.
type fakeAgent struct {
	started atomic.Bool
	ch      chan bus.Message
}

func (f *fakeAgent) Start(ctx context.Context) error {
	f.started.Store(true)
	<-ctx.Done()
	return nil
}

func (f *fakeAgent) Inbox() chan bus.Message {
	if f.ch == nil {
		f.ch = make(chan bus.Message, 1)
	}
	return f.ch
}

var _ agent.Agent = (*fakeAgent)(nil)

// chdirToRepoRoot ensures relative paths like "definitions/..." resolve during tests.
func chdirToRepoRoot(t *testing.T) {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir to repo root: %v", err)
	}
}

func TestNew_ConstructsApp(t *testing.T) {
	chdirToRepoRoot(t)
	a, err := New()
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	if a.cfg == nil || a.bus == nil || a.llm == nil || a.http == nil {
		t.Fatalf("expected non-nil components: cfg=%v bus=%v llm=%v http=%v", a.cfg, a.bus, a.llm, a.http)
	}
	if len(a.agents) == 0 {
		t.Fatalf("expected at least one agent to be registered")
	}
}

func TestNewStateStoreBackendSelection(t *testing.T) {
	t.Run("legacy default is memory", func(t *testing.T) {
		store, closer, err := newStateStore(&config.EnvVars{})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := store.(*state.MemoryStore); !ok {
			t.Fatalf("store type = %T", store)
		}
		if closer != nil {
			t.Fatal("memory store should not require closing")
		}
	})
	t.Run("sqlite backend", func(t *testing.T) {
		store, closer, err := newStateStore(&config.EnvVars{StateStore: "sqlite", SQLitePath: filepath.Join(t.TempDir(), "aos.db")})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := store.(*state.SQLiteStore); !ok {
			t.Fatalf("store type = %T", store)
		}
		if closer == nil {
			t.Fatal("sqlite store should provide a closer")
		}
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("redis requires address", func(t *testing.T) {
		if _, _, err := newStateStore(&config.EnvVars{StateStore: "redis"}); err == nil {
			t.Fatal("expected missing REDIS_ADDR error")
		}
	})
}

func TestNewAuditLoggerRequiresErnestKeyOutsideDevelopment(t *testing.T) {
	logger, closer, err := newAuditLogger(&config.EnvVars{
		AppEnv:            "production",
		ErnestIngestorURL: "http://localhost:3011",
	})
	if err == nil || logger != nil || closer != nil {
		t.Fatalf("expected production Ernest integration without key to fail, got logger=%T closer=%T err=%v", logger, closer, err)
	}
}

func TestNewAuditLoggerAllowsLocalErnestWithoutKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Ernest-Ingest-Key") != "" {
			t.Errorf("unexpected key header in local no-key mode")
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	logger, closer, err := newAuditLogger(&config.EnvVars{
		AppEnv:            "local",
		AuditLogPath:      filepath.Join(t.TempDir(), "aos-audit.jsonl"),
		ErnestIngestorURL: server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()
	logger.Log(audit.Event{TraceID: "local-trace", Phase: "test", Result: "success"})
	if err := audit.LoggerError(logger); err != nil {
		t.Fatalf("local Ernest event failed: %v", err)
	}
	provider, ok := logger.(audit.HashProvider)
	if !ok || provider.HashForTrace("local-trace") == "" {
		t.Fatal("composite logger lost the local hash provider used by anchoring pipelines")
	}
}

func TestHTTPServer_Routes_LiveOK(t *testing.T) {
	// Build a real app to get the mux with health routes.
	chdirToRepoRoot(t)
	a, err := New()
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// Wrap the app's HTTP handler into a test server to avoid binding real ports.
	ts := httptest.NewServer(a.http.srv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health/live")
	if err != nil {
		t.Fatalf("GET /health/live failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestEventsStreamDoesNotBypassAuthentication(t *testing.T) {
	chdirToRepoRoot(t)
	t.Setenv("JWT_SECRET", "test-only-secret")
	a, err := New()
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/stream?task_id=task-1", nil)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated event stream status = %d, want 401", rec.Code)
	}
}

func TestAppRun_StartsAgentsAndHTTP_AndStopsOnContextCancel(t *testing.T) {
	// Construct a minimal App that uses fake agents and an HTTP server that listens on a random port.
	f1, f2 := &fakeAgent{}, &fakeAgent{}

	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	a := &App{
		agents: []agent.Agent{f1, f2},
		http:   &HTTPServer{srv: &http.Server{Addr: "127.0.0.1:0", Handler: mux}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	// Give some time for goroutines to start.
	time.Sleep(50 * time.Millisecond)
	if !f1.started.Load() || !f2.started.Load() {
		t.Fatalf("expected both fake agents to have started, got f1=%v f2=%v", f1.started.Load(), f2.started.Load())
	}

	// Cancel the context and expect Run to return cleanly.
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error after cancel: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Run to return after cancel")
	}
}
