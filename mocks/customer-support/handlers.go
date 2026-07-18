package devops

import (
	"encoding/json"
	"net/http"
)

func RegisterHandlers(mux *http.ServeMux) {

	mux.HandleFunc("/mock/customer-support/purchase", handlePurchase)
	mux.HandleFunc("/mock/customer-support/return", handleProductReturn)

}

//
// /devops/status
//

type ProductResponse struct {
	Service   string `json:"service"`
	Status    string `json:"status"`
	Uptime    string `json:"uptime"`
	Version   string `json:"version"`
	Instances int    `json:"instances"`
}

func handlePurchase(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")
	if service == "" {
		service = "unknown"
	}

	response := map[string]any{
		"service":   service,
		"status":    "running",
		"instances": 3,
		"uptime":    "32h",
		"version":   "1.4.2",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func handleProductReturn(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")
	if service == "" {
		service = "unknown"
	}

	response := map[string]any{
		"service":   service,
		"status":    "running",
		"instances": 3,
		"uptime":    "32h",
		"version":   "1.4.2",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

//
// helpers
//

func jsonResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}
