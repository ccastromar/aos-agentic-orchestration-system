package custody

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
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
		resp, err := http.Post(req.CallbackURL, "application/json", strings.NewReader(string(body)))
		if err == nil && resp != nil {
			_ = resp.Body.Close()
		}
	}()
}
