package tools

import (
	"context"
	"encoding/json"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMCPToolCall(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["method"] != "tools/call" {
			t.Fatalf("method=%v", req["method"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"content": "ok"}})
	}))
	defer s.Close()
	out, err := ExecuteProtocolTool(context.Background(), config.Tool{Type: "mcp", Target: s.URL, Name: "crm.lookup"}, map[string]string{"id": "42"})
	if err != nil || out["protocol"] != "mcp" {
		t.Fatalf("out=%v err=%v", out, err)
	}
}

func TestA2AToolCall(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["method"] != "message/send" {
			t.Fatalf("method=%v", req["method"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"id": "task-1", "status": map[string]any{"state": "completed"}}})
	}))
	defer s.Close()
	out, err := ExecuteProtocolTool(context.Background(), config.Tool{Type: "a2a", Target: s.URL}, map[string]string{"message": "review"})
	if err != nil || out["protocol"] != "a2a" {
		t.Fatalf("out=%v err=%v", out, err)
	}
}

func TestA2AToolPollsWorkingTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/message:send" {
			_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"id": "t-1", "status": map[string]any{"state": "working"}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"id": "t-1", "status": map[string]any{"state": "completed"}, "artifacts": []any{"approved"}}})
	}))
	defer server.Close()
	out, err := ExecuteProtocolTool(context.Background(), config.Tool{Type: "a2a", Target: server.URL + "/message:send", TimeoutMs: 2000}, map[string]string{"message": "review"})
	if err != nil || out["protocol"] != "a2a" {
		t.Fatalf("out=%v err=%v", out, err)
	}
	result := out["result"].(map[string]any)
	if result["status"].(map[string]any)["state"] != "completed" {
		t.Fatalf("not completed: %v", out)
	}
}
