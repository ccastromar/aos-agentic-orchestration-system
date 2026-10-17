package ollama

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// OpenAI-compatible surface, for AOS deployments that use LLM_ENGINE=mock|openai with
// LLM_BASE_URL=http://<mock>/v1 instead of the Ollama client. Same rules, same answers; only
// the wire format differs. Non-streaming only (AOS's OpenAI client never streams). With
// `tools` (e.g. a Pydantic AI agent) it calls the best-matching tool, then answers with the
// tool's result once it comes back.

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

// openAIMessage accepts the OpenAI chat shapes clients send: text or content-part arrays,
// assistant tool calls (arguments as a JSON string) and tool results keyed by tool_call_id.
type openAIMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Name       string          `json:"name"`
	ToolCallID string          `json:"tool_call_id"`
	ToolCalls  []struct {
		ID       string `json:"id"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tool_calls"`
}

// text flattens string or [{type:text,text:...}] content.
func (m openAIMessage) text() string {
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(m.Content, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			if p.Type == "text" {
				b.WriteString(p.Text)
			}
		}
		return b.String()
	}
	return ""
}

// toChatMessages maps OpenAI messages onto the internal shape, naming each tool result
// after the call it answers so reactStep can quote it.
func toChatMessages(in []openAIMessage) []chatMessage {
	names := map[string]string{}
	out := make([]chatMessage, 0, len(in))
	for _, m := range in {
		for _, c := range m.ToolCalls {
			names[c.ID] = c.Function.Name
		}
		msg := chatMessage{Role: m.Role, Content: m.text()}
		if m.Role == "tool" {
			msg.ToolName = names[m.ToolCallID]
			if msg.ToolName == "" {
				msg.ToolName = m.Name
			}
		}
		out = append(out, msg)
	}
	return out
}

func (s *Server) handleOpenAIChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Model    string           `json:"model"`
		Messages []openAIMessage  `json:"messages"`
		Tools    []toolDefinition `json:"tools"`
		Stream   bool             `json:"stream"`
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
	messages := toChatMessages(req.Messages)

	reply := chatMessage{Role: "assistant"}
	switch failureMarker(messages) {
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
		reply.Content = "Sure! {\"intent\": banking, oops"
	}
	if reply.Content == "" {
		if len(req.Tools) > 0 {
			reply = reactStep(messages, req.Tools)
			log.Printf("[MOCK OLLAMA] /v1 kind=react tools=%d -> %s", len(req.Tools), describeReact(reply))
		} else {
			var kind promptKind
			kind, reply.Content = respond(lastUserContent(messages), s.cfg.Scenarios)
			log.Printf("[MOCK OLLAMA] /v1 kind=%s -> %s", kind, abbreviate(reply.Content, 160))
		}
	}

	message := map[string]any{"role": "assistant", "content": reply.Content}
	finish := "stop"
	if len(reply.ToolCalls) > 0 {
		calls := make([]map[string]any, len(reply.ToolCalls))
		for i, c := range reply.ToolCalls {
			args, _ := json.Marshal(c.Function.Arguments)
			calls[i] = map[string]any{
				"id":       fmt.Sprintf("call_aos_demo_%d", i+1),
				"type":     "function",
				"function": map[string]any{"name": c.Function.Name, "arguments": string(args)},
			}
		}
		message = map[string]any{"role": "assistant", "content": nil, "tool_calls": calls}
		finish = "tool_calls"
	}
	completion := len(strings.Fields(reply.Content)) + len(reply.ToolCalls)*8
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      "chatcmpl-aos-demo",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{{"index": 0, "message": message, "finish_reason": finish}},
		"usage":   map[string]any{"prompt_tokens": 0, "completion_tokens": completion, "total_tokens": completion},
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
