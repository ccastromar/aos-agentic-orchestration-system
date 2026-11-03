package agent

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A task id AOS never issued must not look like work in progress, nor expose an in-memory
// result that has no owner to check it against.
func TestUnknownTaskIsNotFound(t *testing.T) {
	api, _, _, _ := securityTestAgent()
	storeResult("never-issued", Result{Status: "ok", Data: map[string]any{"secret": true}})
	defer deleteResult("never-issued")

	rec := httptest.NewRecorder()
	api.handleTask(rec, requestWithFunctions(http.MethodGet, "/task?id=never-issued", "mallory"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body %s", rec.Code, rec.Body.String())
	}
}
