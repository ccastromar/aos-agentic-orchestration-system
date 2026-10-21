package tools

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
)

// ExecuteProtocolTool adapts protocol-native calls to the same map result used
// by ordinary HTTP tools. The protocol boundary remains explicit in YAML.
// Every connection goes through the same SSRF check as HTTP tools (guardedDial).
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

func protocolURL(t config.Tool) string {
	if t.Target != "" {
		return t.Target
	}
	return t.URL
}

// jsonRPC sends one JSON-RPC 2.0 request through the SSRF-guarded client and returns its
// result object. An HTTP error, a JSON-RPC error or a missing result is an error.
func jsonRPC(ctx context.Context, url string, headers map[string]string, method string, params any) (map[string]any, error) {
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": uuid.NewString(), "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := safeClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		Result map[string]any `json:"result"`
		Error  any            `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Error != nil {
		return nil, fmt.Errorf("JSON-RPC error: %v", out.Error)
	}
	if out.Result == nil {
		return nil, fmt.Errorf("JSON-RPC response has no result")
	}
	return out.Result, nil
}

func executeMCP(ctx context.Context, t config.Tool, params map[string]string) (map[string]any, error) {
	ctx, cancel := protocolTimeout(ctx, t.TimeoutMs)
	defer cancel()
	method := t.Operation
	if method == "" {
		method = "tools/call"
	}
	args := map[string]any{}
	for k, v := range params {
		args[k] = v
	}
	result, err := jsonRPC(ctx, protocolURL(t), t.Headers, method, map[string]any{"name": t.Name, "arguments": args})
	if err != nil {
		return nil, fmt.Errorf("MCP %s: %w", t.Name, err)
	}
	// A tool that failed answers with a result, not a JSON-RPC error: isError marks it.
	if isErr, _ := result["isError"].(bool); isErr {
		return nil, fmt.Errorf("MCP tool %s failed: %s", t.Name, mcpText(result))
	}
	return map[string]any{"ok": true, "protocol": "mcp", "result": result}, nil
}

// mcpText joins the text parts of an MCP tool result.
func mcpText(result map[string]any) string {
	parts, _ := result["content"].([]any)
	var texts []string
	for _, p := range parts {
		if m, ok := p.(map[string]any); ok && m["type"] == "text" {
			if s, ok := m["text"].(string); ok {
				texts = append(texts, s)
			}
		}
	}
	if len(texts) == 0 {
		return "no details"
	}
	return strings.Join(texts, " ")
}

// executeA2A delegates to a remote agent over A2A's JSON-RPC binding: message/send, then
// tasks/get on the same endpoint while the task is submitted or working. Only a completed
// task (or a direct message reply) is a success; a failed, canceled or rejected task is an
// error, and so is one that asks for input or authorization, which a pipeline step cannot give.
func executeA2A(ctx context.Context, t config.Tool, params map[string]string) (map[string]any, error) {
	ctx, cancel := protocolTimeout(ctx, t.TimeoutMs)
	defer cancel()
	url := protocolURL(t)
	text := params["message"]
	if text == "" {
		text = fmt.Sprint(params)
	}
	message := map[string]any{
		"kind":      "message",
		"messageId": uuid.NewString(),
		"role":      "user",
		"parts":     []any{map[string]any{"kind": "text", "text": text}},
	}
	method := t.Operation
	if method == "" {
		method = "message/send"
	}
	result, err := jsonRPC(ctx, url, t.Headers, method, map[string]any{"message": message})
	if err != nil {
		return nil, fmt.Errorf("A2A: %w", err)
	}

	delay := 200 * time.Millisecond
	for {
		if result["kind"] == "message" {
			return map[string]any{"ok": true, "protocol": "a2a", "result": result}, nil
		}
		status, _ := result["status"].(map[string]any)
		state, _ := status["state"].(string)
		switch state {
		case "completed":
			return map[string]any{"ok": true, "protocol": "a2a", "result": result}, nil
		case "submitted", "working":
			id, _ := result["id"].(string)
			if id == "" {
				return nil, fmt.Errorf("A2A %s task has no id", state)
			}
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("A2A task %s still %s: %w", id, state, ctx.Err())
			case <-time.After(delay):
			}
			if delay < 2*time.Second {
				delay *= 2
			}
			if result, err = jsonRPC(ctx, url, t.Headers, "tasks/get", map[string]any{"id": id}); err != nil {
				return nil, fmt.Errorf("A2A tasks/get %s: %w", id, err)
			}
		case "input-required", "auth-required":
			return nil, fmt.Errorf("A2A agent needs %s; a pipeline step cannot provide it", strings.TrimSuffix(state, "-required"))
		case "failed", "canceled", "rejected":
			return nil, fmt.Errorf("A2A task %s", state)
		default:
			return nil, fmt.Errorf("A2A task in unknown state %q", state)
		}
	}
}

// grpcTarget keeps the configured host name all the way to the dialer, so the SSRF check
// (and its internal-hosts allowlist) sees the name, not an address gRPC already resolved.
func grpcTarget(target string) string {
	if strings.Contains(target, "://") {
		return target
	}
	return "passthrough:///" + target
}

func executeGRPC(ctx context.Context, t config.Tool, params map[string]string) (map[string]any, error) {
	ctx, cancel := protocolTimeout(ctx, t.TimeoutMs)
	defer cancel()
	if t.Target == "" || t.Method == "" {
		return nil, fmt.Errorf("grpc requires target and method")
	}
	// TLS unless the tool explicitly says the service is plaintext (a local or in-cluster one).
	creds := credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	if t.Insecure {
		creds = insecure.NewCredentials()
	}
	conn, err := grpc.NewClient(grpcTarget(t.Target),
		grpc.WithTransportCredentials(creds),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return guardedDial(ctx, "tcp", addr)
		}))
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
