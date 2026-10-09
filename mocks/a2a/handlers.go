package a2a

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	mu    sync.Mutex
	tasks = map[string]time.Time{}
)

func RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/mock/a2a/message:send", send)
	mux.HandleFunc("/mock/a2a/tasks/", task)
}

func send(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	id := "a2a-task-" + time.Now().Format("150405.000000000")
	mu.Lock()
	tasks[id] = time.Now()
	mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"id": id, "status": map[string]any{"state": "working"}}})
}

func task(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/mock/a2a/tasks/")
	mu.Lock()
	started, ok := tasks[id]
	mu.Unlock()
	if !ok {
		http.Error(w, "task not found", 404)
		return
	}
	state := "working"
	result := map[string]any{}
	if time.Since(started) >= 250*time.Millisecond {
		state = "completed"
		result = map[string]any{"artifacts": []any{map[string]any{"parts": []any{map[string]any{"kind": "text", "text": "Compliance review approved"}}}}}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"id": id, "status": map[string]any{"state": state}, "result": result}})
}
