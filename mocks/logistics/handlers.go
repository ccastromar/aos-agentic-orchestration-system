package devops

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"time"
)

func RegisterHandlers(mux *http.ServeMux) {

	mux.HandleFunc("/mock/logistics/customer", handleCustomer)
	mux.HandleFunc("/mock/logistics/shipment", handleShipment)

}

type ShipmentResponse struct {
	Service   string `json:"service"`
	Status    string `json:"status"`
	Uptime    string `json:"uptime"`
	Version   string `json:"version"`
	Instances int    `json:"instances"`
}

func handleShipment(w http.ResponseWriter, r *http.Request) {
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

type CustomerResponse struct {
	CustomerID string `json:"customerId"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	Tier       string `json:"tier"`
}

func handleCustomer(w http.ResponseWriter, r *http.Request) {
	customerId := r.URL.Query().Get("customerId")
	if customerId == "" {
		customerId = "unknown-customer"
	}

	time.Sleep(time.Duration(100+rand.Intn(200)) * time.Millisecond)

	resp := CustomerResponse{
		CustomerID: customerId,
		Name:       "Acme Corp Logistics",
		Email:      "contact@acme.corp",
		Tier:       "enterprise",
	}

	jsonResponse(w, resp)
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
