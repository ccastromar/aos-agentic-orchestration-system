package crm

import (
	"encoding/json"
	"net/http"
)

func RegisterHandlers(mux *http.ServeMux) {

	mux.HandleFunc("/mock/crm/customer", handleCustomerProfile)
	mux.HandleFunc("/mock/crm/interactions", handleCustomerInteractions)
	mux.HandleFunc("/mock/crm/ticket", handleCreateTicket)
	mux.HandleFunc("/mock/crm/lead/status", handleUpdateLeadStatus)
}

func handleCustomerProfile(w http.ResponseWriter, r *http.Request) {
	customerId := r.URL.Query().Get("customerId")
	if customerId == "" {
		http.Error(w, "customerId requerido", http.StatusBadRequest)
		return
	}

	resp := map[string]any{
		"customerId":    customerId,
		"name":          "Laura Fernández",
		"segment":       "Gold",
		"email":         "laura.fernandez@example.com",
		"lastPurchase":  "2025-10-10",
		"lifetimeValue": 12450.75,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handleCustomerInteractions(w http.ResponseWriter, r *http.Request) {
	customerId := r.URL.Query().Get("customerId")
	if customerId == "" {
		http.Error(w, "customerId required", http.StatusBadRequest)
		return
	}

	days := r.URL.Query().Get("days")
	if days == "" {
		days = "30"
	}

	resp := map[string]any{
		"customerId": customerId,
		"windowDays": days,
		"items": []map[string]any{
			{
				"date":    "2025-11-15",
				"type":    "email",
				"agent":   "Sofia",
				"summary": "Request for latest order status.",
			},
			{
				"date":    "2025-11-08",
				"type":    "call",
				"agent":   "Miguel",
				"summary": "Question about the next invoice and available payment methods.",
			},
			{
				"date":    "2025-10-30",
				"type":    "ticket",
				"agent":   "A Team",
				"summary": "Issue with a return, resolved satisfactorily.",
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handleCreateTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "body is not valid", http.StatusBadRequest)
		return
	}

	resp := map[string]any{
		"ticketId":   "TCK-12345",
		"status":     "OPEN",
		"customerId": payload["customerId"],
		"subject":    payload["subject"],
		"priority":   payload["priority"],
		"channel":    payload["channel"],
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handleUpdateLeadStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "body is not valid", http.StatusBadRequest)
		return
	}

	resp := map[string]any{
		"leadId":    payload["leadId"],
		"newStatus": payload["newStatus"],
		"updated":   true,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
