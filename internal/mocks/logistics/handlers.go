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
	Service string `json:"service"`
	Result  string `json:"result"`
	Message string `json:"message"`
	Took    string `json:"took"`
}

func handleCustomer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	service := r.Form.Get("service")
	if service == "" {
		service = "unknown-service"
	}

	// Simulamos tiempo de reinicio
	time.Sleep(time.Duration(500+rand.Intn(300)) * time.Millisecond)

	resp := CustomerResponse{
		Service: service,
		Result:  "ok",
		Message: "service restarted successfully",
		Took:    "750ms",
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
