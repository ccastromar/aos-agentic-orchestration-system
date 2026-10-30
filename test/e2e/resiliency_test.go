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

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/app"
)

func TestE2EResiliency(t *testing.T) {
	chdirToRepoRoot(t)
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

	mockServer := httptest.NewServer(mux9000)
	defer mockServer.Close()
	t.Setenv("AOS_MOCK_BASE_URL", mockServer.URL)

	application, err := app.New()
	if err != nil {
		t.Fatalf("initialize app: %v", err)
	}

	// Iniciar agentes en background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	application.StartAgents(ctx)

	ts := httptest.NewServer(application.Handler())
	defer ts.Close()

	// Disparar el pipeline que tiene retries y timeouts
	reqBody := `{"operation": "intent_resiliency_demo", "lang": "es", "params": {"param1": "test"}}`
	resp, err := http.Post(ts.URL+"/ask_structured", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post /ask_structured: %v", err)
	}
	defer resp.Body.Close()

	// El pipeline deberia fallar por el timeout del paso slow (300ms > 100ms)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}

	var res map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		t.Fatalf("decode error response: %v", err)
	}

	// Verificamos que el error reportado sea un timeout. net/http lo redacta de dos formas, según
	// dónde lo detecte, y ninguna es parte de un contrato.
	errStr, ok := res["error"].(string)
	if !ok || !(strings.Contains(errStr, "context deadline exceeded") || strings.Contains(errStr, "Client.Timeout exceeded")) {
		t.Fatalf("expected timeout error, got %#v", res["error"])
	}

	// Además flaky tool debio haber sido llamado 3 veces (1 original + 2 retries)
	if got := atomic.LoadInt32(&flakyHits); got != 3 {
		t.Fatalf("flaky tool called %d times, want 3 (one initial call and two retries)", got)
	}
}
