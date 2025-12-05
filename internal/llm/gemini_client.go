package llm

import (
	"context"
	"fmt"
	"os"
	"strings"

	"google.golang.org/genai"
)

// GeminiClient es una implementación de LLMClient usando Google Gemini.
// Ajusta los métodos para que encajen con tu interfaz LLMClient actual.
type GeminiClient struct {
	client *genai.Client
	model  string
}

// NewGeminiClient crea un nuevo cliente Gemini usando el SDK oficial.
// - Lee la API key de GEMINI_API_KEY (o puedes pasársela tú a mano si prefieres).
// - model típico: "gemini-2.5-flash", "gemini-1.5-pro", etc.
func NewGeminiClient(ctx context.Context, model string) (*GeminiClient, error) {
	apiKey := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY not set")
	}

	cli, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
		// Opcional: versión de API explícita
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

// -----------------------------------------------------------------------------
// 🔵 Ping method — for health checks
// -----------------------------------------------------------------------------
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

// Close libera recursos del cliente Gemini.
// Llama a esto si tu ciclo de vida de app lo permite (por ejemplo al cerrar la App).
//func (g *GeminiClient) Close() error {
//	if g == nil || g.client == nil {
//		return nil
//	}
//	return g.client.Close()
//}

// -----------------------------------------------------------------------------
// Métodos de alto nivel para enganchar con tu LLMClient
// -----------------------------------------------------------------------------
//
// *** IMPORTANTE ***
// Adapta este método a la firma que ya use tu interfaz LLMClient.
// Ejemplo: si tu interfaz es:
//
//   type LLMClient interface {
//       Chat(ctx context.Context, systemPrompt, userPrompt string) (string, error)
//   }
//
// entonces este método ya serviría directamente.
// -----------------------------------------------------------------------------

// Chat genera una respuesta de Gemini dado un systemPrompt y un userPrompt.
// Combina ambos en un único texto (simple, pero suficiente para tu caso actual).
func (g *GeminiClient) Chat(ctx context.Context, userPrompt string) (string, error) {
	if g == nil || g.client == nil {
		return "", fmt.Errorf("gemini client not initialized")
	}

	fullPrompt := strings.TrimSpace(userPrompt)

	resp, err := g.client.Models.GenerateContent(
		ctx,
		g.model,
		genai.Text(fullPrompt),
		nil, // sin configuración extra de momento
	)
	if err != nil {
		return "", fmt.Errorf("gemini generate content failed: %w", err)
	}

	return resp.Text(), nil
}

// -----------------------------------------------------------------------------
// (Opcional) Helpers específicos para tu lib llm
// -----------------------------------------------------------------------------
//
// Si en tu paquete llm tienes helpers como:
//
//   func DetectIntent(ctx context.Context, c LLMClient, msg string, intents map[string]any) (...)
//   func ExtractParams(...)
//   func SummarizeResult(...)
//
// lo normal es que internamente usen algo tipo c.Chat(...).
// Si tu interfaz LLMClient es distinta, solo tienes que:
//
// 1. Hacer que GeminiClient implemente esa interfaz (renombrar Chat → Generate, etc.)
// 2. Dejar los helpers tal cual.
//
// Aquí te dejo un ejemplo extra de método más genérico por si lo quieres usar:
// -----------------------------------------------------------------------------

// GenerateSimple es un wrapper genérico: le pasas un prompt y devuelve texto.
// Puedes ignorarlo si tu interfaz ya usa otro nombre.
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
