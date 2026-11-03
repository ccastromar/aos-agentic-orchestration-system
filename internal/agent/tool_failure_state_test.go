package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/tools"
)

// A failed run must read as failed on every /task, not only the first: the in-memory result is
// consumed by the first read, so the outcome has to live in the task's state too.
func TestToolFailureOutlivesTheFirstRead(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus string
	}{
		{"rejection", &tools.RejectedError{Reason: "high risk"}, "rejected"},
		{"fault", errors.New("connection refused"), "error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api, manager, _, _ := securityTestAgent()
			v := NewVerifier(bus.New(), &config.Config{}, nil, bus.New(), manager)
			id := "task-" + tc.name
			if err := manager.Save(context.Background(), &state.ExecutionState{ID: id, UserID: "alice", Status: "processing"}); err != nil {
				t.Fatal(err)
			}

			v.failTool(nil, id, "compliance_review", "compliance.a2a_review", tc.err)

			for read := 1; read <= 2; read++ {
				rec := httptest.NewRecorder()
				api.handleTask(rec, requestWithFunctions(http.MethodGet, "/task?id="+id, "alice"))
				var got map[string]any
				_ = json.NewDecoder(rec.Body).Decode(&got)
				if got["status"] != tc.wantStatus {
					t.Fatalf("read %d: status = %v, want %s (%v)", read, got["status"], tc.wantStatus, got)
				}
				if tc.wantStatus == "rejected" {
					data, _ := got["data"].(map[string]any)
					if data["reason"] != "high risk" || data["step"] != "compliance_review" {
						t.Fatalf("read %d: data = %v", read, got["data"])
					}
				}
			}
		})
	}
}
