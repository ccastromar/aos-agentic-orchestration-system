package health

import (
    "context"
    "errors"
    "io"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/ccastromar/aos-agent-orchestration-system/internal/llm"
    "github.com/ccastromar/aos-agent-orchestration-system/internal/runtime"
)

type fakeLLM struct{ pingErr error }

func (f *fakeLLM) Ping(ctx context.Context) error                   { return f.pingErr }
func (f *fakeLLM) Embed(ctx context.Context, text string) ([]float32, error) { return []float32{}, nil }
func (f *fakeLLM) Chat(ctx context.Context, prompt string) (string, error) { return "", nil }

var _ llm.LLMClient = (*fakeLLM)(nil)

func TestLiveHandler_OK(t *testing.T) {
    req := httptest.NewRequest(http.MethodGet, "/live", nil)
    w := httptest.NewRecorder()

    LiveHandler(w, req)

    res := w.Result()
    if res.StatusCode != http.StatusOK {
        t.Fatalf("expected 200, got %d", res.StatusCode)
    }
    body, _ := io.ReadAll(res.Body)
    if string(body) == "" {
        t.Fatalf("expected non-empty body")
    }
}

func TestReadyHandler_SpecsNotLoaded(t *testing.T) {
    rt := &runtime.Runtime{SpecsLoaded: false, LLMClient: &fakeLLM{}}
    h := ReadyHandler(rt)

    w := httptest.NewRecorder()
    req := httptest.NewRequest(http.MethodGet, "/ready", nil)
    h(w, req)

    if w.Code != http.StatusServiceUnavailable {
        t.Fatalf("expected 503, got %d", w.Code)
    }
}

func TestReadyHandler_LLMUnreachable(t *testing.T) {
    rt := &runtime.Runtime{SpecsLoaded: true, LLMClient: &fakeLLM{pingErr: errors.New("down")}}
    h := ReadyHandler(rt)

    w := httptest.NewRecorder()
    req := httptest.NewRequest(http.MethodGet, "/ready", nil)
    h(w, req)

    if w.Code != http.StatusServiceUnavailable {
        t.Fatalf("expected 503, got %d", w.Code)
    }
}

func TestReadyHandler_OK(t *testing.T) {
    rt := &runtime.Runtime{SpecsLoaded: true, LLMClient: &fakeLLM{}}
    h := ReadyHandler(rt)

    w := httptest.NewRecorder()
    req := httptest.NewRequest(http.MethodGet, "/ready", nil)
    h(w, req)

    if w.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d", w.Code)
    }
    body, _ := io.ReadAll(w.Body)
    if string(body) == "" {
        t.Fatalf("expected non-empty body")
    }
}
