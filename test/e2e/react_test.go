package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ccastromar/aos-agent-orchestration-system/internal/app"
)

func TestE2EReAct(t *testing.T) {
	chdirToRepoRoot(t)

	var chatCalls int
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		
		bodyBytes, _ := io.ReadAll(r.Body)
		fmt.Printf("OLLAMA MOCK REQ path=%s body=%s\n", r.URL.Path, string(bodyBytes))
		
		if r.URL.Path == "/api/embeddings" {
			_ = enc.Encode(map[string]any{
				"embedding": []float32{0.1, 0.2, 0.3},
			})
			return
		}
		
		chatCalls++
		if chatCalls == 1 {
			// Intent Detection
			_ = enc.Encode(map[string]any{
				"message": map[string]any{
					"role": "assistant",
					"content": "```json\n{\n  \"intent\": \"intent_react_demo\",\n  \"confidence\": 0.9,\n  \"language\": \"es\",\n  \"parameters\": {\"accountId\": \"555\"},\n  \"errors\": []\n}\n```",
				},
			})
		} else if chatCalls == 2 {
			// ReAct Step 1: Call check risk
			_ = enc.Encode(map[string]any{
				"message": map[string]any{
					"role": "assistant",
					"content": `{"thought": "I need to check the risk first.", "action": "banking.aml_risk_check", "action_input": "{\"userId\":\"555\"}"}`,
				},
			})
		} else if chatCalls == 3 {
			// ReAct Step 2: Call core get balance
			_ = enc.Encode(map[string]any{
				"message": map[string]any{
					"role": "assistant",
					"content": `{"thought": "Risk is low. Now get balance.", "action": "banking.core_get_balance", "action_input": "{\"accountId\":\"555\"}"}`,
				},
			})
		} else {
			// ReAct Step 3: FINISH
			_ = enc.Encode(map[string]any{
				"message": map[string]any{
					"role": "assistant",
					"content": `{"thought": "I have the balance.", "action": "FINISH", "action_input": "Balance is 1000"}`,
				},
			})
		}
	}))
	defer ollama.Close()

	// Mock target services
	listener, err := net.Listen("tcp", "127.0.0.1:19001")
	if err != nil {
		t.Fatalf("Failed to listen on port 19001: %v", err)
	}
	targetMock := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/mock/aml/check" {
			w.Write([]byte(`{"risk_score": 10, "status": "APPROVED"}`))
		} else if r.URL.Path == "/mock/core/balance" {
			w.Write([]byte(`{"balance": 1000, "currency": "EUR"}`))
		}
	}))
	targetMock.Listener.Close()
	targetMock.Listener = listener
	targetMock.Start()
	defer targetMock.Close()

	t.Setenv("OLLAMA_BASE_URL", ollama.URL)
	t.Setenv("OLLAMA_MODEL", "test-model")
	t.Setenv("LLM_ENGINE", "ollama")

	a, err := app.New()
	if err != nil {
		t.Fatalf("failed to init app: %v", err)
	}

	stopAgents := a.StartAgents(context.Background())
	defer stopAgents()
	
	ts := httptest.NewServer(a.Handler())
	defer ts.Close()

	reqBody := map[string]any{
		"message": "Check balance for account 555",
		"lang":    "en",
	}
	b, _ := json.Marshal(reqBody)
	resp, err := http.Post(ts.URL+"/ask", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("post /ask err: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 202 {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}

	var res map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&res)
	id := res["id"].(string)

	var finalRes map[string]any
	for i := 0; i < 20; i++ {
		respTask, _ := http.Get(ts.URL + "/task?id=" + id)
		_ = json.NewDecoder(respTask.Body).Decode(&finalRes)
		respTask.Body.Close()

		if finalRes["status"] == "completed" || finalRes["status"] == "error" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if finalRes["status"] != "completed" {
		t.Fatalf("task did not complete: %#v", finalRes)
	}

	data := finalRes["data"].(map[string]any)
	raw := data["raw"].(map[string]any)
	reactAnswer := raw["final_answer"].(string)
	if reactAnswer != "Balance is 1000" {
		t.Fatalf("expected Balance is 1000, got %s", reactAnswer)
	}
}
