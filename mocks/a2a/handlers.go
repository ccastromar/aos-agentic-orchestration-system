package a2a

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

var (
	mu    sync.Mutex
	tasks = map[string]time.Time{}
)

// RegisterHandlers exposes a tiny A2A agent over the JSON-RPC binding: one endpoint for
// message/send (answers a working task) and tasks/get (the task completes after 250 ms).
func RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/mock/a2a", handle)
}

func handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID     any    `json:"id"`
		Method string `json:"method"`
		Params struct {
			ID      string         `json:"id"`
			Message map[string]any `json:"message"`
		} `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch req.Method {
	case "message/send":
		if req.Params.Message["messageId"] == nil {
			writeError(w, req.ID, -32602, "message.messageId is required")
			return
		}
		id := "a2a-task-" + time.Now().Format("150405.000000000")
		mu.Lock()
		tasks[id] = time.Now()
		mu.Unlock()
		writeResult(w, req.ID, task(id, "working", nil))
	case "tasks/get":
		mu.Lock()
		started, ok := tasks[req.Params.ID]
		mu.Unlock()
		if !ok {
			writeError(w, req.ID, -32001, "task not found")
			return
		}
		if time.Since(started) < 250*time.Millisecond {
			writeResult(w, req.ID, task(req.Params.ID, "working", nil))
			return
		}
		writeResult(w, req.ID, task(req.Params.ID, "completed", []any{map[string]any{
			"artifactId": "review",
			"parts":      []any{map[string]any{"kind": "text", "text": "Compliance review approved"}},
		}}))
	default:
		writeError(w, req.ID, -32601, "method not found")
	}
}

func task(id, state string, artifacts []any) map[string]any {
	t := map[string]any{"kind": "task", "id": id, "contextId": "ctx-" + id, "status": map[string]any{"state": state}}
	if artifacts != nil {
		t["artifacts"] = artifacts
	}
	return t
}

func writeResult(w http.ResponseWriter, id any, result any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeError(w http.ResponseWriter, id any, code int, message string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}
