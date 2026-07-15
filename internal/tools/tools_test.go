package tools_test

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

func TestExecuteTool_URLRendering(t *testing.T) {
	var receivedURL string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.String()
		err := json.NewEncoder(w).Encode(map[string]any{"ok": true})
		if err != nil {
			return
		}
	}))
	defer ts.Close()

	tool := config.Tool{
		Name:      "core_get_balance",
		Type:      "http",
		Method:    "GET",
		URL:       ts.URL + "/mock/balance?accountId={{ .accountId }}",
		TimeoutMs: 2000,
	}

	params := map[string]string{"accountId": "555"}

	out, err := tools.ExecuteTool(tool, params)
	require.NoError(t, err)
	require.Equal(t, true, out["ok"])

	require.Equal(t, "/mock/balance?accountId=555", receivedURL)
}

func TestExecuteTool_URLRendering_MissingParamRendersEmpty(t *testing.T) {
	var receivedURL string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.String()
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer ts.Close()

	tool := config.Tool{
		Name:      "core_get_balance",
		Type:      "http",
		Method:    "GET",
		URL:       ts.URL + "/mock/balance?accountId={{ .accountId }}",
		TimeoutMs: 2000,
	}

	// Note: accountId is intentionally missing to verify it renders as empty string, not "<no value>"
	params := map[string]string{}

	out, err := tools.ExecuteTool(tool, params)
	require.NoError(t, err)
	require.Equal(t, true, out["ok"])

	require.NotContains(t, receivedURL, "<no value>")
	require.Equal(t, "/mock/balance?accountId=", receivedURL)
}

func TestExecuteTool_BodyRendering_MissingParamsRenderEmpty(t *testing.T) {
	var receivedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		json.NewDecoder(r.Body).Decode(&receivedBody)
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer ts.Close()

	tool := config.Tool{
		Name:      "payments_bizum_send",
		Type:      "http",
		Method:    "POST",
		URL:       ts.URL + "/mock/payments/bizum",
		TimeoutMs: 2000,
		Body: map[string]string{
			"from":    "{{ .fromPhone }}",
			"to":      "{{ .toPhone }}",
			"amount":  "{{ .amount }}",
			"concept": "{{ .concept }}",
		},
	}

	// Provide only some params; others are missing and should render as empty strings
	params := map[string]string{"fromPhone": "111", "amount": "5"}

	out, err := tools.ExecuteTool(tool, params)
	require.NoError(t, err)
	require.Equal(t, true, out["ok"])

	require.NotNil(t, receivedBody)
	require.Equal(t, "111", receivedBody["from"])
	require.Equal(t, "5", receivedBody["amount"])
	// Missing ones should be empty strings, not "<no value>"
	require.Equal(t, "", receivedBody["to"])
	require.Equal(t, "", receivedBody["concept"])
}

func TestExecuteTool_HTTPErrorIncludesStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	tool := config.Tool{
		Name:      "err_tool",
		Type:      "http",
		Method:    "GET",
		URL:       ts.URL + "/err?x={{ .x }}",
		TimeoutMs: 1000,
	}

	_, err := tools.ExecuteTool(tool, map[string]string{"x": "1"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 500")
}

func TestExecuteTool_HeadersWithEnv(t *testing.T) {
	// Set an env var that will be consumed by template function env
	t.Setenv("API_TOKEN", "secret123")

	var gotAuth string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		err := json.NewEncoder(w).Encode(map[string]any{"ok": true})
		if err != nil {
			return
		}
	}))
	defer ts.Close()

	tool := config.Tool{
		Name:      "with_headers",
		Type:      "http",
		Method:    "GET",
		URL:       ts.URL + "/ping",
		TimeoutMs: 1000,
		Headers: map[string]string{
			"Authorization": "Bearer {{ env \"API_TOKEN\" }}",
		},
	}

	_, err := tools.ExecuteTool(tool, map[string]string{})
	require.NoError(t, err)
	require.Equal(t, "Bearer secret123", gotAuth)
}

func TestExecuteTool_HeadersWithAPIKeyEnv(t *testing.T) {
	// Ensure we can read API_KEY from environment in header templates
	t.Setenv("API_KEY", "banking-abc-123")

	var gotAuth string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		err := json.NewEncoder(w).Encode(map[string]any{"ok": true})
		if err != nil {
			return
		}
	}))
	defer ts.Close()

	tool := config.Tool{
		Name:      "with_api_key_header",
		Type:      "http",
		Method:    "GET",
		URL:       ts.URL + "/ping",
		TimeoutMs: 1000,
		Headers: map[string]string{
			"Authorization": "Bearer {{ env \"API_KEY\" }}",
		},
	}

	_, err := tools.ExecuteTool(tool, map[string]string{})
	require.NoError(t, err)
	require.Equal(t, "Bearer banking-abc-123", gotAuth)
}
