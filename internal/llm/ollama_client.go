package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/logx"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/metrics"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/telemetry"
)

type OllamaClient struct {
	BaseURL         string
	Model           string
	IntentModel     string
	EmbedModel      string
	HTTPClient      *http.Client
	NumCtx          int
	ReactNumPredict int
}

var _ LLMClient = (*OllamaClient)(nil)
var _ ModelRoutableClient = (*OllamaClient)(nil)

func (c *OllamaClient) WithModel(model string) LLMClient {
	copy := *c
	copy.Model = strings.TrimSpace(model)
	return &copy
}

func NewOllamaClient(baseURL, model, embedModel string) *OllamaClient {
	if embedModel == "" {
		embedModel = "nomic-embed-text"
	}
	numCtx := 8192
	if raw := os.Getenv("OLLAMA_NUM_CTX"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			numCtx = parsed
		}
	}
	reactNumPredict := 4096
	if raw := os.Getenv("OLLAMA_REACT_NUM_PREDICT"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			reactNumPredict = parsed
		}
	}
	intentModel := strings.TrimSpace(os.Getenv("OLLAMA_INTENT_MODEL"))
	return &OllamaClient{
		BaseURL:         baseURL,
		Model:           model,
		IntentModel:     intentModel,
		EmbedModel:      embedModel,
		NumCtx:          numCtx,
		ReactNumPredict: reactNumPredict,
		HTTPClient: &http.Client{
			Timeout:   305 * time.Second,
			Transport: telemetry.OutboundTransport(nil),
		},
	}
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatResponse struct {
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

type ollamaRuntimeStats struct {
	TotalDuration      int64 `json:"total_duration"`
	LoadDuration       int64 `json:"load_duration"`
	PromptEvalCount    int64 `json:"prompt_eval_count"`
	PromptEvalDuration int64 `json:"prompt_eval_duration"`
	EvalCount          int64 `json:"eval_count"`
	EvalDuration       int64 `json:"eval_duration"`
}

func tokensPerSecond(count, durationNS int64) float64 {
	if count <= 0 || durationNS <= 0 {
		return 0
	}
	return float64(count) / (float64(durationNS) / float64(time.Second))
}

func logOllamaRuntimeStats(model string, stats ollamaRuntimeStats) {
	logx.Info("Ollama", "model=%s total=%s load=%s prompt=%d (%.1f tok/s) output=%d (%.1f tok/s)",
		model,
		time.Duration(stats.TotalDuration),
		time.Duration(stats.LoadDuration),
		stats.PromptEvalCount,
		tokensPerSecond(stats.PromptEvalCount, stats.PromptEvalDuration),
		stats.EvalCount,
		tokensPerSecond(stats.EvalCount, stats.EvalDuration),
	)
}

func runtimeStatsForUI(model string, stats ollamaRuntimeStats) *state.LLMRuntimeStats {
	if stats.TotalDuration <= 0 && stats.PromptEvalCount <= 0 && stats.EvalCount <= 0 {
		return nil
	}
	return &state.LLMRuntimeStats{
		Model:                 model,
		TotalDurationSeconds:  time.Duration(stats.TotalDuration).Seconds(),
		LoadDurationSeconds:   time.Duration(stats.LoadDuration).Seconds(),
		PromptTokens:          stats.PromptEvalCount,
		PromptTokensPerSecond: tokensPerSecond(stats.PromptEvalCount, stats.PromptEvalDuration),
		OutputTokens:          stats.EvalCount,
		OutputTokensPerSecond: tokensPerSecond(stats.EvalCount, stats.EvalDuration),
	}
}

func (c *OllamaClient) Chat(ctx context.Context, prompt string) (string, error) {
	return c.chat(ctx, prompt, c.Model, false, 0)
}

// ChatForIntent disables Qwen3's extended reasoning and caps the tiny classifier
// response. Other Ollama chat tasks keep their normal generation behavior.
func (c *OllamaClient) ChatForIntent(ctx context.Context, prompt string, maxTokens int) (string, error) {
	model := c.IntentModel
	if model == "" {
		model = c.Model
	}
	return c.chat(ctx, prompt, model, true, maxTokens)
}

func (c *OllamaClient) chat(ctx context.Context, prompt, model string, direct bool, maxTokens int) (string, error) {
	options := map[string]any{"num_ctx": c.NumCtx}
	if maxTokens > 0 {
		options["num_predict"] = maxTokens
	}
	payload := map[string]any{
		"model": model,
		"messages": []map[string]any{
			{"role": "user", "content": prompt},
		},
		"stream":  true,
		"options": options,
	}
	if direct {
		payload["think"] = false
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	// Context with timeout prevents hangs; derive from provided ctx
	if ctx == nil {
		ctx = context.Background()
	}
	// Increase timeout for slower machines like Mac Mini (e.g. 5 minutes)
	ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 305 * time.Second, Transport: telemetry.OutboundTransport(nil)}
	}

	start := time.Now()
	resp, err := retryHTTP(ctx, 3, 100*time.Millisecond, func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/api/chat", bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("new request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		return httpClient.Do(req)
	})
	if err != nil {
		metrics.LLMChats.Inc(map[string]string{"provider": "ollama", "outcome": "error"})
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		metrics.LLMChats.Inc(map[string]string{"provider": "ollama", "outcome": "error"})
		return "", fmt.Errorf("ollama chat failed: status %d, body: %s", resp.StatusCode, string(b))
	}

	dec := json.NewDecoder(resp.Body)
	var out bytes.Buffer
	var runtimeStats ollamaRuntimeStats
	doneReason := ""

	for {
		var chunk struct {
			Message *struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			Done       bool   `json:"done"`
			DoneReason string `json:"done_reason"`
			ollamaRuntimeStats
		}

		if err := dec.Decode(&chunk); err != nil {
			if err.Error() == "EOF" {
				break
			}
			metrics.LLMChats.Inc(map[string]string{"provider": "ollama", "outcome": "error"})
			return "", err
		}

		if chunk.Message != nil {
			out.WriteString(chunk.Message.Content)
		}

		if chunk.Done {
			runtimeStats = chunk.ollamaRuntimeStats
			doneReason = chunk.DoneReason
			break
		}
	}
	logOllamaRuntimeStats(model, runtimeStats)
	if doneReason == "length" {
		return "", fmt.Errorf("ollama chat response was truncated by the model token limit")
	}

	metrics.LLMChats.Inc(map[string]string{"provider": "ollama", "outcome": "ok"})
	metrics.LLMChatDur.Observe(map[string]string{"provider": "ollama", "outcome": "ok"}, time.Since(start).Seconds())
	return out.String(), nil
}

