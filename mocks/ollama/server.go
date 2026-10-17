// Package ollama is a deterministic stand-in for an Ollama server, built for live demos of
// AOS (and Prudence through AOS) without a real model. It speaks the subset of the Ollama
// HTTP API that AOS's OllamaClient uses — /api/tags, /api/chat (streaming, non-streaming and
// native tool calls) and /api/embeddings — so the real client code path runs unchanged. The
// same answers are also served OpenAI-style under /v1 for deployments using that client.
//
// It is NOT a language model. Each request is classified by the fixed prompts AOS sends
// (intent detection, parameter extraction, analyst summary, ReAct, code generation) and
// answered with rules: optional scenarios (scenarios.yaml) first, then keyword overlap with
// the intent catalog embedded in AOS's own prompt. The advertised model name says so.
package ollama

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// DefaultModel is the model name advertised by /api/tags. It is deliberately explicit so
// that UI stats and sealed evidence never suggest that a real LLM produced the output.
const DefaultModel = "aos-demo:deterministic"

// Config tunes the mock. Zero values are valid.
type Config struct {
	Model      string        // advertised model name (DefaultModel when empty)
	TokenDelay time.Duration // pause between streamed chunks, so the UI visibly "thinks"
	SlowDelay  time.Duration // pause injected by the #mock-fail:slow marker
	Scenarios  []Scenario    // demo-script overrides, checked before the generic matcher
}

type Server struct {
	cfg Config
}

func NewServer(cfg Config) *Server {
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if cfg.SlowDelay <= 0 {
		cfg.SlowDelay = 8 * time.Second
	}
	return &Server{cfg: cfg}
}

func (s *Server) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/api/version", s.handleVersion)
	mux.HandleFunc("/api/tags", s.handleTags)
	mux.HandleFunc("/api/chat", s.handleChat)
	mux.HandleFunc("/api/embeddings", s.handleEmbeddings)
	mux.HandleFunc("/api/embed", s.handleEmbed)
	s.registerOpenAI(mux)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write([]byte("Ollama is running (aos-demo deterministic mock)"))
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"version": "0.0.0-aos-demo"})
}

func (s *Server) handleTags(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"models": []map[string]any{{
			"name":        s.cfg.Model,
			"model":       s.cfg.Model,
			"modified_at": "2026-01-01T00:00:00Z",
			"size":        0,
			"details":     map[string]any{"family": "deterministic-mock", "parameter_size": "0B"},
		}},
	})
}

type chatMessage struct {
	Role      string     `json:"role"`
	Content   string     `json:"content,omitempty"`
	ToolName  string     `json:"tool_name,omitempty"`
	ToolCalls []toolCall `json:"tool_calls,omitempty"`
}

