package devops

import (
	"encoding/json"
	"net/http"
)

func RegisterHandlers(mux *http.ServeMux) {

	mux.HandleFunc("/mock/human-resources/vacation", handleVacations)

}

type VacationsResponse struct {
	Employee string `json:"employee"`
	Status   string `json:"status"`
	Uptime   string `json:"uptime"`
	Version  string `json:"version"`
	Days     int    `json:"days"`
}

func handleVacations(w http.ResponseWriter, r *http.Request) {
	employee := r.URL.Query().Get("employeeId")
	if employee == "" {
		employee = "unknown"
	}

	response := map[string]any{
		"employee": employee,
		"status":   "running",
		"days":     3,
		"uptime":   "32h",
		"version":  "1.4.2",
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
