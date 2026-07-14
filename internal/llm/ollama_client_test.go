package llm

import (
    "bufio"
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
)

func TestPing_OK(t *testing.T) {
    ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/api/tags" {
            t.Fatalf("unexpected path: %s", r.URL.Path)
        }
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"models":[{"name":"qwen3:0.6b"}]}`))
    }))
    defer ts.Close()

    c := NewOllamaClient(ts.URL, "qwen3:0.6b", "test-embed")
    if err := c.Ping(context.Background()); err != nil {
        t.Fatalf("Ping() unexpected error: %v", err)
    }
}

func TestPing_Non200(t *testing.T) {
    ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusBadGateway)
    }))
    defer ts.Close()

    c := NewOllamaClient(ts.URL, "qwen3:0.6b", "test-embed")
    if err := c.Ping(context.Background()); err == nil {
        t.Fatalf("expected error when non-200 status")
    }
}

func TestChat_StreamsConcatenated(t *testing.T) {
    ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/api/chat" {
            t.Fatalf("unexpected path: %s", r.URL.Path)
        }
        w.Header().Set("Content-Type", "application/json")
        bw := bufio.NewWriter(w)
        // send two chunks with message content, then a done=true chunk
        _ = json.NewEncoder(bw).Encode(map[string]any{
            "message": map[string]any{"role": "assistant", "content": "Hello"},
            "done":    false,
        })
        bw.Flush()
        _ = json.NewEncoder(bw).Encode(map[string]any{
            "message": map[string]any{"role": "assistant", "content": ", world"},
            "done":    false,
        })
        bw.Flush()
        _ = json.NewEncoder(bw).Encode(map[string]any{
            "done": true,
        })
        bw.Flush()
    }))
    defer ts.Close()

    c := NewOllamaClient(ts.URL, "qwen3:0.6b", "test-embed")
    out, err := c.Chat(context.Background(), "Say hello")
    if err != nil {
        t.Fatalf("Chat() unexpected error: %v", err)
    }
    if out != "Hello, world" {
        t.Fatalf("unexpected chat output: %q", out)
    }
}

func TestChat_ErrorStatus(t *testing.T) {
    ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        http.Error(w, "fail", http.StatusInternalServerError)
    }))
    defer ts.Close()

    c := NewOllamaClient(ts.URL, "qwen3:0.6b", "test-embed")
    _, err := c.Chat(context.Background(), "x")
    if err == nil {
        t.Fatalf("expected error on non-200 status")
    }
    if !strings.Contains(err.Error(), "status 500") || !strings.Contains(err.Error(), "fail") {
        t.Fatalf("error should include status and body, got: %v", err)
    }
}

func TestChat_BadJSONChunk(t *testing.T) {
    ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Header().Set("Content-Type", "application/json")
        // Write an invalid JSON chunk to trigger decoder error
        w.Write([]byte("{invalid json}"))
    }))
    defer ts.Close()

    c := NewOllamaClient(ts.URL, "qwen3:0.6b", "test-embed")
    if _, err := c.Chat(context.Background(), "x"); err == nil {
        t.Fatalf("expected error on malformed json stream")
    }
}
