package a2a

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

type review struct {
	started  time.Time
	rejected bool
}

var (
	mu    sync.Mutex
	tasks = map[string]review{}
)

// RegisterHandlers exposes a tiny A2A agent over the JSON-RPC binding: one endpoint for
// message/send (answers a working task) and tasks/get (the task completes after 250 ms).
// A review whose message reports "risk band high" is rejected; any other is approved.
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
		tasks[id] = review{started: time.Now(), rejected: strings.Contains(messageText(req.Params.Message), "risk band high")}
		mu.Unlock()
		writeResult(w, req.ID, task(id, "working", nil))
	case "tasks/get":
		mu.Lock()
		rv, ok := tasks[req.Params.ID]
		mu.Unlock()
		if !ok {
			writeError(w, req.ID, -32001, "task not found")
			return
		}
		if time.Since(rv.started) < 250*time.Millisecond {
			writeResult(w, req.ID, task(req.Params.ID, "working", nil))
			return
		}
		if rv.rejected {
			writeResult(w, req.ID, task(req.Params.ID, "rejected", []any{map[string]any{
				"artifactId": "review",
				"parts":      []any{map[string]any{"kind": "text", "text": "Compliance review rejected: high risk"}},
			}}))
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

// messageText joins the text parts of an A2A message.
func messageText(message map[string]any) string {
	parts, _ := message["parts"].([]any)
	var b strings.Builder
	for _, p := range parts {
		if m, ok := p.(map[string]any); ok {
			if s, ok := m["text"].(string); ok {
				b.WriteString(s)
			}
		}
	}
	return b.String()
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
