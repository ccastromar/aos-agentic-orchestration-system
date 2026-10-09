package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/app"
)

func TestE2EReAct(t *testing.T) {
	chdirToRepoRoot(t)

	var reactCalls int
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)

		bodyBytes, _ := io.ReadAll(r.Body)
		var request struct {
			Tools json.RawMessage `json:"tools"`
		}
		_ = json.Unmarshal(bodyBytes, &request)

		if r.URL.Path == "/api/embeddings" {
			_ = enc.Encode(map[string]any{
				"embedding": []float32{0.1, 0.2, 0.3},
			})
			return
		}

		if len(request.Tools) == 0 {
			// Intent Detection
			_ = enc.Encode(map[string]any{
				"message": map[string]any{
					"role":    "assistant",
					"content": "```json\n{\n  \"intent\": \"intent_react_demo\",\n  \"confidence\": 0.9,\n  \"language\": \"es\",\n  \"parameters\": {\"accountId\": \"555\"},\n  \"errors\": []\n}\n```",
				},
			})
			return
		}

		reactCalls++
		if reactCalls == 1 {
			// Ollama-native function call: check risk.
			_ = enc.Encode(map[string]any{
				"message": map[string]any{
					"role": "assistant",
					"tool_calls": []any{map[string]any{"function": map[string]any{
						"name": "banking.aml_risk_check", "arguments": map[string]any{"userId": "555"},
					}}},
				},
			})
		} else if reactCalls == 2 {
			// Ollama-native function call: get balance.
			_ = enc.Encode(map[string]any{
				"message": map[string]any{
					"role": "assistant",
					"tool_calls": []any{map[string]any{"function": map[string]any{
						"name": "banking.core_get_balance", "arguments": map[string]any{"accountId": "555"},
					}}},
				},
			})
		} else {
			// Ollama returns plain assistant content when the agent is done.
			_ = enc.Encode(map[string]any{
				"message": map[string]any{
					"role":    "assistant",
					"content": "Balance is 1000",
				},
			})
		}
	}))
	defer ollama.Close()

	// Mock target services on an ephemeral port and point the loaded tool
	// definitions at it. Fixed ports can collide with a developer's local mocks.
	targetMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/mock/aml/check" {
			w.Write([]byte(`{"risk_score": 10, "status": "APPROVED"}`))
		} else if r.URL.Path == "/mock/core/balance" {
			w.Write([]byte(`{"balance": 1000, "currency": "EUR"}`))
		}
	}))
	defer targetMock.Close()

	t.Setenv("OLLAMA_BASE_URL", ollama.URL)
	t.Setenv("OLLAMA_MODEL", "test-model")
	t.Setenv("LLM_ENGINE", "ollama")
	t.Setenv("AOS_MOCK_BASE_URL", targetMock.URL)

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
