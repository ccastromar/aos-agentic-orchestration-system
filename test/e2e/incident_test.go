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

func TestE2E_IncidentResponse(t *testing.T) {
	chdirToRepoRoot(t)

	// 1. Iniciar App
	application, err := app.New()
	assert.NoError(t, err)

	// 2. Fake backend para todos los dominios
	mux9000 := http.NewServeMux()
	mux9000.HandleFunc("/mock/", func(w http.ResponseWriter, r *http.Request) {
		// Retornamos OK para cualquier endpoint (devops, crm, helpdesk)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "status": "ok", "message": "mocked"})
	})
	srv9000 := &http.Server{Addr: "localhost:9000", Handler: mux9000}
	go func() { _ = srv9000.ListenAndServe() }()

	client := &http.Client{Timeout: 100 * time.Millisecond}
	ready := false
	for i := 0; i < 50; i++ {
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	application.StartAgents(ctx)

	ts := httptest.NewServer(application.Handler())
	defer ts.Close()

	// 3. Disparar el pipeline usando ask_structured
	reqBody := `{"operation": "incident.resolve_outage", "lang": "en", "params": {"customerId": "VIP123", "serviceName": "api"}}`
	resp, err := http.Post(ts.URL+"/ask_structured", "application/json", strings.NewReader(reqBody))
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var askRes map[string]any
	json.NewDecoder(resp.Body).Decode(&askRes)
	id := askRes["id"].(string)

	// 4. Wait for it to reach human gate
	time.Sleep(1 * time.Second)

	resp2, _ := http.Get(ts.URL + "/task?id=" + id)
	var taskRes map[string]any
	json.NewDecoder(resp2.Body).Decode(&taskRes)

	assert.Equal(t, "await_human", taskRes["status"])

	// 5. Approve the gate
	gate := "sre_lead_approval"
	resp3, err := http.Post(ts.URL+"/task/approve?id="+id+"&gate="+gate, "application/json", nil)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp3.StatusCode)

	// 6. Wait for completion
	time.Sleep(1 * time.Second)
	resp4, _ := http.Get(ts.URL + "/task?id=" + id)
	var finalRes map[string]any
	json.NewDecoder(resp4.Body).Decode(&finalRes)

	assert.Equal(t, "ok", finalRes["status"])
}
