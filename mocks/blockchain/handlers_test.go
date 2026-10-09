package blockchain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnchorCreatesInspectableReceipt(t *testing.T) {
	mux := http.NewServeMux()
	RegisterHandlers(mux)
	body := []byte(fmt.Sprintf(`{"traceId":"trace-1","auditHash":"%064x","operationId":"op-1","custodyTxHash":"mock-custody-tx"}`, 42))
	req := httptest.NewRequest(http.MethodPost, "/mock/blockchain/anchors", bytes.NewReader(body))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}
	var receipt anchorReceipt
	if err := json.Unmarshal(res.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Status != "anchored" || receipt.AuditHash != fmt.Sprintf("%064x", 42) || receipt.AnchorTxHash == "" {
		t.Fatalf("unexpected anchor receipt: %#v", receipt)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/mock/blockchain/anchors/"+receipt.AnchorTxHash, nil)
	getRes := httptest.NewRecorder()
	mux.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected to retrieve anchor receipt, got %d", getRes.Code)
	}
}

func TestAnchorRejectsMalformedHash(t *testing.T) {
	mux := http.NewServeMux()
	RegisterHandlers(mux)
	req := httptest.NewRequest(http.MethodPost, "/mock/blockchain/anchors", bytes.NewBufferString(`{"traceId":"trace-1","auditHash":"bad","operationId":"op-1"}`))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}
