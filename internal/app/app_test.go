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
    "github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
)

// fakeAgent implements agent.Agent for testing App.Run lifecycle.
type fakeAgent struct{
    started atomic.Bool
    ch chan bus.Message
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
    if a.cfg == nil || a.bus == nil || a.ui == nil || a.llm == nil || a.http == nil {
        t.Fatalf("expected non-nil components: cfg=%v bus=%v ui=%v llm=%v http=%v", a.cfg, a.bus, a.ui, a.llm, a.http)
    }
    if len(a.agents) == 0 {
        t.Fatalf("expected at least one agent to be registered")
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
    if err != nil { t.Fatalf("GET /health/live failed: %v", err) }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        t.Fatalf("expected 200, got %d", resp.StatusCode)
    }
}

func TestAppRun_StartsAgentsAndHTTP_AndStopsOnContextCancel(t *testing.T) {
    // Construct a minimal App that uses fake agents and an HTTP server that listens on a random port.
    f1, f2 := &fakeAgent{}, &fakeAgent{}

    mux := http.NewServeMux()
    mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

    a := &App{
        agents: []agent.Agent{f1, f2},
        http: &HTTPServer{srv: &http.Server{Addr: "127.0.0.1:0", Handler: mux}},
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
