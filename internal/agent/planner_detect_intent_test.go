package agent

import (
	"context"
	"testing"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/llm"
	"github.com/stretchr/testify/require"
)

type dummyLLM struct {
	output string
}

// Ping implements llm.LLMClient.
func (d dummyLLM) Ping(ctx context.Context) error { return nil }

func (d dummyLLM) Embed(ctx context.Context, text string) ([]float32, error) { return []float32{}, nil }
func (d dummyLLM) Chat(ctx context.Context, prompt string) (string, error) {
	return d.output, nil
}

func TestDetectIntent(t *testing.T) {
	mock := dummyLLM{
		// DetectIntent expects the LLM to return ONLY the intent key
		output: "banking.get_balance",
	}

	schemas := map[string]any{
		"banking.get_balance": struct{}{},
	}

	di, err := llm.DetectIntent(context.Background(), mock, "saldo de mi cuenta", schemas)
	require.NoError(t, err)
	require.Equal(t, "banking.get_balance", di.Type)
	// current DetectIntent initializes empty params map
	require.NotNil(t, di.Params)
	require.Equal(t, 0, len(di.Params))
}
