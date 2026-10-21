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

// mcpFake is a minimal MCP server: it assigns a session on initialize, requires it afterwards,
// lists one tool with an integer parameter, and answers as JSON or, with sse, as an SSE stream.
// Without init it is a stateless server that does not implement initialize.
type mcpFake struct {
	sse, init bool
	call      func(name string, args map[string]any) map[string]any
	gotArgs   map[string]any
}

func (f *mcpFake) server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if f.init && req.Method != "initialize" && r.Header.Get("Mcp-Session-Id") != "s-1" {
			http.Error(w, "missing session", http.StatusBadRequest)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			if !f.init {
				writeRPC(w, f.sse, req.ID, nil, map[string]any{"code": -32601, "message": "method not found"})
				return
			}
			w.Header().Set("Mcp-Session-Id", "s-1")
			result = map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}}
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
			return
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "lookup", "inputSchema": map[string]any{
				"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer"}, "active": map[string]any{"type": "boolean"}}}}}}
		case "tools/call":
			f.gotArgs, _ = req.Params["arguments"].(map[string]any)
			name, _ := req.Params["name"].(string)
			result = f.call(name, f.gotArgs)
		}
		writeRPC(w, f.sse, req.ID, result, nil)
	}))
}

func writeRPC(w http.ResponseWriter, sse bool, id, result, rpcErr any) {
	body := map[string]any{"jsonrpc": "2.0", "id": id}
	if rpcErr != nil {
		body["error"] = rpcErr
	} else {
		body["result"] = result
	}
	data, _ := json.Marshal(body)
	if sse {
		w.Header().Set("Content-Type", "text/event-stream")
		// A progress notification first: the client must skip it and wait for its answer.
		_, _ = w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{}}\n\n"))
		_, _ = w.Write([]byte("event: message\ndata: " + string(data) + "\n\n"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func okText(text string) func(string, map[string]any) map[string]any {
	return func(string, map[string]any) map[string]any {
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}
	}
}

func TestMCPToolCall(t *testing.T) {
	for _, sse := range []bool{false, true} {
		f := &mcpFake{sse: sse, init: true, call: okText("ok")}
		s := f.server(t)
		out, err := ExecuteProtocolTool(context.Background(),
			config.Tool{Type: "mcp", Target: s.URL, Name: "crm.lookup", RemoteTool: "lookup"},
			map[string]string{"id": "42", "limit": "3", "active": "true"})
		s.Close()
		if err != nil || out["protocol"] != "mcp" || out["ok"] != true {
			t.Fatalf("sse=%v out=%v err=%v", sse, out, err)
		}
		// Typed per the tool's input schema; JSON numbers decode as float64.
		if f.gotArgs["id"] != "42" || f.gotArgs["limit"] != float64(3) || f.gotArgs["active"] != true {
			t.Fatalf("sse=%v args=%#v", sse, f.gotArgs)
		}
	}
}

func TestMCPWithoutInitializeStillWorks(t *testing.T) {
	f := &mcpFake{init: false, call: okText("ok")}
	s := f.server(t)
	defer s.Close()
	if _, err := ExecuteProtocolTool(context.Background(), config.Tool{Type: "mcp", Target: s.URL, Name: "lookup"}, nil); err != nil {
		t.Fatalf("a server without initialize must still be callable: %v", err)
	}
}

func TestMCPToolErrorIsNotSuccess(t *testing.T) {
	f := &mcpFake{init: true, call: func(string, map[string]any) map[string]any {
		return map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": "customer 42 not found"}}}
	}}
	s := f.server(t)
	defer s.Close()
	_, err := ExecuteProtocolTool(context.Background(), config.Tool{Type: "mcp", Target: s.URL, Name: "lookup"}, nil)
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
