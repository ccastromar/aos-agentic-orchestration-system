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
	ShipmentID   string `json:"shipmentId"`
	Status       string `json:"status"`
	Carrier      string `json:"carrier"`
	LastLocation string `json:"lastLocation"`
	EstimatedAt  string `json:"estimatedDelivery"`
}

func handleShipment(w http.ResponseWriter, r *http.Request) {
	shipmentId := r.URL.Query().Get("shipmentId")
	if shipmentId == "" {
		shipmentId = "unknown-shipment"
	}

	resp := ShipmentResponse{
		ShipmentID:   shipmentId,
		Status:       "in_transit",
		Carrier:      "Acme Freight",
		LastLocation: "Zaragoza hub",
		EstimatedAt:  time.Now().AddDate(0, 0, 2).Format("2006-01-02"),
	}

	jsonResponse(w, resp)
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
