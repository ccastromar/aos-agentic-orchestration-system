package devops

import (
	"encoding/json"
	"net/http"
)

func RegisterHandlers(mux *http.ServeMux) {

	mux.HandleFunc("/mock/human-resources/vacation", handleVacations)

}

type VacationsResponse struct {
	EmployeeID    string `json:"employeeId"`
	AnnualDays    int    `json:"annualDays"`
	UsedDays      int    `json:"usedDays"`
	AvailableDays int    `json:"availableDays"`
	PendingDays   int    `json:"pendingApprovalDays"`
}

func handleVacations(w http.ResponseWriter, r *http.Request) {
	employee := r.URL.Query().Get("employeeId")
	if employee == "" {
		employee = "unknown"
	}

	response := VacationsResponse{
		EmployeeID:    employee,
		AnnualDays:    23,
		UsedDays:      17,
		AvailableDays: 3,
		PendingDays:   3,
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
