package llm

import "context"

type LLMClient interface {
    Ping(ctx context.Context) error
    Chat(ctx context.Context, prompt string) (string, error)
    Embed(ctx context.Context, text string) ([]float32, error)
    //DetectIntent(text string) (string, map[string]any, error)
    //Summarize(ctx map[string]any) (string, error)
}
