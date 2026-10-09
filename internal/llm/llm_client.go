package llm

import "context"

type LLMClient interface {
	Ping(ctx context.Context) error
	Chat(ctx context.Context, prompt string) (string, error)
	Embed(ctx context.Context, text string) ([]float32, error)
	//DetectIntent(text string) (string, map[string]any, error)
	//Summarize(ctx map[string]any) (string, error)
}

// ModelRoutableClient creates an execution-local client configured for a model.
// Implementations must not mutate the shared client.
type ModelRoutableClient interface {
	WithModel(model string) LLMClient
}
