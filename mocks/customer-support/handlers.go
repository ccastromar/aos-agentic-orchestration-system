package devops

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func RegisterHandlers(mux *http.ServeMux) {

	mux.HandleFunc("/mock/customer-support/case", handleCaseLookup)
	mux.HandleFunc("/mock/customer-support/purchase", handlePurchase)
	mux.HandleFunc("/mock/customer-support/return", handleProductReturn)

}

//
// /customer-support/case: the read-only lookup behind the governed support demo
//

type CaseResponse struct {
	Service   string `json:"service"`
	OpenCases int    `json:"openCases"`
	LastCase  string `json:"lastCase"`
	Channel   string `json:"channel"`
}

func handleCaseLookup(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")
	if service == "" {
		service = "unknown"
	}

	jsonResponse(w, CaseResponse{
		Service:   service,
		OpenCases: 2,
		LastCase:  "Customer cannot download the latest invoice",
		Channel:   "chat",
	})
}

//
// /customer-support/purchase
//

type PurchaseResponse struct {
	PurchaseID string  `json:"purchaseId"`
	ProductID  string  `json:"productId"`
	Date       string  `json:"date"`
	Amount     float64 `json:"amount"`
	Currency   string  `json:"currency"`
	Status     string  `json:"status"`
}

func handlePurchase(w http.ResponseWriter, r *http.Request) {
	purchaseId := r.URL.Query().Get("purchaseId")
	if purchaseId == "" {
		http.Error(w, "purchaseId required", http.StatusBadRequest)
		return
	}

	jsonResponse(w, PurchaseResponse{
		PurchaseID: purchaseId,
		ProductID:  "PROD-33",
		Date:       time.Now().AddDate(0, 0, -14).Format("2006-01-02"),
		Amount:     89.90,
		Currency:   "EUR",
		Status:     "delivered",
	})
}

//
// /customer-support/return: opens a return (RMA) for a purchased product
//

type ReturnResponse struct {
	ReturnID   string `json:"returnId"`
	PurchaseID string `json:"purchaseId"`
	ProductID  string `json:"productId"`
	Reason     string `json:"reason"`
	Status     string `json:"status"`
}

func handleProductReturn(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "only POST allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		PurchaseID string `json:"purchaseId"`
		ProductID  string `json:"productId"`
		Reason     string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PurchaseID == "" || req.ProductID == "" {
		http.Error(w, "purchaseId and productId required", http.StatusBadRequest)
		return
	}

	jsonResponse(w, ReturnResponse{
		ReturnID:   fmt.Sprintf("RMA-%06d", time.Now().UnixNano()%1_000_000),
		PurchaseID: req.PurchaseID,
		ProductID:  req.ProductID,
		Reason:     req.Reason,
		Status:     "accepted",
	})
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
