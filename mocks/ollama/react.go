package ollama

import (
	"encoding/json"
	"hash/fnv"
	"math"
	"net/http"
	"sort"
	"strings"
)

// reactStep answers a native tool-calling turn. First turn: call the tool whose name and
// description best overlap the task, filling required arguments with the same extractor
// used for intents. Once a tool observation is in the history: finish, quoting it.
func reactStep(messages []chatMessage, tools []toolDefinition) chatMessage {
	task := ""
	var lastTool *chatMessage
	for i := range messages {
		m := &messages[i]
		switch m.Role {
		case "user":
			if task == "" {
				task = m.Content
			}
		case "tool":
			lastTool = m
		}
	}
	if lastTool != nil {
		return chatMessage{Role: "assistant", Content: "Deterministic demo model: tool " + lastTool.ToolName +
			" returned: " + abbreviate(lastTool.Content, 400)}
	}

	taskSet := tokenSet(task)
	best, bestScore := -1, -1.0
	order := make([]int, len(tools))
	for i := range tools {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return tools[order[a]].Function.Name < tools[order[b]].Function.Name })
	for _, i := range order {
		fn := tools[i].Function
		score := overlap(taskSet, tokenSet(strings.NewReplacer(".", " ", "_", " ").Replace(fn.Name)+" "+fn.Description))
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 {
		return chatMessage{Role: "assistant", Content: "Deterministic demo model: no tools available."}
	}
	fn := tools[best].Function
	call := toolCall{Type: "function"}
	call.Function.Name = fn.Name
	call.Function.Arguments = map[string]any{}
	for _, name := range requiredArgs(fn.Parameters) {
		value := guessParam(task, name)
		if value == "" && name == "path" {
			value = "."
		}
		call.Function.Arguments[name] = value
	}
	return chatMessage{Role: "assistant", ToolCalls: []toolCall{call}}
}

func requiredArgs(schema map[string]any) []string {
	var out []string
	if list, ok := schema["required"].([]any); ok {
		for _, v := range list {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// Embeddings: feature hashing of the same normalized tokens the matcher uses, L2-normalized.
// Similar texts share tokens and therefore get a high cosine similarity, which is enough for
// AOS's memory/recall paths to behave plausibly in a demo.
const embeddingDims = 768

func embed(text string) []float32 {
	vec := make([]float64, embeddingDims)
	for _, t := range tokens(text) {
		h := fnv.New64a()
		_, _ = h.Write([]byte(t))
		sum := h.Sum64()
		sign := 1.0
		if sum&1 == 1 {
			sign = -1
		}
		vec[(sum>>1)%embeddingDims] += sign
	}
	norm := 0.0
	for _, v := range vec {
		norm += v * v
	}
	out := make([]float32, embeddingDims)
	if norm == 0 {
		return out
	}
	norm = math.Sqrt(norm)
	for i, v := range vec {
		out[i] = float32(v / norm)
	}
	return out
}

func (s *Server) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"embedding": embed(req.Prompt)})
}

// handleEmbed is the newer batch endpoint (input: string or []string).
func (s *Server) handleEmbed(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
		Input any    `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	var inputs []string
	switch v := req.Input.(type) {
	case string:
		inputs = []string{v}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				inputs = append(inputs, s)
			}
		}
	}
	vectors := make([][]float32, len(inputs))
	for i, in := range inputs {
		vectors[i] = embed(in)
	}
	writeJSON(w, http.StatusOK, map[string]any{"model": req.Model, "embeddings": vectors})
}
