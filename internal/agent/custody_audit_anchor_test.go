package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/audit"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
	mockBlockchain "github.com/ccastromar/aos-agentic-orchestration-system/mocks/blockchain"
	"github.com/stretchr/testify/require"
)

func TestCustodyCallbackAnchorsCurrentAuditHash(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/submit", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "accepted", "operationId": "custody-op-1"})
	})
	mockBlockchain.RegisterHandlers(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	chain, err := audit.NewFileChain(auditPath)
	require.NoError(t, err)
	defer chain.Close()
	chain.Log(audit.Event{TraceID: "trace-anchor", Phase: "intent.rbac_allowed", Result: "success"})

	pipe := config.Pipeline{Name: "custody-anchor-test", Mode: "linear", Steps: []config.PipelineStep{
		{ID: "submit", Tool: "custody.submit", WithParams: map[string]string{"callbackUrl": "{{ .callback_url }}", "callbackToken": "{{ .callback_token }}"}},
		{ID: "wait_callback", Type: "await_callback", TimeoutMs: 60000},
		{ID: "anchor", Tool: "blockchain.anchor", When: `audit_hash != ""`, WithParams: map[string]string{
			"traceId": "{{ .trace_id }}", "auditHash": "{{ .audit_hash }}",
			"operationId":   "{{ index . \"wait_callback.operationId\" }}",
			"custodyTxHash": "{{ index . \"wait_callback.txHash\" }}",
		}},
		{ID: "summarize", Analyst: true},
	}}
	cfg := &config.Config{Pipelines: map[string]config.Pipeline{pipe.Name: pipe}, Tools: map[string]config.Tool{
		"custody.submit": {Name: "custody.submit", Type: "http", Method: http.MethodPost, URL: server.URL + "/submit", TimeoutMs: 1000,
			Body: map[string]string{"callbackUrl": "{{ .callbackUrl }}", "callbackToken": "{{ .callbackToken }}"}},
		"blockchain.anchor": {Name: "blockchain.anchor", Type: "http", Method: http.MethodPost, URL: server.URL + "/mock/blockchain/anchors", TimeoutMs: 1000,
			Body: map[string]string{"traceId": "{{ .traceId }}", "auditHash": "{{ .auditHash }}", "operationId": "{{ .operationId }}", "custodyTxHash": "{{ .custodyTxHash }}"}},
	}}
	messageBus := bus.New()
	analystInbox := make(chan bus.Message, 1)
	messageBus.Subscribe("analyst", analystInbox)
	sm := state.NewStateManager(state.NewMemoryStore())
	require.NoError(t, sm.Save(context.Background(), &state.ExecutionState{
		ID: "trace-anchor", UserID: "user-1", Intent: "custody.transfer_async", Pipeline: pipe.Name,
		Params: map[string]string{"assetId": "BTC-001", "amount": "0.5", "destination": "wallet-demo"},
	}))
	v := NewVerifier(messageBus, cfg, nil, bus.New(), sm)
	v.SetAuditLogger(chain)

	// Record the pipeline manifest first, as the run_pipeline handler does before executing.
	// Skipping it makes the callback resume record pipeline.started after callback.received,
	// so the anchor would cover that late manifest record instead of the callback.
	existing, err := sm.Load(context.Background(), "trace-anchor")
	require.NoError(t, err)
	_, err = v.ensurePipelineManifest("trace-anchor", "session-1", "custody.transfer_async", pipe, map[string]any{}, existing)
	require.NoError(t, err)

	executeLinear(v, pipe, "trace-anchor", "session-1", "custody.transfer_async", "es", context.Background(), map[string]any{}, map[string]any{})
	waiting, err := sm.Load(context.Background(), "trace-anchor")
	require.NoError(t, err)
	require.Equal(t, "await_callback", waiting.Gate)
	require.NotEmpty(t, waiting.CallbackToken)

	v.handleCallback(bus.Message{Type: "callback_received", Payload: map[string]any{
		"token": waiting.CallbackToken,
		"data":  map[string]any{"status": "completed", "operationId": "custody-op-1", "txHash": "mock-tx-1"},
	}})

	select {
	case msg := <-analystInbox:
		raw := msg.Payload["rawResult"].(map[string]any)
		anchor := raw["blockchain.anchor"].(map[string]any)
		require.Equal(t, "anchored", anchor["status"])
		require.Equal(t, "custody-op-1", anchor["operationId"])
		require.Equal(t, "mock-tx-1", anchor["custodyTxHash"])
		file, err := os.Open(auditPath)
		require.NoError(t, err)
		scanner := bufio.NewScanner(file)
		var callbackHash string
		for scanner.Scan() {
			var record audit.Record
			require.NoError(t, json.Unmarshal(scanner.Bytes(), &record))
			if record.Event.Phase == "callback.received" {
				callbackHash = record.Hash
			}
		}
		require.NoError(t, scanner.Err())
		require.NoError(t, file.Close())
		require.NotEmpty(t, callbackHash)
		require.Equal(t, callbackHash, anchor["auditHash"])
	case <-time.After(time.Second):
		t.Fatal("custody flow did not advance to audit anchoring")
	}
	verified, err := audit.VerifyFile(auditPath)
	require.NoError(t, err)
	require.True(t, verified.Valid)
}
