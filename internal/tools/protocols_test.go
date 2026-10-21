package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
)

// withSSRF runs fn with the SSRF check on (the package tests run with it off, against local
// test servers), then restores the previous settings.
func withSSRF(t *testing.T, allowed []string, fn func()) {
	t.Helper()
	prevSkip, prevHosts := SkipSSRF, internalHosts
	SkipSSRF = false
	SetInternalHosts(allowed)
	defer func() { SkipSSRF, internalHosts = prevSkip, prevHosts }()
	fn()
}

func rpcServer(t *testing.T, handle func(method string, params map[string]any) (any, any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		result, rpcErr := handle(req.Method, req.Params)
		body := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rpcErr != nil {
			body["error"] = rpcErr
		} else {
			body["result"] = result
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
}

func TestMCPToolCall(t *testing.T) {
	s := rpcServer(t, func(method string, params map[string]any) (any, any) {
		if method != "tools/call" || params["name"] != "crm.lookup" {
			t.Fatalf("method=%v params=%v", method, params)
		}
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}}, nil
	})
	defer s.Close()
	out, err := ExecuteProtocolTool(context.Background(), config.Tool{Type: "mcp", Target: s.URL, Name: "crm.lookup"}, map[string]string{"id": "42"})
	if err != nil || out["protocol"] != "mcp" || out["ok"] != true {
		t.Fatalf("out=%v err=%v", out, err)
	}
}

func TestMCPToolErrorIsNotSuccess(t *testing.T) {
	s := rpcServer(t, func(string, map[string]any) (any, any) {
		return map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": "customer 42 not found"}}}, nil
	})
	defer s.Close()
	_, err := ExecuteProtocolTool(context.Background(), config.Tool{Type: "mcp", Target: s.URL, Name: "crm.lookup"}, nil)
	if err == nil || !strings.Contains(err.Error(), "customer 42 not found") {
		t.Fatalf("want the tool's error, got %v", err)
	}
}

func TestProtocolToolsKeepTheSSRFCheck(t *testing.T) {
	s := rpcServer(t, func(string, map[string]any) (any, any) { return map[string]any{}, nil })
	defer s.Close()
	addr := strings.TrimPrefix(s.URL, "http://")
	withSSRF(t, nil, func() {
		for _, tool := range []config.Tool{
			{Type: "mcp", Target: s.URL, Name: "x"},
			{Type: "a2a", Target: s.URL},
			{Type: "grpc", Target: addr, Method: "/svc/M", Insecure: true, TimeoutMs: 2000},
		} {
			_, err := ExecuteProtocolTool(context.Background(), tool, map[string]string{"message": "hi"})
			if err == nil || !strings.Contains(err.Error(), "ssrf prevention") {
				t.Fatalf("%s to a loopback address must be refused, got %v", tool.Type, err)
			}
		}
	})
}

func TestProtocolToolsHonourTheInternalHostsAllowlist(t *testing.T) {
	s := rpcServer(t, func(string, map[string]any) (any, any) { return map[string]any{"content": []any{}}, nil })
	defer s.Close()
	withSSRF(t, []string{"127.0.0.1"}, func() {
		if _, err := ExecuteProtocolTool(context.Background(), config.Tool{Type: "mcp", Target: s.URL, Name: "x"}, nil); err != nil {
			t.Fatalf("an allowlisted internal host must be reachable: %v", err)
		}
	})
}

func TestA2ASendsAFullMessageAndPollsWithTasksGet(t *testing.T) {
	polls := 0
	s := rpcServer(t, func(method string, params map[string]any) (any, any) {
		switch method {
		case "message/send":
			msg, _ := params["message"].(map[string]any)
			if msg["messageId"] == nil || msg["kind"] != "message" || msg["role"] != "user" {
				t.Fatalf("incomplete message: %v", msg)
			}
			return map[string]any{"kind": "task", "id": "t-1", "status": map[string]any{"state": "submitted"}}, nil
		case "tasks/get":
			if params["id"] != "t-1" {
				t.Fatalf("tasks/get id=%v", params["id"])
			}
			polls++
			state := "working"
			if polls >= 2 {
				state = "completed"
			}
			return map[string]any{"kind": "task", "id": "t-1", "status": map[string]any{"state": state}}, nil
		}
		return nil, map[string]any{"code": -32601, "message": "method not found"}
	})
	defer s.Close()
	out, err := ExecuteProtocolTool(context.Background(), config.Tool{Type: "a2a", Target: s.URL, TimeoutMs: 5000}, map[string]string{"message": "review"})
	if err != nil || out["protocol"] != "a2a" {
		t.Fatalf("out=%v err=%v", out, err)
	}
	if state := out["result"].(map[string]any)["status"].(map[string]any)["state"]; state != "completed" || polls != 2 {
		t.Fatalf("state=%v polls=%d", state, polls)
	}
}

func TestA2ATasksThatCannotFinishInAStepAreErrors(t *testing.T) {
	for _, state := range []string{"input-required", "auth-required", "failed", "canceled", "rejected"} {
		s := rpcServer(t, func(string, map[string]any) (any, any) {
			return map[string]any{"kind": "task", "id": "t-1", "status": map[string]any{"state": state}}, nil
		})
		_, err := ExecuteProtocolTool(context.Background(), config.Tool{Type: "a2a", Target: s.URL}, map[string]string{"message": "review"})
		s.Close()
		if err == nil {
			t.Fatalf("state %s must be an error", state)
		}
	}
}

func TestA2ADirectMessageReplyIsSuccess(t *testing.T) {
	s := rpcServer(t, func(string, map[string]any) (any, any) {
		return map[string]any{"kind": "message", "messageId": "m-2", "role": "agent", "parts": []any{}}, nil
	})
	defer s.Close()
	out, err := ExecuteProtocolTool(context.Background(), config.Tool{Type: "a2a", Target: s.URL}, map[string]string{"message": "hi"})
	if err != nil || out["ok"] != true {
		t.Fatalf("out=%v err=%v", out, err)
	}
}
