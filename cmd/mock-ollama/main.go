// Command mock-ollama serves a deterministic Ollama-compatible API for live demos of AOS
// without a real model. Point AOS at it with LLM_ENGINE=ollama and OLLAMA_BASE_URL, or with
// LLM_ENGINE=mock, LLM_BASE_URL=http://<host>:<port>/v1 and any non-empty LLM_API_KEY.
//
//	MOCK_OLLAMA_PORT            listen port (default 11435, next to a real Ollama on 11434)
//	MOCK_OLLAMA_MODEL           advertised model name (default aos-demo:deterministic)
//	MOCK_OLLAMA_TOKEN_DELAY_MS  delay between streamed chunks (default 25)
//	MOCK_OLLAMA_SLOW_MS         delay injected by "#mock-fail:slow" (default 8000)
//	MOCK_OLLAMA_SCENARIOS       demo script YAML (default: the embedded mocks/ollama/scenarios.yaml)
package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/mocks/ollama"
)

func main() {
	port := envOr("MOCK_OLLAMA_PORT", "11435")
	scenarios, err := ollama.LoadScenarios(os.Getenv("MOCK_OLLAMA_SCENARIOS"))
	if err != nil {
		log.Fatalf("[MOCK OLLAMA] %v", err)
	}
	srv := ollama.NewServer(ollama.Config{
		Model:      os.Getenv("MOCK_OLLAMA_MODEL"),
		TokenDelay: millis("MOCK_OLLAMA_TOKEN_DELAY_MS", 25),
		SlowDelay:  millis("MOCK_OLLAMA_SLOW_MS", 8000),
		Scenarios:  scenarios,
	})
	mux := http.NewServeMux()
	srv.RegisterHandlers(mux)
	log.Printf("[MOCK OLLAMA] deterministic mock (no real model) listening on :%s, %d scenario(s)", port, len(scenarios))
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("[MOCK OLLAMA] %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func millis(key string, fallback int) time.Duration {
	if raw := os.Getenv(key); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			return time.Duration(n) * time.Millisecond
		}
	}
	return time.Duration(fallback) * time.Millisecond
}
