package runtime

import (
    "context"
    "testing"
)

// The Runtime type is a simple data holder; this test ensures
// its fields can be set and read as expected.
type fakeLLM struct{}

func (f *fakeLLM) Ping(ctx context.Context) error                   { return nil }
func (f *fakeLLM) Embed(ctx context.Context, text string) ([]float32, error) { return []float32{}, nil }
func (f *fakeLLM) Chat(ctx context.Context, prompt string) (string, error) { return "", nil }

func TestRuntimeFields(t *testing.T) {
    rt := &Runtime{SpecsLoaded: true, LLMClient: &fakeLLM{}}

    if !rt.SpecsLoaded {
        t.Fatalf("SpecsLoaded should be true")
    }
    if rt.LLMClient == nil {
        t.Fatalf("LLMClient should not be nil")
    }
    if err := rt.LLMClient.Ping(context.Background()); err != nil {
        t.Fatalf("Ping should succeed: %v", err)
    }
}
