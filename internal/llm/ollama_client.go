package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ccastromar/aos-agent-orchestration-system/internal/metrics"
)

type OllamaClient struct {
	BaseURL    string
	Model      string
	EmbedModel string
	HTTPClient *http.Client
}

var _ LLMClient = (*OllamaClient)(nil)

func NewOllamaClient(baseURL, model, embedModel string) *OllamaClient {
	if embedModel == "" {
		embedModel = "nomic-embed-text"
	}
	return &OllamaClient{
		BaseURL:    baseURL,
		Model:      model,
		EmbedModel: embedModel,
		HTTPClient: &http.Client{
			Timeout: 305 * time.Second,
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

func (c *OllamaClient) Chat(ctx context.Context, prompt string) (string, error) {
	payload := map[string]any{
		"model": c.Model,
		"messages": []map[string]any{
			{"role": "user", "content": prompt},
		},
		"stream": true,
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
		httpClient = &http.Client{Timeout: 305 * time.Second}
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

	for {
		var chunk struct {
			Message *struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			Done bool `json:"done"`
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
			break
		}
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
		httpClient = &http.Client{Timeout: 1 * time.Second}
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
		httpClient = &http.Client{Timeout: 305 * time.Second}
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
