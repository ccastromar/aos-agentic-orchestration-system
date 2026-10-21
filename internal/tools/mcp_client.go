package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// mcpProtocolVersion is the MCP revision AOS asks for in initialize; the server answers with
// the one it will speak, which AOS then sends on every request (MCP-Protocol-Version).
const mcpProtocolVersion = "2025-11-25"

// maxMCPResponse bounds what AOS reads from one MCP response (JSON or SSE stream).
const maxMCPResponse = 8 << 20

// mcpClient speaks MCP's Streamable HTTP transport for one step: initialize, then requests on
// the session the server assigned, each answered either as JSON or as an SSE stream.
type mcpClient struct {
	url     string
	headers map[string]string
	session string
	version string
	nextID  int
}

type rpcReply struct {
	ID     json.RawMessage `json:"id"`
	Result map[string]any  `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type rpcError struct {
	Code    int
	Message string
}

func (e *rpcError) Error() string { return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message) }

// request sends a JSON-RPC request and returns its result. With notify set it sends a
// notification (no id) and expects no answer.
func (c *mcpClient) request(ctx context.Context, method string, params any, notify bool) (map[string]any, error) {
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		msg["params"] = params
	}
	id := ""
	if !notify {
		c.nextID++
		id = strconv.Itoa(c.nextID)
		msg["id"] = c.nextID
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
	}
	if c.version != "" {
		req.Header.Set("MCP-Protocol-Version", c.version)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	resp, err := safeClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if s := resp.Header.Get("Mcp-Session-Id"); s != "" {
		c.session = s
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if notify {
		return nil, nil
	}
	body := io.LimitReader(resp.Body, maxMCPResponse)
	var reply *rpcReply
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		reply, err = readSSEReply(body, id)
	} else {
		reply = &rpcReply{}
		err = json.NewDecoder(body).Decode(reply)
	}
	if err != nil {
		return nil, err
	}
	if reply.Error != nil {
		return nil, &rpcError{Code: reply.Error.Code, Message: reply.Error.Message}
	}
	if reply.Result == nil {
		return nil, fmt.Errorf("JSON-RPC response has no result")
	}
	return reply.Result, nil
}

// readSSEReply reads an SSE stream until the JSON-RPC response with the given id arrives;
// server notifications and requests on the same stream are skipped.
func readSSEReply(r io.Reader, id string) (*rpcReply, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), maxMCPResponse)
	var data strings.Builder
	flush := func() (*rpcReply, error) {
		defer data.Reset()
		if data.Len() == 0 {
			return nil, nil
		}
		var reply rpcReply
		if err := json.Unmarshal([]byte(data.String()), &reply); err != nil {
			return nil, nil // not a JSON-RPC message for us
		}
		if strings.Trim(string(reply.ID), `"`) == id && (reply.Result != nil || reply.Error != nil) {
			return &reply, nil
		}
		return nil, nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			if reply, err := flush(); reply != nil || err != nil {
				return reply, err
			}
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if reply, err := flush(); reply != nil || err != nil {
		return reply, err
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("SSE stream ended without the response to request %s", id)
}

// initialize opens the session. A server that does not implement it (a minimal, stateless
// one) is used without a session.
func (c *mcpClient) initialize(ctx context.Context) error {
	result, err := c.request(ctx, "initialize", map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "aos", "version": "1"},
	}, false)
	if err != nil {
		var rpcErr *rpcError
		if errors.As(err, &rpcErr) && rpcErr.Code == -32601 {
			return nil
		}
		return fmt.Errorf("initialize: %w", err)
	}
	if v, _ := result["protocolVersion"].(string); v != "" {
		c.version = v
	}
	if _, err := c.request(ctx, "notifications/initialized", nil, true); err != nil {
		return fmt.Errorf("notifications/initialized: %w", err)
	}
	return nil
}

// close ends the session (best effort): servers keep per-session state until then.
func (c *mcpClient) close() {
	if c.session == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.url, nil)
	if err != nil {
		return
	}
	req.Header.Set("Mcp-Session-Id", c.session)
	if c.version != "" {
		req.Header.Set("MCP-Protocol-Version", c.version)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if resp, err := safeClient.Do(req); err == nil {
		resp.Body.Close()
	}
}

// inputSchema returns the named tool's input schema from tools/list (following pagination),
// or nil when the server does not list it.
func (c *mcpClient) inputSchema(ctx context.Context, tool string) (map[string]any, error) {
	cursor := ""
	for page := 0; page < 50; page++ {
		var params map[string]any
		if cursor != "" {
			params = map[string]any{"cursor": cursor}
		}
		result, err := c.request(ctx, "tools/list", params, false)
		if err != nil {
			return nil, err
		}
		list, _ := result["tools"].([]any)
		for _, item := range list {
			if m, ok := item.(map[string]any); ok && m["name"] == tool {
				schema, _ := m["inputSchema"].(map[string]any)
				return schema, nil
			}
		}
		next, _ := result["nextCursor"].(string)
		if next == "" {
			return nil, nil
		}
		cursor = next
	}
	return nil, nil
}

// typedArguments turns AOS's string parameters into the JSON types the tool's input schema
// declares (integer, number, boolean, object, array). A value that does not parse stays a
// string, so the server's own validation reports it.
func typedArguments(params map[string]string, schema map[string]any) map[string]any {
	props, _ := schema["properties"].(map[string]any)
	args := make(map[string]any, len(params))
	for k, v := range params {
		args[k] = v
		prop, _ := props[k].(map[string]any)
		switch prop["type"] {
		case "integer":
			if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				args[k] = n
			}
		case "number":
			if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				args[k] = f
			}
		case "boolean":
			if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
				args[k] = b
			}
		case "object", "array":
			var j any
			if err := json.Unmarshal([]byte(v), &j); err == nil {
				args[k] = j
			}
		}
	}
	return args
}
