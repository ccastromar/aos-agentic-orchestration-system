package agent

import (
	"context"
	"testing"

	"github.com/ccastromar/aos-agent-orchestration-system/internal/llm"
	"github.com/stretchr/testify/require"
)

type dummyParams struct {
	data string
}

// Ping implements llm.LLMClient.
func (d dummyParams) Ping(ctx context.Context) error { return nil }

func (d dummyParams) Embed(ctx context.Context, text string) ([]float32, error) { return []float32{}, nil }
func (d dummyParams) Chat(ctx context.Context, prompt string) (string, error) {
	return d.data, nil
}

func TestExtractParams(t *testing.T) {
	mock := dummyParams{
		data: `{"accountId":"999"}`,
	}

	params, err := llm.ExtractParams(context.Background(), mock, "check my balance", []string{"accountId"})
	require.NoError(t, err)
	require.Equal(t, "999", params["accountId"])
}
