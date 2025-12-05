package banking

import (
	"encoding/json"
	"log"
	"net/http"
)

func RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/mock/core/balance", getBalance)
	mux.HandleFunc("/mock/core/movements", getMovements)
	mux.HandleFunc("/mock/core/creditcard", getCreditCard)
	mux.HandleFunc("/mock/payments/bizum", postBizumPayment)
	mux.HandleFunc("/mock/payments/bitcoin", postBitcoinPayment)
	mux.HandleFunc("/mock/aml/check", postAmlCheck)
	mux.HandleFunc("/mock/notifications/send", postSendNotification)
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

func getMovements(w http.ResponseWriter, r *http.Request) {
	accountId := r.URL.Query().Get("accountId")
	resp := map[string]any{
		"accountId": accountId,
		"currency":  "EUR",
		"movements": []map[string]any{
			{
				"date":   "2025-01-10",
				"amount": -25.00,
				"desc":   "Bizum Laura",
			},
			{
				"date":   "2025-01-05",
				"amount": 1500.00,
				"desc":   "Nómina",
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
