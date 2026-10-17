package ollama

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

// OpenAI-compatible surface, for AOS deployments that use LLM_ENGINE=mock|openai with
// LLM_BASE_URL=http://<mock>/v1 instead of the Ollama client. Same rules, same answers; only
// the wire format differs. Non-streaming only (AOS's OpenAI client never streams).

func (s *Server) registerOpenAI(mux *http.ServeMux) {
	mux.HandleFunc("/v1/models", s.handleOpenAIModels)
	mux.HandleFunc("/v1/chat/completions", s.handleOpenAIChat)
	mux.HandleFunc("/v1/embeddings", s.handleOpenAIEmbeddings)
}

func (s *Server) handleOpenAIModels(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   []map[string]any{{"id": s.cfg.Model, "object": "model", "owned_by": "aos-demo"}},
	})
}

func (s *Server) handleOpenAIChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Model    string        `json:"model"`
		Messages []chatMessage `json:"messages"`
		Stream   bool          `json:"stream"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "invalid request body"}})
		return
	}
	if req.Stream {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "streaming is not supported by the aos-demo mock on /v1"}})
		return
	}
	model := req.Model
	if model == "" {
		model = s.cfg.Model
	}

	content := ""
	switch failureMarker(req.Messages) {
	case "500":
		log.Printf("[MOCK OLLAMA] /v1 fault injected: 500")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]any{"message": "injected failure (#mock-fail:500)"}})
		return
	case "slow":
		log.Printf("[MOCK OLLAMA] /v1 fault injected: slow (%s)", s.cfg.SlowDelay)
		select {
		case <-time.After(s.cfg.SlowDelay):
		case <-r.Context().Done():
			return
		}
	case "badjson":
		log.Printf("[MOCK OLLAMA] /v1 fault injected: malformed model output")
		content = "Sure! {\"intent\": banking, oops"
	}
	if content == "" {
		var kind promptKind
		kind, content = respond(lastUserContent(req.Messages), s.cfg.Scenarios)
		log.Printf("[MOCK OLLAMA] /v1 kind=%s -> %s", kind, abbreviate(content, 160))
	}

	completion := len(strings.Fields(content))
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      "chatcmpl-aos-demo",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": content},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 0, "completion_tokens": completion, "total_tokens": completion},
	})
}

func (s *Server) handleOpenAIEmbeddings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
		Input any    `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "invalid request body"}})
		return
	}
	var inputs []string
	switch v := req.Input.(type) {
	case string:
		inputs = []string{v}
	case []any:
		for _, item := range v {
			if text, ok := item.(string); ok {
				inputs = append(inputs, text)
			}
		}
	}
	data := make([]map[string]any, len(inputs))
	for i, in := range inputs {
		data[i] = map[string]any{"object": "embedding", "index": i, "embedding": embed(in)}
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data, "model": req.Model})
}
