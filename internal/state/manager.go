package state

import (
	"context"
	"encoding/json"
	"time"
)

type Interaction struct {
	UserMessage string `json:"user_message"`
	Intent      string `json:"intent"`
	Summary     string `json:"summary"`
}

type SessionState struct {
	ID           string        `json:"id"`
	Interactions []Interaction `json:"interactions"`
}

type ReActStep struct {
	Thought     string `json:"thought,omitempty"`
	Action      string `json:"action,omitempty"`
	ActionInput string `json:"action_input,omitempty"`
	Observation string `json:"observation,omitempty"`
}

type ExecutionState struct {
	ID            string            `json:"id"`
	SessionID     string            `json:"session_id"`
	Intent        string            `json:"intent"`
	Pipeline      string            `json:"pipeline"`
	CompletedSteps []string          `json:"completed_steps,omitempty"` // IDs of completed steps
	Params        map[string]string `json:"params"`
	MissingParams []string          `json:"missing_params"`
	StepResults   map[string]any    `json:"step_results,omitempty"`
	ReActHistory  []ReActStep       `json:"react_history,omitempty"`
	Gate          string            `json:"gate"` // "clarification", "human_approval", etc.
	CreatedAt     time.Time         `json:"created_at"`
}

type StateStore interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, val string, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

type StateManager struct {
	store  StateStore
	vector VectorStore
}

func NewStateManager(store StateStore, vector ...VectorStore) *StateManager {
	var vs VectorStore
	if len(vector) > 0 {
		vs = vector[0]
	}
	return &StateManager{store: store, vector: vs}
}

func (sm *StateManager) Vector() VectorStore {
	return sm.vector
}

func (sm *StateManager) LoadSession(ctx context.Context, sessionID string) (*SessionState, error) {
	key := "session:" + sessionID
	val, err := sm.store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if val == "" {
		return &SessionState{ID: sessionID}, nil
	}
	var st SessionState
	if err := json.Unmarshal([]byte(val), &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (sm *StateManager) SaveSession(ctx context.Context, st *SessionState) error {
	// Limitar a los ultimos 5 turnos para evitar prompts enormes
	const maxTurns = 5
	if len(st.Interactions) > maxTurns {
		st.Interactions = st.Interactions[len(st.Interactions)-maxTurns:]
	}

	key := "session:" + st.ID
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return sm.store.Set(ctx, key, string(b), 24*time.Hour) // Las sesiones expiran en 24h
}

func (m *StateManager) Save(ctx context.Context, st *ExecutionState) error {
	if st.CreatedAt.IsZero() {
		st.CreatedAt = time.Now().UTC()
	}
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return m.store.Set(ctx, st.ID, string(b), 0)
}

func (m *StateManager) Load(ctx context.Context, id string) (*ExecutionState, error) {
	val, err := m.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if val == "" {
		return nil, nil // Not found
	}
	var st ExecutionState
	if err := json.Unmarshal([]byte(val), &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (m *StateManager) Delete(ctx context.Context, id string) error {
	return m.store.Delete(ctx, id)
}
