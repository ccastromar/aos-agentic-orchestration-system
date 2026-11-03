package custody

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/audit"
)

type operationRequest struct {
	AssetID     string `json:"assetId"`
	Destination string `json:"destination"`
	Amount      string `json:"amount"`
	CallbackURL string `json:"callbackUrl"`
	Token       string `json:"callbackToken"`
}

// RegisterHandlers exposes a deterministic asynchronous custodial provider.
func RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/mock/custody/operations", handleSubmit)
}

func handleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req operationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	id := fmt.Sprintf("custody-%d", time.Now().UnixNano())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "accepted", "operationId": id})

	if req.CallbackURL == "" || req.Token == "" {
		return
	}
	go func() {
		time.Sleep(250 * time.Millisecond)
		payload := map[string]any{
			"event": "custody.operation.completed", "operationId": id, "status": "completed",
			"assetId": req.AssetID, "destination": req.Destination, "amount": req.Amount,
			"txHash": "mock-tx-" + strings.TrimPrefix(id, "custody-"),
		}
		body, _ := json.Marshal(payload)
		cb, err := http.NewRequest(http.MethodPost, req.CallbackURL, strings.NewReader(string(body)))
		if err != nil {
			return
		}
		cb.Header.Set("Content-Type", "application/json")
		// Signed like a real custodian would, when the mock shares a secret with AOS.
		if secret := os.Getenv("AOS_MOCK_CALLBACK_SECRET"); secret != "" {
			timestamp := strconv.FormatInt(time.Now().Unix(), 10)
			cb.Header.Set("X-AOS-Callback-Timestamp", timestamp)
			cb.Header.Set("X-AOS-Callback-Signature", audit.ProviderSignature(secret, timestamp, body))
		}
		resp, err := http.DefaultClient.Do(cb)
		if err == nil && resp != nil {
			_ = resp.Body.Close()
		}
	}()
}
