package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ccastromar/aos-agent-orchestration-system/internal/app"
	"github.com/stretchr/testify/assert"
)

func TestE2EOpenAPI(t *testing.T) {
	chdirToRepoRoot(t)
	application, err := app.New()
	assert.NoError(t, err)

	// Fake backend para el API dinámico
	mux9001 := http.NewServeMux()
	mux9001.HandleFunc("/api/v1/users/", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer mocktoken123", r.Header.Get("Authorization"))
		assert.Equal(t, http.MethodGet, r.Method)
		assert.True(t, strings.HasSuffix(r.URL.Path, "123")) // userId = 123
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "name": "John Doe"})
	})
	mux9001.HandleFunc("/api/v1/posts", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer mocktoken123", r.Header.Get("Authorization"))
		assert.Equal(t, http.MethodPost, r.Method)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "post_id": 999})
	})
	mux9001.HandleFunc("/api/v1/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})

	srv9001 := &http.Server{Addr: "localhost:9001", Handler: mux9001}
	go func() { _ = srv9001.ListenAndServe() }()

	client := &http.Client{Timeout: 100 * time.Millisecond}
	ready := false
	for i := 0; i < 50; i++ { // ~5s max
		resp, err := client.Get("http://localhost:9001/api/v1/ping")
		if err == nil {
			resp.Body.Close()
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("mock backend on :9001 not ready in time")
	}

	defer func() {
		ctx2, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_ = srv9001.Shutdown(ctx2)
	}()

	// Iniciar agentes en background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	application.StartAgents(ctx)

	ts := httptest.NewServer(application.Handler())
	defer ts.Close()

	// Disparar el pipeline
	reqBody := `{"operation": "intent_openapi_demo", "lang": "es", "params": {"userId": "123", "title": "My Post"}}`
	resp, err := http.Post(ts.URL+"/ask_structured", "application/json", strings.NewReader(reqBody))
	assert.NoError(t, err)

	assert.Equal(t, 200, resp.StatusCode)

	var res map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	assert.NoError(t, err)

	assert.Equal(t, "ok", res["status"])
}
