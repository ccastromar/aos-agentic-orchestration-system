package llm

import (
	"context"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"os"
	"testing"
	"time"
)

// Opt-in integration check against the configured remote Ollama instance.
func TestLiveOllamaMemoryIntent(t *testing.T) {
	url := os.Getenv("AOS_TEST_OLLAMA_URL")
	if url == "" {
		t.Skip("set AOS_TEST_OLLAMA_URL to run against Ollama")
	}
	model := os.Getenv("AOS_TEST_OLLAMA_MODEL")
	if model == "" {
		t.Fatal("AOS_TEST_OLLAMA_MODEL required")
	}
	cfg, err := config.LoadFromDir("../../definitions")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Intents["memory.add"]; !ok {
		t.Fatal("memory.add was not loaded")
	}
	t.Logf("loaded %d intents", len(cfg.Intents))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	out, err := DetectIntentAndParams(ctx, NewOllamaClient(url, model, "nomic-embed-text"), "Remember that my dog's name is Rex", cfg.Intents, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("model=%s intent=%s params=%v raw=%s", model, out.Type, out.Params, out.Raw)
	if out.Type != "memory.add" || out.Params["fact"] == "" {
		t.Fatalf("expected memory.add with fact, got %+v", out)
	}
}

func TestLiveOllamaEmbedding(t *testing.T) {
	url := os.Getenv("AOS_TEST_OLLAMA_URL")
	if url == "" {
		t.Skip("set AOS_TEST_OLLAMA_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	started := time.Now()
	embedding, err := NewOllamaClient(url, "qwen2.5:3b", "nomic-embed-text").Embed(ctx, "My dog is called Rex")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("embedding dimensions=%d duration=%s", len(embedding), time.Since(started))
	if len(embedding) != 768 {
		t.Fatalf("unexpected embedding dimensions: %d", len(embedding))
	}
}
