package smoke

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    rt "runtime"
    "testing"
    "time"

    "github.com/ccastromar/aos-agentic-orchestration-system/internal/app"
)

// chdirToRepoRoot ensures relative paths like "definitions/..." resolve during tests.
func chdirToRepoRoot(t *testing.T) {
    t.Helper()
    _, file, _, _ := rt.Caller(0)
    root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
    if err := os.Chdir(root); err != nil {
        t.Fatalf("chdir to repo root: %v", err)
    }
}

// TestSmoke_HealthEndpoints boots the app with a fake LLM and checks basic health endpoints.
func TestSmoke_HealthEndpoints(t *testing.T) {
    chdirToRepoRoot(t)

    // Fake Ollama server to satisfy readiness LLM ping (/api/tags)
    ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch {
        case r.Method == http.MethodGet && r.URL.Path == "/api/tags":
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusOK)
            // minimal valid JSON
            _, _ = w.Write([]byte(`{"models":[{"name":"test-model"}]}`))
        default:
            http.NotFound(w, r)
        }
    }))
    defer ollama.Close()

    // Point app to the fake LLM
    t.Setenv("OLLAMA_BASE_URL", ollama.URL)
    t.Setenv("OLLAMA_MODEL", "test-model")
    t.Setenv("LLM_ENGINE", "ollama")

    // Build the app and start background agents (HTTP will be served via httptest)
    aos, err := app.New()
    if err != nil {
        t.Fatalf("app.New() error: %v", err)
    }
    stopAgents := aos.StartAgents(context.Background())
    defer stopAgents()

    srv := httptest.NewServer(aos.Handler())
    defer srv.Close()

    // Helper to GET and decode JSON, returning the parsed body
    getJSON := func(path string) (int, map[string]any, error) {
        client := &http.Client{Timeout: 2 * time.Second}
        resp, err := client.Get(srv.URL + path)
        if err != nil {
            return 0, nil, err
        }
        defer resp.Body.Close()
        var out map[string]any
        _ = json.NewDecoder(resp.Body).Decode(&out)
        return resp.StatusCode, out, nil
    }

    // /health/live should be 200 and contain status ok
    {
        code, body, err := getJSON("/health/live")
        if err != nil {
            t.Fatalf("GET /health/live error: %v", err)
        }
        if code != http.StatusOK {
            t.Fatalf("/health/live expected 200, got %d body=%v", code, body)
        }
        // Accept either ok or ready keyword here, but live returns ok
        if s, _ := body["status"].(string); s == "" {
            t.Fatalf("/health/live missing status field: %v", body)
        }
    }

    // /health/ready should be 200 when specs are loaded and LLM ping is ok
    {
        // give tiny time for agents/runtime to settle
        time.Sleep(50 * time.Millisecond)
        code, body, err := getJSON("/health/ready")
        if err != nil {
            t.Fatalf("GET /health/ready error: %v", err)
        }
        if code != http.StatusOK {
            t.Fatalf("/health/ready expected 200, got %d body=%v", code, body)
        }
        if s, _ := body["status"].(string); s != "ready" {
            t.Fatalf("/health/ready unexpected status: %v", body)
        }
    }
}
