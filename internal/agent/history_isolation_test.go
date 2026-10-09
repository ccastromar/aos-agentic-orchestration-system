package agent

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/auth"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/state"
)

func TestHistoryAndMemoriesRequireOwner(t *testing.T) {
	sm := state.NewStateManager(state.NewMemoryStore(), state.NewMemoryVectorStore())
	for _, user := range []string{"alice", "bob"} {
		sm.Save(context.Background(), &state.ExecutionState{ID: user, UserID: user})
	}
	a := NewAPIAgent(bus.New(), sm)
	for _, user := range []string{"", "alice", "bob", "empty"} {
		r := httptest.NewRequest("GET", "/api/v1/tasks", nil)
		if user != "" {
			r = r.WithContext(auth.WithIdentity(r.Context(), &auth.Identity{UserID: user}))
		}
		w := httptest.NewRecorder()
		a.HandleListTasks(w, r)
		if user == "" {
			if w.Code != 401 {
				t.Fatal("unauthenticated history exposed")
			}
			continue
		}
		var got []state.ExecutionState
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if user == "empty" {
			if w.Body.String() != "[]\n" {
				t.Fatal("empty history must be an array")
			}
			continue
		}
		if len(got) != 1 || got[0].UserID != user {
			t.Fatalf("history leaked: %s", w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	a.handleMemories(w, httptest.NewRequest("GET", "/api/v1/memories", nil))
	if w.Code != 401 {
		t.Fatal("unauthenticated memory access accepted")
	}
}

func TestAskCannotSelectAnotherUsersMemory(t *testing.T) {
	a := NewAPIAgent(bus.New())
	r := httptest.NewRequest("POST", "/ask", strings.NewReader(`{"message":"hello","session_id":"bob"}`))
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(auth.WithIdentity(r.Context(), &auth.Identity{UserID: "alice"}))
	w := httptest.NewRecorder()
	a.handleAsk(w, r)
	var got map[string]any
	json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 202 || got["session_id"] != "alice" {
		t.Fatalf("wrong scope: %s", w.Body.String())
	}
}

type failingMemoryStore struct{ state.VectorStore }

func (f failingMemoryStore) ClearMemories(context.Context, string) error { return context.Canceled }
func (f failingMemoryStore) ListMemories(context.Context, string) ([]state.Memory, error) {
	return nil, context.Canceled
}
func TestMemoryToolsPropagateFailures(t *testing.T) {
	v := &Verifier{stateManager: state.NewStateManager(state.NewMemoryStore(), failingMemoryStore{})}
	for _, tool := range []string{"system.memory.clear", "system.memory.list", "system.memory.delete"} {
		out, err := v.executeMemoryTool(context.Background(), tool, "alice", map[string]any{"fact_to_forget": "fact"})
		if err == nil || out != nil {
			t.Fatalf("%s reported success: %v", tool, out)
		}
	}
}

func TestHistoricalTaskRejectsAnotherUser(t *testing.T) {
	sm := state.NewStateManager(state.NewMemoryStore())
	if err := sm.Save(context.Background(), &state.ExecutionState{ID: "historical-task", UserID: "alice"}); err != nil {
		t.Fatal(err)
	}
	a := NewAPIAgent(bus.New(), sm)
	for _, user := range []string{"alice", "bob", ""} {
		r := httptest.NewRequest("GET", "/task?id=historical-task", nil)
		if user != "" {
			r = r.WithContext(auth.WithIdentity(r.Context(), &auth.Identity{UserID: user}))
		}
		w := httptest.NewRecorder()
		a.handleTask(w, r)
		want := 404
		if user == "alice" {
			want = 200
		}
		if w.Code != want {
			t.Fatalf("%q got %d: %s", user, w.Code, w.Body.String())
		}
	}
}

func (f failingMemoryStore) AddMemory(context.Context, state.Memory) error { return context.Canceled }
func TestMemoryAddPropagatesStoreFailure(t *testing.T) {
	v := &Verifier{stateManager: state.NewStateManager(state.NewMemoryStore(), failingMemoryStore{}), llmClient: llmNewTaskDummy{}}
	out, err := v.executeMemoryTool(context.Background(), "system.memory.add", "alice", map[string]any{"fact": "A fact"})
	if err == nil || out != nil {
		t.Fatal("failed write reported as success")
	}
}
