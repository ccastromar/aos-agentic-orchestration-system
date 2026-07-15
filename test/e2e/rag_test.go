package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/app"
)

func TestE2ERAG(t *testing.T) {
	chdirToRepoRoot(t)

	var chatCalls int
	var capturedPrompt string
	var mu sync.Mutex

	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/api/embeddings") {
			// Mock Embeddings endpoint
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"embedding": []float32{0.1, 0.2, 0.3},
			})
			return
		}

		if strings.Contains(r.URL.Path, "/api/chat") {
			mu.Lock()
			chatCalls++
			mu.Unlock()
			
			var reqBody struct {
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			json.NewDecoder(r.Body).Decode(&reqBody)
			prompt := ""
			if len(reqBody.Messages) > 0 {
				prompt = reqBody.Messages[0].Content
				if strings.Contains(prompt, "NLU module") {
					mu.Lock()
					capturedPrompt = prompt
					mu.Unlock()
				}
			}

			w.Header().Set("Content-Type", "application/json")
			enc := json.NewEncoder(w)

			if strings.Contains(prompt, "NLU module") {
				if strings.Contains(prompt, "LONG-TERM KNOWLEDGE") {
					// Session 2 detect intent
					_ = enc.Encode(map[string]any{
						"message": map[string]any{
							"role": "assistant",
							"content": "```json\n{\n  \"intent\": \"intent_rag_test\",\n  \"confidence\": 0.9,\n  \"language\": \"es\",\n  \"parameters\": {\"userId\":\"123\"},\n  \"errors\": []\n}\n```",
						},
					})
				} else {
					// Session 1 detect intent
					_ = enc.Encode(map[string]any{
						"message": map[string]any{
							"role": "assistant",
							"content": "```json\n{\n  \"intent\": \"intent_rag_test\",\n  \"confidence\": 0.9,\n  \"language\": \"es\",\n  \"parameters\": {\"userId\":\"123\"},\n  \"errors\": []\n}\n```",
						},
					})
				}
			} else {
				// Analyst call
				_ = enc.Encode(map[string]any{
					"message": map[string]any{
						"role": "assistant",
						"content": "El usuario indico que su color favorito es el rojo.",
					},
				})
			}
		}
	}))
	defer ollama.Close()

	t.Setenv("OLLAMA_BASE_URL", ollama.URL)
	t.Setenv("LLM_ENGINE", "ollama")
	t.Setenv("QDRANT_URL", "memory") // Force MemoryVectorStore
	
	application, err := app.New()
	if err != nil {
		t.Fatalf("Failed to initialize app: %v", err)
	}

	stopAgents := application.StartAgents(context.Background())
	defer stopAgents()
	
	ts := httptest.NewServer(application.Handler())
	defer ts.Close()

	// Session 1: User says something that will be stored in RAG
	sessionID := "rag-session-123"
	
	req1 := map[string]any{
		"message":    "Mi color favorito es el rojo y mi usuario es 123",
		"session_id": sessionID,
	}
	b1, _ := json.Marshal(req1)
	resp1, err := http.Post(ts.URL+"/ask", "application/json", bytes.NewReader(b1))
	if err != nil {
		t.Fatalf("Ask 1 failed: %v", err)
	}
	defer resp1.Body.Close()
	
	var askRes1 map[string]any
	json.NewDecoder(resp1.Body).Decode(&askRes1)
	taskID1 := askRes1["id"].(string)

	// wait for task 1 to complete and analyst to save summary
	for i := 0; i < 20; i++ {
		r, _ := http.Get(ts.URL + "/task?id=" + taskID1)
		var tRes map[string]any
		json.NewDecoder(r.Body).Decode(&tRes)
		r.Body.Close()
		if tRes["status"] == "completed" {
			break
		}
		if tRes["status"] == "error" {
			t.Fatalf("task 1 failed: %v", tRes)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Give the Analyst (async) time to process the summarize event and save the Vector Memory
	time.Sleep(500 * time.Millisecond)

	// Session 2: User asks something implicitly, Vector Store should inject the memory!
	// We use the same session_id to retrieve the memory, though in RAG the session_id filter is optional.
	// We will just verify that the captured prompt for chatCalls == 3 contains the LONG-TERM KNOWLEDGE.
	
	req2 := map[string]any{
		"message":    "De que color pinto la casa?",
		"session_id": sessionID,
	}
	b2, _ := json.Marshal(req2)
	resp2, err := http.Post(ts.URL+"/ask", "application/json", bytes.NewReader(b2))
	if err != nil {
		t.Fatalf("Ask 2 failed: %v", err)
	}
	defer resp2.Body.Close()

	time.Sleep(500 * time.Millisecond) // let the planner run

	mu.Lock()
	p := capturedPrompt
	mu.Unlock()

	if !strings.Contains(p, "LONG-TERM KNOWLEDGE") {
		t.Errorf("Expected RAG to inject long-term knowledge, but prompt was: %s", p)
	}
	
	if !strings.Contains(p, "El usuario indico que su color favorito es el rojo") {
		t.Errorf("Expected RAG to contain the summary from session 1, but prompt was: %s", p)
	}
}
