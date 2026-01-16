package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ccastromar/aos-agent-orchestration-system/internal/metrics"
)

type OpenAIClient struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
	Timeout time.Duration
}

// Compile-time interface conformance
var _ LLMClient = (*OpenAIClient)(nil)

func NewOpenAIClient(baseURL, apiKey, model string) *OpenAIClient {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	return &OpenAIClient{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   model,
		HTTP: &http.Client{
			Timeout: 30 * time.Second,
		},
		Timeout: 30 * time.Second,
	}

}

func (c *OpenAIClient) Ping(ctx context.Context) error {
	if c.APIKey == "" {
		return fmt.Errorf("openai api key is empty")
	}

	to := c.Timeout
	if to <= 0 {
		to = 2 * time.Second
	}
	var cancel context.CancelFunc
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel = context.WithTimeout(ctx, to)
	defer cancel()

	url := strings.TrimRight(c.BaseURL, "/") + "/models"
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: to}
	}

	resp, err := retryHTTP(ctx, 3, 100*time.Millisecond, func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		return httpClient.Do(req)
	})
	if err != nil {
		metrics.LLMPings.Inc(map[string]string{"provider": "openai", "outcome": "error"})
		return fmt.Errorf("openai ping failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		metrics.LLMPings.Inc(map[string]string{"provider": "openai", "outcome": "error"})
		return fmt.Errorf("openai ping bad status: %d, body: %s", resp.StatusCode, string(b))
	}

	metrics.LLMPings.Inc(map[string]string{"provider": "openai", "outcome": "ok"})
	return nil

}

func (c *OpenAIClient) Chat(ctx context.Context, prompt string) (string, error) {
	if c.APIKey == "" {
		return "", fmt.Errorf("openai api key is empty")
	}

	payload := map[string]any{
		"model": c.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"temperature": 0,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	to := c.Timeout
	if to <= 0 {
		to = 30 * time.Second
	}
	var cancel context.CancelFunc
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel = context.WithTimeout(ctx, to)
	defer cancel()

	url := strings.TrimRight(c.BaseURL, "/") + "/chat/completions"
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: to}
	}

	start := time.Now()
	resp, err := retryHTTP(ctx, 3, 100*time.Millisecond, func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", "application/json")
		return httpClient.Do(req)
	})
	if err != nil {
		metrics.LLMChats.Inc(map[string]string{"provider": "openai", "outcome": "error"})
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		metrics.LLMChats.Inc(map[string]string{"provider": "openai", "outcome": "error"})
		return "", fmt.Errorf("openai chat failed: status %d, body: %s", resp.StatusCode, string(b))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		metrics.LLMChats.Inc(map[string]string{"provider": "openai", "outcome": "error"})
		return "", err
	}

	if len(result.Choices) == 0 {
		metrics.LLMChats.Inc(map[string]string{"provider": "openai", "outcome": "error"})
		return "", fmt.Errorf("openai: empty response")
	}

	metrics.LLMChats.Inc(map[string]string{"provider": "openai", "outcome": "ok"})
	metrics.LLMChatDur.Observe(map[string]string{"provider": "openai", "outcome": "ok"}, time.Since(start).Seconds())
	return result.Choices[0].Message.Content, nil

}
