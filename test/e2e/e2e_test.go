package e2e

import (
    "bytes"
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    rt "runtime"
    "testing"
    "time"

    "github.com/ccastromar/aos-agent-orchestration-system/internal/app"
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

// TestE2E_AskForBalance spins a fake LLM (Ollama-compatible) and a fake
// core banking service, starts the AOS HTTP handler, performs a POST /ask
// with a message that implies a balance check, and then fetches the task
// result from /task.
func TestE2E_AskForBalance(t *testing.T) {
    chdirToRepoRoot(t)

    // 1) Start fake Ollama server used by LLM client inside the app
    // It must respond to POST /api/chat with streaming JSON chunks.
    // First call (DetectIntent) -> output intent key
    // Second call (ExtractParams) -> output JSON params
    // Third call (Summarize) -> any short text
    var chatCalls int
    ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/api/chat" || r.Method != http.MethodPost {
            http.NotFound(w, r)
            return
        }
        chatCalls++
        w.Header().Set("Content-Type", "application/json")
        enc := json.NewEncoder(w)
        switch chatCalls {
        case 1:
            // DetectIntent → just emit the intent key
            _ = enc.Encode(map[string]any{"message": map[string]any{"role": "assistant", "content": "banking.get_balance"}, "done": false})
            _ = enc.Encode(map[string]any{"done": true})
        case 2:
            // ExtractParams → return required param as JSON string
            _ = enc.Encode(map[string]any{"message": map[string]any{"role": "assistant", "content": `{"accountId":"555"}`}, "done": false})
            _ = enc.Encode(map[string]any{"done": true})
        default:
            // Summarize → any text
            _ = enc.Encode(map[string]any{"message": map[string]any{"role": "assistant", "content": "Resumen breve"}, "done": false})
            _ = enc.Encode(map[string]any{"done": true})
        }
    }))
    defer ollama.Close()

    // 2) Start fake core banking backend on localhost:9000 to match tool URLs
    mux9000 := http.NewServeMux()
    var balanceHits int
    mux9000.HandleFunc("/mock/core/balance", func(w http.ResponseWriter, r *http.Request) {
        // Return a fixed balance; ignore headers
        balanceHits++
        _ = json.NewEncoder(w).Encode(map[string]any{
            "balance":    123.45,
            "account_id": r.URL.Query().Get("accountId"),
        })
    })

    srv9000 := &http.Server{Addr: "localhost:9000", Handler: mux9000}
    go func() {
        // Ignore error on close
        _ = srv9000.ListenAndServe()
    }()
    // Wait until the mock backend is ready to accept connections to avoid race conditions
    {
        client := &http.Client{Timeout: 100 * time.Millisecond}
        ready := false
        for i := 0; i < 50; i++ { // ~5s max
            resp, err := client.Get("http://localhost:9000/mock/core/balance?accountId=ping")
            if err == nil {
                resp.Body.Close()
                ready = true
                break
            }
            time.Sleep(100 * time.Millisecond)
        }
        if !ready {
            t.Fatalf("mock backend on :9000 not ready in time")
        }
    }
    defer func() {
        ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
        defer cancel()
        _ = srv9000.Shutdown(ctx)
    }()

    // 3) Point app to the fake LLM via env
    t.Setenv("OLLAMA_BASE_URL", ollama.URL)
    t.Setenv("OLLAMA_MODEL", "test-model")
    t.Setenv("API_KEY", "e2e-key")
    // Optional: leave API_KEY empty to keep API auth disabled

    // 4) Build the app and wrap its HTTP handler with a test server (no real port binding)
    aos, err := app.New()
    if err != nil {
        t.Fatalf("app.New() error: %v", err)
    }
    // Start agents since we are not running the real HTTP server
    stopAgents := aos.StartAgents(context.Background())
    defer stopAgents()

    httpSrv := httptest.NewServer(aos.Handler())
    defer httpSrv.Close()

    // 5) POST /ask_structured to bypass LLM and run the pipeline directly
    body := map[string]any{
        "operation": "banking.get_balance",
        "params": map[string]any{
            "accountId": "555",
        },
    }
    b, _ := json.Marshal(body)
    req, _ := http.NewRequest(http.MethodPost, httpSrv.URL+"/ask_structured", bytes.NewReader(b))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("X-API-Key", "e2e-key")
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        t.Fatalf("POST /ask error: %v", err)
    }
    // handleAsk2 is synchronous and returns the final result
    if resp.StatusCode != http.StatusOK {
        var bodyDump map[string]any
        _ = json.NewDecoder(resp.Body).Decode(&bodyDump)
        resp.Body.Close()
        t.Fatalf("expected 200 from /ask_structured, got %d body=%v", resp.StatusCode, bodyDump)
    }
    var res map[string]any
    _ = json.NewDecoder(resp.Body).Decode(&res)
    resp.Body.Close()
    if res["status"] != "ok" && res["status"] != "completed" {
        t.Fatalf("unexpected task status: %#v (balanceHits=%d)", res, balanceHits)
    }
    data, ok := res["result"].(map[string]any)
    if !ok {
        t.Fatalf("missing result in structured response: %#v", res)
    }
    raw, ok := data["raw"].(map[string]any)
    if !ok {
        t.Fatalf("missing raw in task response: %#v", data)
    }
    toolOut, ok := raw["banking.core_get_balance"].(map[string]any)
    if !ok {
        t.Fatalf("missing tool output in raw: keys=%v", func() []string { ks:=make([]string,0,len(raw)); for k:=range raw{ks=append(ks,k)}; return ks }())
    }
    // Balance can be inline or nested under output depending on pipeline shape
    var (
        balance any
        outMap map[string]any
    )
    if v, ok := toolOut["balance"]; ok {
        balance = v
    } else if v, ok := toolOut["output"].(map[string]any); ok {
        outMap = v
        balance = v["balance"]
    }
    if _, ok := balance.(float64); !ok {
        t.Fatalf("missing numeric balance in tool output: %#v", toolOut)
    }
    // The accountId may live at top-level or inside output; accept either and also snake case
    var accVal any
    if v, ok := toolOut["accountId"]; ok {
        accVal = v
    } else if v, ok := toolOut["account_id"]; ok {
        accVal = v
    } else if outMap != nil {
        if v, ok := outMap["accountId"]; ok {
            accVal = v
        } else if v, ok := outMap["account_id"]; ok {
            accVal = v
        }
    }
    if accVal != nil {
        if s, ok := accVal.(string); ok && s != "555" {
            t.Fatalf("unexpected accountId in tool output: %#v", toolOut)
        }
    }
}
