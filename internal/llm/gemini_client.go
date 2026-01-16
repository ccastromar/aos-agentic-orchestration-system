package llm

import (
	"context"
	"fmt"
	"os"
	"strings"

	"google.golang.org/genai"
)

type GeminiClient struct {
	client *genai.Client
	model  string
}

func NewGeminiClient(ctx context.Context, model string) (*GeminiClient, error) {
	apiKey := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY not set")
	}

	cli, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
		HTTPOptions: genai.HTTPOptions{
			APIVersion: "v1",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create gemini client: %w", err)
	}

	if model == "" {
		model = "gemini-2.5-flash"
	}

	return &GeminiClient{
		client: cli,
		model:  model,
	}, nil
}

func (g *GeminiClient) Ping(ctx context.Context) error {
	if g == nil || g.client == nil {
		return fmt.Errorf("gemini client not initialized")
	}

	// send a minimal harmless prompt just to check connectivity
	resp, err := g.client.Models.GenerateContent(
		ctx,
		g.model,
		genai.Text("ping"),
		nil,
	)
	if err != nil {
		return fmt.Errorf("gemini ping failed: %w", err)
	}

	if resp == nil || strings.TrimSpace(resp.Text()) == "" {
		return fmt.Errorf("gemini ping returned empty response")
	}

	return nil
}

func (g *GeminiClient) Chat(ctx context.Context, userPrompt string) (string, error) {
	if g == nil || g.client == nil {
		return "", fmt.Errorf("gemini client not initialized")
	}

	fullPrompt := strings.TrimSpace(userPrompt)

	resp, err := g.client.Models.GenerateContent(
		ctx,
		g.model,
		genai.Text(fullPrompt),
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("gemini generate content failed: %w", err)
	}

	return resp.Text(), nil
}

func (g *GeminiClient) GenerateSimple(ctx context.Context, prompt string) (string, error) {
	if g == nil || g.client == nil {
		return "", fmt.Errorf("gemini client not initialized")
	}
	resp, err := g.client.Models.GenerateContent(
		ctx,
		g.model,
		genai.Text(prompt),
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("gemini generate content failed: %w", err)
	}
	return resp.Text(), nil
}