// Ping checks if Ollama is reachable and responding.
func (c *OllamaClient) Ping(ctx context.Context) error {
	// Ollama health: GET /api/tags
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 1 * time.Second, Transport: telemetry.OutboundTransport(nil)}
	}

	resp, err := retryHTTP(ctx, 3, 50*time.Millisecond, func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+"/api/tags", nil)
		if err != nil {
			return nil, err
		}
		return httpClient.Do(req)
	})
	if err != nil {
		metrics.LLMPings.Inc(map[string]string{"provider": "ollama", "outcome": "error"})
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		metrics.LLMPings.Inc(map[string]string{"provider": "ollama", "outcome": "error"})
		return fmt.Errorf("llm ping failed: status %d", resp.StatusCode)
	}
	metrics.LLMPings.Inc(map[string]string{"provider": "ollama", "outcome": "ok"})
	return nil
}

type ollamaEmbedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbedResponse struct {
	Embedding []float32 `json:"embedding"`
}

func (c *OllamaClient) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := ollamaEmbedRequest{
		Model:  c.EmbedModel,
		Prompt: text,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal embed payload: %w", err)
	}

	if ctx == nil {
		ctx = context.Background()
	}
	// Increase timeout for slower machines like Mac Mini (e.g. 5 minutes)
	ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 305 * time.Second, Transport: telemetry.OutboundTransport(nil)}
	}

	resp, err := retryHTTP(ctx, 3, 100*time.Millisecond, func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/api/embeddings", bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("new request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		return httpClient.Do(req)
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed failed: status %d, body: %s", resp.StatusCode, string(b))
	}

	var res ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("decode embed response: %w", err)
	}

	return res.Embedding, nil
}
