package main

import (
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/mock/aml/check", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"risk_score": 10, "status": "APPROVED"}`))
	})

	mux.HandleFunc("/mock/core/balance", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"balance": 1000, "currency": "EUR"}`))
	})

	// catch-all for any other mock endpoints
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status": "ok", "mocked": true}`))
	})

	log.Println("Starting Mock Server on :19001")
	if err := http.ListenAndServe(":19001", mux); err != nil {
		log.Fatalf("Mock server failed: %v", err)
	}
}
