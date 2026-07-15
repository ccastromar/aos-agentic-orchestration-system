package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/app"
	"github.com/stretchr/testify/assert"
)

func TestE2E_HumanGateResume(t *testing.T) {
	chdirToRepoRoot(t)

	// 1. Start App
	application, err := app.New()
	assert.NoError(t, err)

	// 2. Fake backend
	mux9000 := http.NewServeMux()
	var callCount int
	var mu sync.Mutex
	mux9000.HandleFunc("/mock/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "ping") {
			mu.Lock()
			callCount++
			mu.Unlock()
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "risk_level": "low", "balance": 5000})
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

	// 3. Trigger DAG demo pipeline
	reqBody := `{"operation": "intent_dag_demo", "lang": "es", "params": {"amount": "100", "source": "A", "target": "B"}}`
	resp, err := http.Post(ts.URL+"/ask_structured", "application/json", strings.NewReader(reqBody))
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var askRes map[string]any
	json.NewDecoder(resp.Body).Decode(&askRes)
	id := askRes["id"].(string)

	// 4. Wait for it to hit the human gate
	time.Sleep(1 * time.Second)

	resp2, _ := http.Get(ts.URL + "/task?id=" + id)
	var taskRes map[string]any
	json.NewDecoder(resp2.Body).Decode(&taskRes)

	assert.Equal(t, "await_human", taskRes["status"])
	
	// Ensure that initial steps ran exactly once (check_risk, check_balance)
	assert.Equal(t, 2, callCount, "Expected exactly 2 mock API calls before gate")

	// 5. Approve the gate
	gate := "manager_review"
	approveResp, err := http.Post(ts.URL+"/task/approve?id="+id+"&gate="+gate, "application/json", nil)
	assert.NoError(t, err)
	assert.Equal(t, 200, approveResp.StatusCode)

	// 6. Wait for execution to finish
	time.Sleep(2 * time.Second)

	resp3, _ := http.Get(ts.URL + "/task?id=" + id)
	var taskResFinal map[string]any
	json.NewDecoder(resp3.Body).Decode(&taskResFinal)

	assert.Equal(t, "ok", taskResFinal["status"], "Pipeline should complete with status ok")
	
	// 7. Verify that tools BEFORE the gate were NOT executed again, but tools AFTER the gate were.
	// Initial tools: check_risk, check_balance (2 calls)
	// Post-gate tools: execute_transfer, notify (2 calls)
	// Total expected mock API calls = 4
	assert.Equal(t, 4, callCount, "Expected exactly 4 total mock API calls (2 before gate, 2 after gate)")
}