type toolCall struct {
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"function"`
}

type toolDefinition struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

type chatRequest struct {
	Model    string           `json:"model"`
	Messages []chatMessage    `json:"messages"`
	Tools    []toolDefinition `json:"tools"`
	Stream   *bool            `json:"stream"`
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body: " + err.Error()})
		return
	}
	model := req.Model
	if model == "" {
		model = s.cfg.Model
	}
	// Ollama streams unless told otherwise.
	stream := req.Stream == nil || *req.Stream
	prompt := lastUserContent(req.Messages)

	switch failureMarker(req.Messages) {
	case "500":
		log.Printf("[MOCK OLLAMA] fault injected: 500")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "injected failure (#mock-fail:500)"})
		return
	case "slow":
		log.Printf("[MOCK OLLAMA] fault injected: slow (%s)", s.cfg.SlowDelay)
		select {
		case <-time.After(s.cfg.SlowDelay):
		case <-r.Context().Done():
			return
		}
	case "badjson":
		log.Printf("[MOCK OLLAMA] fault injected: malformed model output")
		s.reply(w, r, model, stream, chatMessage{Role: "assistant", Content: "Sure! {\"intent\": banking, oops"})
		return
	}

	if len(req.Tools) > 0 {
		msg := reactStep(req.Messages, req.Tools)
		log.Printf("[MOCK OLLAMA] kind=react tools=%d -> %s", len(req.Tools), describeReact(msg))
		s.reply(w, r, model, stream, msg)
		return
	}

	kind, content := respond(prompt, s.cfg.Scenarios)
	log.Printf("[MOCK OLLAMA] kind=%s -> %s", kind, abbreviate(content, 160))
	s.reply(w, r, model, stream, chatMessage{Role: "assistant", Content: content})
}

// reply writes either one JSON object or an NDJSON stream of chunks, followed by a final
// chunk carrying done=true and Ollama-style runtime stats (AOS logs and surfaces them).
func (s *Server) reply(w http.ResponseWriter, r *http.Request, model string, stream bool, msg chatMessage) {
	start := time.Now()
	evalCount := len(strings.Fields(msg.Content)) + len(msg.ToolCalls)*8
	promptCount := 0
	if !stream {
		writeJSON(w, http.StatusOK, final(model, msg, start, promptCount, evalCount))
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	enc := json.NewEncoder(w)
	for _, piece := range chunks(msg.Content) {
		_ = enc.Encode(map[string]any{
			"model":      model,
			"created_at": time.Now().UTC().Format(time.RFC3339Nano),
			"message":    map[string]any{"role": msg.Role, "content": piece},
			"done":       false,
		})
		if flusher != nil {
			flusher.Flush()
		}
		if s.cfg.TokenDelay > 0 {
			select {
			case <-time.After(s.cfg.TokenDelay):
			case <-r.Context().Done():
				return
			}
		}
	}
	last := final(model, chatMessage{Role: msg.Role, ToolCalls: msg.ToolCalls}, start, promptCount, evalCount)
	_ = enc.Encode(last)
	if flusher != nil {
		flusher.Flush()
	}
}

func final(model string, msg chatMessage, start time.Time, promptCount, evalCount int) map[string]any {
	elapsed := time.Since(start).Nanoseconds()
	if elapsed <= 0 {
		elapsed = 1
	}
	return map[string]any{
		"model":                model,
		"created_at":           time.Now().UTC().Format(time.RFC3339Nano),
		"message":              msg,
		"done":                 true,
		"done_reason":          "stop",
		"total_duration":       elapsed,
		"load_duration":        0,
		"prompt_eval_count":    promptCount,
		"prompt_eval_duration": 0,
		"eval_count":           evalCount,
		"eval_duration":        elapsed,
	}
}

// chunks splits text into word-sized pieces, keeping whitespace so concatenation is exact.
func chunks(text string) []string {
	if text == "" {
		return []string{""}
	}
	var out []string
	var cur strings.Builder
	for _, r := range text {
		cur.WriteRune(r)
		if r == ' ' || r == '\n' {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func lastUserContent(messages []chatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

// failureMarker lets a presenter trigger failures from the chat box, e.g. typing
// "#mock-fail:500" in the message. Only the USER MESSAGE embedded in an AOS prompt is
// inspected — not conversation context or tool results — so a marker typed once does not
// keep failing every later turn of the session.
func failureMarker(messages []chatMessage) string {
	for _, m := range messages {
		if m.Role != "user" {
			continue
		}
		text := userMessage(m.Content)
		if text == "" {
			continue
		}
		idx := strings.Index(text, "#mock-fail:")
		if idx < 0 {
			continue
		}
		rest := text[idx+len("#mock-fail:"):]
		for _, kind := range []string{"500", "slow", "badjson"} {
			if strings.HasPrefix(rest, kind) {
				return kind
			}
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func abbreviate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func describeReact(msg chatMessage) string {
	if len(msg.ToolCalls) > 0 {
		return fmt.Sprintf("tool_call %s", msg.ToolCalls[0].Function.Name)
	}
	return "final answer"
}
