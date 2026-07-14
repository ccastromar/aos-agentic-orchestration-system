package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ccastromar/aos-agent-orchestration-system/internal/app"
	"github.com/stretchr/testify/assert"
)

func TestE2EResiliency(t *testing.T) {
	chdirToRepoRoot(t)
	application, err := app.New()
	assert.NoError(t, err)

	var flakyHits int32

	// Fake backend
	mux9000 := http.NewServeMux()
	mux9000.HandleFunc("/mock/flaky", func(w http.ResponseWriter, r *http.Request) {
		hits := atomic.AddInt32(&flakyHits, 1)
		if hits < 3 { // Fallar las 2 primeras veces
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
	})
	mux9000.HandleFunc("/mock/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
	})
	mux9000.HandleFunc("/mock/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})

	srv9000 := &http.Server{Addr: "localhost:9000", Handler: mux9000}
	go func() { _ = srv9000.ListenAndServe() }()

	client := &http.Client{Timeout: 100 * time.Millisecond}
	ready := false
	for i := 0; i < 50; i++ { // ~5s max
		resp, err := client.Get("http://localhost:9000/mock/ping")
		if err == nil {
			resp.Body.Close()
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("mock backend on :9000 not ready in time")
	}

	defer func() {
		ctx2, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_ = srv9000.Shutdown(ctx2)
	}()

	// Iniciar agentes en background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	application.StartAgents(ctx)

	ts := httptest.NewServer(application.Handler())
	defer ts.Close()

	// Disparar el pipeline que tiene retries y timeouts
	reqBody := `{"operation": "intent_resiliency_demo", "lang": "es", "params": {"param1": "test"}}`
	resp, err := http.Post(ts.URL+"/ask_structured", "application/json", strings.NewReader(reqBody))
	assert.NoError(t, err)

	// El pipeline deberia fallar por el timeout del paso slow (300ms > 100ms)
	assert.Equal(t, 500, resp.StatusCode)

	var res map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	assert.NoError(t, err)

	// Verificamos que el error reportado contenga "context deadline exceeded" (timeout)
	errStr, ok := res["error"].(string)
	assert.True(t, ok)
	assert.Contains(t, errStr, "context deadline exceeded")

	// Además flaky tool debio haber sido llamado 3 veces (1 original + 2 retries)
	assert.Equal(t, int32(3), atomic.LoadInt32(&flakyHits))
}
