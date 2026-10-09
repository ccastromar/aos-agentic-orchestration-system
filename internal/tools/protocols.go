package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
)

// ExecuteProtocolTool adapts protocol-native calls to the same map result used
// by ordinary HTTP tools. The protocol boundary remains explicit in YAML.
func ExecuteProtocolTool(ctx context.Context, t config.Tool, params map[string]string) (map[string]any, error) {
	switch strings.ToLower(t.Type) {
	case "mcp":
		return executeMCP(ctx, t, params)
	case "a2a":
		return executeA2A(ctx, t, params)
	case "grpc":
		return executeGRPC(ctx, t, params)
	default:
		return nil, fmt.Errorf("unsupported protocol tool type %q", t.Type)
	}
}

func protocolTimeout(ctx context.Context, ms int) (context.Context, context.CancelFunc) {
	if ms <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, time.Duration(ms)*time.Millisecond)
}

func executeMCP(ctx context.Context, t config.Tool, params map[string]string) (map[string]any, error) {
	ctx, cancel := protocolTimeout(ctx, t.TimeoutMs)
	defer cancel()
	url := t.Target
	if url == "" {
		url = t.URL
	}
	method := t.Operation
	if method == "" {
		method = "tools/call"
	}
	args := map[string]any{}
	for k, v := range params {
		args[k] = v
	}
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": map[string]any{"name": t.Name, "arguments": args}}
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.Headers {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("MCP HTTP %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		Result map[string]any `json:"result"`
		Error  any            `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Error != nil {
		return nil, fmt.Errorf("MCP error: %v", out.Error)
	}
	return map[string]any{"ok": true, "protocol": "mcp", "result": out.Result}, nil
}

func executeA2A(ctx context.Context, t config.Tool, params map[string]string) (map[string]any, error) {
	ctx, cancel := protocolTimeout(ctx, t.TimeoutMs)
	defer cancel()
	url := t.Target
	if url == "" {
		url = t.URL
	}
	message := map[string]any{"role": "user", "parts": []any{map[string]any{"kind": "text", "text": params["message"]}}}
	if message["parts"].([]any)[0].(map[string]any)["text"] == "" {
		message["parts"].([]any)[0].(map[string]any)["text"] = fmt.Sprint(params)
	}
	operation := t.Operation
	if operation == "" {
		operation = "message/send"
	}
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": operation, "params": map[string]any{"message": message}}
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.Headers {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("A2A HTTP %d: %s", resp.StatusCode, string(b))
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	out["protocol"] = "a2a"
	// A2A may return a task in working state. Poll its task resource until it
	// reaches a terminal state or the step context expires.
	if result, ok := out["result"].(map[string]any); ok {
		if status, ok := result["status"].(map[string]any); ok && status["state"] == "working" {
			id, _ := result["id"].(string)
			if id == "" {
				return nil, fmt.Errorf("A2A working response has no task id")
			}
			base := strings.TrimSuffix(url, "/message:send")
			pollURL := base + "/tasks/" + id
			for {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				default:
				}
				time.Sleep(100 * time.Millisecond)
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
				if err != nil {
					return nil, err
				}
				poll, err := (&http.Client{}).Do(req)
				if err != nil {
					return nil, err
				}
				var current map[string]any
				err = json.NewDecoder(poll.Body).Decode(&current)
				poll.Body.Close()
				if err != nil {
					return nil, err
				}
				out = current
				out["protocol"] = "a2a"
				if currentResult, ok := current["result"].(map[string]any); ok {
					if currentStatus, ok := currentResult["status"].(map[string]any); ok && currentStatus["state"] == "working" {
						continue
					}
				}
				break
			}
		}
	}
	return out, nil
}

func executeGRPC(ctx context.Context, t config.Tool, params map[string]string) (map[string]any, error) {
	ctx, cancel := protocolTimeout(ctx, t.TimeoutMs)
	defer cancel()
	if t.Target == "" || t.Method == "" {
		return nil, fmt.Errorf("grpc requires target and method")
	}
	conn, err := grpc.NewClient(t.Target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	in := map[string]any{}
	for k, v := range params {
		in[k] = v
	}
	req, err := structpb.NewStruct(in)
	if err != nil {
		return nil, err
	}
	res := &structpb.Struct{}
	method := t.Method
	if t.Service != "" && !strings.HasPrefix(method, "/") {
		method = "/" + t.Service + "/" + method
	}
	if !strings.HasPrefix(method, "/") {
		method = "/" + method
	}
	if err := conn.Invoke(ctx, method, req, res); err != nil {
		return nil, fmt.Errorf("gRPC %s: %w", method, err)
	}
	return map[string]any{"ok": true, "protocol": "grpc", "result": res.AsMap()}, nil
}
