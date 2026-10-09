package mcp

import (
	"encoding/json"
	"net/http"
)

// RegisterHandlers exposes a tiny JSON-RPC MCP server backed by the existing
// CRM-shaped mock data. It is intentionally deterministic for local demos.
func RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/mock/mcp", handle)
}

func handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		JSONRPC string `json:"jsonrpc"`
		ID      any    `json:"id"`
		Method  string `json:"method"`
		Params  struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	result := map[string]any{}
	switch req.Method {
	case "tools/list":
		result["tools"] = []any{map[string]any{"name": "crm.mcp_lookup", "description": "Look up a CRM customer", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"id": map[string]string{"type": "string"}}}}}
	case "tools/call":
		id := req.Params.Arguments["id"]
		if id == nil {
			id = req.Params.Arguments["customerId"]
		}
		if id == nil {
			id = "unknown-customer"
		}
		result["content"] = []any{map[string]any{"type": "text", "text": "CRM customer lookup completed"}}
		result["structuredContent"] = map[string]any{"customerId": id, "name": "Demo Customer", "status": "active"}
	default:
		writeError(w, req.ID, -32601, "method not found")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
}

func writeError(w http.ResponseWriter, id any, code int, message string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}
