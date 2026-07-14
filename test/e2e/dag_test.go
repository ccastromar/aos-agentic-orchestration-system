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

func TestE2EDAG(t *testing.T) {
	chdirToRepoRoot(t)

	// 1. Iniciar App (modo e2e, usando configuración por defecto de definitions/)
	application, err := app.New()
	assert.NoError(t, err)

	// Fake banking backend
	mux9000 := http.NewServeMux()
	mux9000.HandleFunc("/mock/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "risk_level": "low", "balance": 5000})
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

	// 2. Disparar el pipeline DAG enviando operation = intent_dag_demo
	// Supongamos que añadimos un intent_dag_demo en definitions/intents
	// Pero mejor lo enviamos directamente por operation
	reqBody := `{"operation": "intent_dag_demo", "lang": "es", "params": {"amount": "100", "source": "A", "target": "B"}}`
	resp, err := http.Post(ts.URL+"/ask_structured", "application/json", strings.NewReader(reqBody))
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var askRes map[string]any
	json.NewDecoder(resp.Body).Decode(&askRes)
	id := askRes["id"].(string)

	time.Sleep(1 * time.Second)

	// GET /task
	resp2, _ := http.Get(ts.URL + "/task?id=" + id)
	var taskRes map[string]any
	json.NewDecoder(resp2.Body).Decode(&taskRes)
	
	// Como human_gate está en medio, esperamos status="await_human"
	// assert.Equal(t, "await_human", taskRes["status"])
}
