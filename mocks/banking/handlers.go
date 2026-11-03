package banking

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

func RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/mock/core/balance", getBalance)
	mux.HandleFunc("/mock/core/movements", getMovements)
	mux.HandleFunc("/mock/core/creditcard", getCreditCard)
	mux.HandleFunc("/mock/payments/bizum", postBizumPayment)
	mux.HandleFunc("/mock/payments/transfer", postTransfer)
	mux.HandleFunc("/mock/payments/bitcoin", postBitcoinPayment)
	mux.HandleFunc("/mock/aml/check", postAmlCheck)
	mux.HandleFunc("/mock/notifications/send", postSendNotification)
	mux.HandleFunc("/mock/flaky", postFlaky)
	mux.HandleFunc("/mock/slow", postSlow)
}

func getBalance(w http.ResponseWriter, r *http.Request) {
	log.Println("MOCK URL:", r.URL.String())
	log.Println("MOCK QUERY:", r.URL.Query())

	accountId := r.URL.Query().Get("accountId")
	resp := map[string]any{
		"accountId": accountId,
		"currency":  "EUR",
		"balance":   15.56,
	}
	json.NewEncoder(w).Encode(resp)
}

func getCreditCard(w http.ResponseWriter, r *http.Request) {
	log.Println("MOCK URL:", r.URL.String())
	log.Println("MOCK QUERY:", r.URL.Query())

	cardId := r.URL.Query().Get("cardId")
	resp := map[string]any{
		"cardId":      cardId,
		"currency":    "EUR",
		"current":     1000,
		"outstanding": 100,
	}
	json.NewEncoder(w).Encode(resp)
}

// getMovements returns two movements inside the requested from/to window (the last 30 days when
// the window is missing or not a pair of dates).
func getMovements(w http.ResponseWriter, r *http.Request) {
	accountId := r.URL.Query().Get("accountId")
	to, errTo := time.Parse("2006-01-02", r.URL.Query().Get("to"))
	from, errFrom := time.Parse("2006-01-02", r.URL.Query().Get("from"))
	if errTo != nil || errFrom != nil || from.After(to) {
		to = time.Now()
		from = to.AddDate(0, 0, -30)
	}
	mid := from.Add(to.Sub(from) / 2)
	resp := map[string]any{
		"accountId": accountId,
		"currency":  "EUR",
		"from":      from.Format("2006-01-02"),
		"to":        to.Format("2006-01-02"),
		"movements": []map[string]any{
			{
				"date":   to.Format("2006-01-02"),
				"amount": -25.00,
				"desc":   "Bizum Laura",
			},
			{
				"date":   mid.Format("2006-01-02"),
				"amount": 1500.00,
				"desc":   "Deposit",
			},
		},
	}
	json.NewEncoder(w).Encode(resp)
}

func postBizumPayment(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	resp := map[string]any{
		"status": "ok",
		"detail": body,
	}
	json.NewEncoder(w).Encode(resp)
}

func postTransfer(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)

	// Simulate bad request
	//w.WriteHeader(http.StatusBadRequest)
	//w.WriteHeader(http.StatusUnauthorized)
	resp := map[string]any{
		"status":  "ok",
		"detail":  body,
		"message": "Done",
	}

	//resp := map[string]any{
	//	"status": "ok",
	//	"detail": body,
	//}
	json.NewEncoder(w).Encode(resp)
}

func postBitcoinPayment(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	resp := map[string]any{
		"status": "ok",
		"detail": body,
	}
	json.NewEncoder(w).Encode(resp)
}

func postAmlCheck(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	resp := map[string]any{
		"riskScore":  12,
		"riskLevel":  "LOW",
		"sanctioned": false,
	}
	json.NewEncoder(w).Encode(resp)
}

func postSendNotification(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	log.Println("[MOCK NOTIF]", body)
	resp := map[string]any{
		"sent": true,
	}
	json.NewEncoder(w).Encode(resp)
}
