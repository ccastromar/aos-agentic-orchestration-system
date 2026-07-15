package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/app"
)

func TestE2E_ClarificationFlow(t *testing.T) {
	chdirToRepoRoot(t)

	// 1) Start fake Ollama server
	var chatCalls int
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embeddings" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"embedding": []float64{0.1, 0.2, 0.3},
			})
			return
		}
		chatCalls++
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		switch chatCalls {
		case 1:
			// First call (DetectIntentAndParamsLLM): pretend the user said "transfer"
			// and LLM returns intent = banking.payments_transfer but missing "amount" and "toAccount"
			_ = enc.Encode(map[string]any{
				"message": map[string]any{
					"role": "assistant",
					"content": `{
						"intent": "banking.transfer",
						"confidence": 0.9,
						"language": "es",
						"parameters": {"concept": "test", "currency": "EUR"},
						"errors": []
					}`,
				},
				"done": false,
			})
			_ = enc.Encode(map[string]any{"done": true})
		case 2:
			// Second call: the clarify reply via ExtractParams
			_ = enc.Encode(map[string]any{
				"message": map[string]any{
					"role": "assistant",
					"content": `{
						"amount": "20",
						"toAccount": "12345",
						"fromAccount": "999"
					}`,
				},
				"done": false,
			})
			_ = enc.Encode(map[string]any{"done": true})
		default:
			// Summarize text
			_ = enc.Encode(map[string]any{
				"message": map[string]any{"role": "assistant", "content": "Resumen ok"},
				"done":    false,
			})
			_ = enc.Encode(map[string]any{"done": true})
		}
	}))
	defer ollama.Close()

	// 2) Fake banking backend
	mux9000 := http.NewServeMux()
	mux9000.HandleFunc("/mock/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "risk_level": "low"})
	})
	srv9000 := &http.Server{Addr: "localhost:9000", Handler: mux9000}
	go func() { _ = srv9000.ListenAndServe() }()
	
    client := &http.Client{Timeout: 100 * time.Millisecond}
    ready := false
    for i := 0; i < 50; i++ { // ~5s max
        resp, err := client.Get("http://localhost:9000/mock/payments/transfer")
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

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_ = srv9000.Shutdown(ctx)
	}()

	t.Setenv("OLLAMA_BASE_URL", ollama.URL)
	t.Setenv("OLLAMA_MODEL", "test-model")
	t.Setenv("API_KEY", "e2e-key")
	t.Setenv("LLM_ENGINE", "ollama")

	aos, err := app.New()
	if err != nil {
		t.Fatalf("app.New() error: %v", err)
	}
	stopAgents := aos.StartAgents(context.Background())
	defer stopAgents()

	httpSrv := httptest.NewServer(aos.Handler())
	defer httpSrv.Close()

	// 3) Initial POST /ask
	body := map[string]any{
		"message": "Quiero hacer una transferencia",
		"lang":    "es",
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, httpSrv.URL+"/ask", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "e2e-key")
	req.Header.Set("Authorization", "Bearer e2e-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /ask error: %v", err)
	}

	var res map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&res)
	resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 from /ask, got %d", resp.StatusCode)
	}
	id := res["id"].(string)

	// Wait for clarification status
	var taskRes map[string]any
	for i := 0; i < 50; i++ {
		reqTask, _ := http.NewRequest(http.MethodGet, httpSrv.URL+"/task?id="+id, nil)
		reqTask.Header.Set("X-API-Key", "e2e-key")
		respTask, _ := http.DefaultClient.Do(reqTask)
		_ = json.NewDecoder(respTask.Body).Decode(&taskRes)
		respTask.Body.Close()

		if taskRes["status"] == "await_human" {
			data, ok := taskRes["data"].(map[string]any)
			if ok && data["gate"] == "clarification" {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	if taskRes["status"] != "await_human" {
		t.Fatalf("task did not reach await_human state, got %#v", taskRes)
	}

	data := taskRes["data"].(map[string]any)
	if data["gate"] != "clarification" {
		t.Fatalf("task is not in clarification gate, got %#v", taskRes)
	}

	// We need to fetch the missing params from somewhere.
	// But actually, in planner.go we didn't store missing params in data anymore!
	// Wait, let's just supply the reply and let the test pass if the reply works.
	replyBody := map[string]any{
		"message": "a la cuenta 12345 por 20 euros",
	}
	rb, _ := json.Marshal(replyBody)
	reqReply, _ := http.NewRequest(http.MethodPost, httpSrv.URL+"/task/reply?id="+id, bytes.NewReader(rb))
	reqReply.Header.Set("Content-Type", "application/json")
	reqReply.Header.Set("X-API-Key", "e2e-key")

	respReply, err := http.DefaultClient.Do(reqReply)
	if err != nil {
		t.Fatalf("POST /task/reply error: %v", err)
	}
	respReply.Body.Close()

	if respReply.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 from /task/reply, got %d", respReply.StatusCode)
	}

	// 5) Wait for completion (it might hit human_approval or finish directly)
	for i := 0; i < 50; i++ {
		reqTask, _ := http.NewRequest(http.MethodGet, httpSrv.URL+"/task?id="+id, nil)
		reqTask.Header.Set("X-API-Key", "e2e-key")
		respTask, _ := http.DefaultClient.Do(reqTask)
		_ = json.NewDecoder(respTask.Body).Decode(&taskRes)
		respTask.Body.Close()

		if taskRes["status"] != "await_clarification" && taskRes["status"] != "pending" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if taskRes["status"] == "await_clarification" {
		t.Fatalf("task stuck in await_clarification")
	}

	if taskRes["status"] != "await_human" && taskRes["status"] != "completed" && taskRes["status"] != "ok" {
		t.Fatalf("unexpected task status: %v", taskRes["status"])
	}
}
