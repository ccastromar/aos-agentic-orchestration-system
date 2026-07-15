package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/tools"
	"github.com/stretchr/testify/require"
)

func init() {
	tools.SkipSSRF = true
}

func TestVerifierPipelineMock(t *testing.T) {
	// mock service
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"service": "payments-api",
			"status":  "running",
		})
	}))
	defer ts.Close()

	tool := config.Tool{
		Name:      "infra_get_status",
		Type:      "http",
		Method:    "GET",
		URL:       ts.URL + "/status?service={{ .serviceName }}",
		TimeoutMs: 2000,
	}

 out, err := tools.ExecuteTool(tool, map[string]string{"serviceName": "payments-api"})
	require.NoError(t, err)
	require.Equal(t, "payments-api", out["service"])
	require.Equal(t, "running", out["status"])
}
