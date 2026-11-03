package crm

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
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

	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil || days <= 0 {
		days = 30
	}

	// Interactions at a fixed age; only those inside the requested window are returned.
	history := []struct {
		daysAgo              int
		kind, agent, summary string
	}{
		{3, "email", "Sofia", "Request for latest order status."},
		{10, "call", "Miguel", "Question about the next invoice and available payment methods."},
		{25, "ticket", "A Team", "Issue with a return, resolved satisfactorily."},
		{60, "email", "Sofia", "Change of billing address."},
	}
	items := []map[string]any{}
	for _, h := range history {
		if h.daysAgo <= days {
			items = append(items, map[string]any{
				"date":    time.Now().AddDate(0, 0, -h.daysAgo).Format("2006-01-02"),
				"type":    h.kind,
				"agent":   h.agent,
				"summary": h.summary,
			})
		}
	}

	resp := map[string]any{
		"customerId": customerId,
		"windowDays": days,
		"items":      items,
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
