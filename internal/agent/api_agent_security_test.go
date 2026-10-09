package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/auth"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
)

func securityTestAgent() (*APIAgent, *state.StateManager, *bus.Bus, *bus.Bus) {
	executionBus, eventBus := bus.New(), bus.New()
	manager := state.NewStateManager(state.NewMemoryStore())
	return NewAPIAgent(executionBus, eventBus, manager), manager, executionBus, eventBus
}

func requestAsUser(method, target, userID string, body []byte) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	if userID != "" {
		req = req.WithContext(auth.WithIdentity(req.Context(), &auth.Identity{UserID: userID}))
	}
	return req
}

func TestHumanDecisionRequiresOwnerAndMatchingPendingGate(t *testing.T) {
	api, manager, executionBus, _ := securityTestAgent()
	if err := manager.Save(context.Background(), &state.ExecutionState{ID: "task-1", UserID: "alice", Gate: "manager_review"}); err != nil {
		t.Fatal(err)
	}
	decisions := make(chan bus.Message, 2)
	executionBus.Subscribe("verifier", decisions)

	cases := []struct {
		name, user, gate string
		wantStatus       int
	}{
		{name: "anonymous", gate: "manager_review", wantStatus: http.StatusUnauthorized},
		{name: "different owner", user: "bob", gate: "manager_review", wantStatus: http.StatusNotFound},
		{name: "wrong gate", user: "alice", gate: "other_gate", wantStatus: http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := requestAsUser(http.MethodPost, "/task/approve?id=task-1&gate="+tc.gate, tc.user, nil)
			rec := httptest.NewRecorder()
			api.handleHumanApprove(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
	select {
	case msg := <-decisions:
		t.Fatalf("unauthorized request reached verifier: %#v", msg)
	default:
	}

	req := requestAsUser(http.MethodPost, "/task/approve?id=task-1&gate=manager_review", "alice", nil)
	rec := httptest.NewRecorder()
	api.handleHumanApprove(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner decision status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	select {
	case msg := <-decisions:
		if msg.Payload["decision"] != "approved" || msg.Payload["id"] != "task-1" {
			t.Fatalf("unexpected decision payload: %#v", msg.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("authorized decision was not sent to verifier")
	}
}

func TestClarificationReplyRequiresOwnerAndClarificationGate(t *testing.T) {
	api, manager, executionBus, _ := securityTestAgent()
	if err := manager.Save(context.Background(), &state.ExecutionState{ID: "task-2", UserID: "alice", Gate: "clarification"}); err != nil {
		t.Fatal(err)
	}
	replies := make(chan bus.Message, 1)
	executionBus.Subscribe("planner", replies)
	body, _ := json.Marshal(map[string]string{"message": "The amount is 25"})

	for _, tc := range []struct {
		name, user string
		want       int
	}{
		{name: "anonymous", want: http.StatusUnauthorized},
		{name: "different owner", user: "bob", want: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := requestAsUser(http.MethodPost, "/task/reply?id=task-2", tc.user, body)
			rec := httptest.NewRecorder()
			api.handleClarifyReply(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
	select {
	case msg := <-replies:
		t.Fatalf("unauthorized request reached planner: %#v", msg)
	default:
	}

	req := requestAsUser(http.MethodPost, "/task/reply?id=task-2", "alice", body)
	rec := httptest.NewRecorder()
	api.handleClarifyReply(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("owner reply status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	select {
	case msg := <-replies:
		if msg.Payload["id"] != "task-2" || msg.Payload["message"] != "The amount is 25" {
			t.Fatalf("unexpected clarification payload: %#v", msg.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("authorized clarification was not sent to planner")
	}
}

func TestEventStreamRequiresOwnerAndOnlyEmitsRequestedTask(t *testing.T) {
	api, manager, _, eventBus := securityTestAgent()
	for _, task := range []state.ExecutionState{
		{ID: "task-alice", UserID: "alice"},
		{ID: "task-bob", UserID: "bob"},
	} {
		if err := manager.Save(context.Background(), &task); err != nil {
			t.Fatal(err)
		}
	}

	unauthorized := httptest.NewRecorder()
	api.handleEventStream(unauthorized, requestAsUser(http.MethodGet, "/api/v1/events/stream?task_id=task-alice", "bob", nil))
	if unauthorized.Code != http.StatusNotFound {
		t.Fatalf("foreign task stream status = %d, want 404", unauthorized.Code)
	}

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/stream?task_id=task-alice", nil)
	req = req.WithContext(auth.WithIdentity(ctx, &auth.Identity{UserID: "alice"}))
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		api.handleEventStream(rec, req)
		close(done)
	}()
	time.Sleep(10 * time.Millisecond)
	for i := 0; i < 5; i++ {
		eventBus.PublishEvent("task-bob", "test", "private", "other-user-event", "")
		eventBus.PublishEvent("task-alice", "test", "visible", "owner-event", "")
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("event stream did not stop after request cancellation")
	}
	if !strings.Contains(rec.Body.String(), "owner-event") {
		t.Fatal("stream did not include the authorized task event")
	}
	if strings.Contains(rec.Body.String(), "other-user-event") {
		t.Fatal("stream included an event for a different user's task")
	}
}
