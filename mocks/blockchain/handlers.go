package blockchain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

type anchorRequest struct {
	TraceID       string `json:"traceId"`
	AuditHash     string `json:"auditHash"`
	OperationID   string `json:"operationId"`
	CustodyTxHash string `json:"custodyTxHash"`
}

type anchorReceipt struct {
	Status        string `json:"status"`
	ChainID       string `json:"chainId"`
	BlockNumber   uint64 `json:"blockNumber"`
	AnchorTxHash  string `json:"anchorTxHash"`
	AuditHash     string `json:"auditHash"`
	TraceID       string `json:"traceId"`
	OperationID   string `json:"operationId"`
	CustodyTxHash string `json:"custodyTxHash"`
}

var anchors = struct {
	sync.Mutex
	next    uint64
	receipt map[string]anchorReceipt
}{receipt: map[string]anchorReceipt{}}

// RegisterHandlers exposes a local blockchain anchoring simulator. It
// produces inspectable receipts but does not submit transactions to a chain.
func RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/mock/blockchain/anchors", handleAnchor)
	mux.HandleFunc("/mock/blockchain/anchors/", handleGetAnchor)
}

func handleAnchor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request anchorRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if request.TraceID == "" || request.OperationID == "" || len(request.AuditHash) != sha256.Size*2 {
		http.Error(w, "traceId, operationId, and 32-byte auditHash are required", http.StatusBadRequest)
		return
	}
	if _, err := hex.DecodeString(request.AuditHash); err != nil {
		http.Error(w, "auditHash must be hexadecimal", http.StatusBadRequest)
		return
	}
	anchors.Lock()
	anchors.next++
	blockNumber := anchors.next
	encoded, _ := json.Marshal(request)
	txDigest := sha256.Sum256([]byte(fmt.Sprintf("aos-mock-chain:%d:%s", blockNumber, encoded)))
	txHash := "0x" + hex.EncodeToString(txDigest[:])
	receipt := anchorReceipt{
		Status: "anchored", ChainID: "aos-mock-31337", BlockNumber: blockNumber,
		AnchorTxHash: txHash, AuditHash: strings.ToLower(request.AuditHash),
		TraceID: request.TraceID, OperationID: request.OperationID,
		CustodyTxHash: request.CustodyTxHash,
	}
	anchors.receipt[txHash] = receipt
	anchors.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(receipt)
}

func handleGetAnchor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	txHash := strings.TrimPrefix(r.URL.Path, "/mock/blockchain/anchors/")
	anchors.Lock()
	receipt, ok := anchors.receipt[txHash]
	anchors.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(receipt)
}
