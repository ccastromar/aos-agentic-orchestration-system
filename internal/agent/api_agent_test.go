package agent

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/ui"
	"github.com/stretchr/testify/require"
)

func TestAPIAgent_HandleAsk_AsyncAndTaskFetch(t *testing.T) {
	// Setup dependencies
	messageBus := bus.New()
	uiStore := ui.NewUIStore()
	apiAgent := NewAPIAgent(messageBus, uiStore)

	// Subscribe to inspector channel to intercept the message
	inspectorChan := make(chan bus.Message, 1)
	messageBus.Subscribe("inspector", inspectorChan)

	// Setup HTTP server
	mux := http.NewServeMux()
	apiAgent.RegisterHTTP(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Prepare request
	reqBody := map[string]string{
		"message": "test message",
	}
	bodyBytes, _ := json.Marshal(reqBody)

	// Start a goroutine to simulate the backend processing
	go func() {
		select {
		case msg := <-inspectorChan:
			// Verify message content
			payload := msg.Payload
			if payload == nil {
				return
			}
			id, ok := payload["id"].(string)
			if !ok {
				return
			}

			// Simulate processing time
			time.Sleep(50 * time.Millisecond)

			// Store result
			storeResult(id, Result{
				Status: "completed",
				Data:   map[string]string{"reply": "processed"},
			})
		case <-time.After(2 * time.Second):
			// Timeout in test helper
		}
	}()

    // Execute async request
    resp, err := http.Post(ts.URL+"/ask", "application/json", bytes.NewBuffer(bodyBytes))
    require.NoError(t, err)
    defer resp.Body.Close()

    // Verify immediate async response
    require.Equal(t, http.StatusAccepted, resp.StatusCode)

    var accepted map[string]any
    err = json.NewDecoder(resp.Body).Decode(&accepted)
    require.NoError(t, err)
    require.Equal(t, "accepted", accepted["status"])
    id, _ := accepted["id"].(string)
    if id == "" { t.Fatalf("expected id in async response") }

    // Initially should be pending
    r1, err := http.Get(ts.URL+"/task?id="+id)
    require.NoError(t, err)
    defer r1.Body.Close()
    require.Equal(t, http.StatusOK, r1.StatusCode)
    var pending map[string]any
    require.NoError(t, json.NewDecoder(r1.Body).Decode(&pending))
    require.Equal(t, "pending", pending["status"])

    // Allow backend goroutine to store the result, then fetch again
    time.Sleep(100 * time.Millisecond)
    r2, err := http.Get(ts.URL+"/task?id="+id)
    require.NoError(t, err)
    defer r2.Body.Close()
    require.Equal(t, http.StatusOK, r2.StatusCode)
    var done map[string]any
    require.NoError(t, json.NewDecoder(r2.Body).Decode(&done))
    require.Equal(t, id, done["id"])
    require.Equal(t, "completed", done["status"])
    data, ok := done["data"].(map[string]any)
    require.True(t, ok)
    require.Equal(t, "processed", data["reply"])
}
