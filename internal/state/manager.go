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
	Thought     string           `json:"thought,omitempty"`
	Action      string           `json:"action,omitempty"`
	ActionInput string           `json:"action_input,omitempty"`
	Observation string           `json:"observation,omitempty"`
	LLMStats    *LLMRuntimeStats `json:"llm_stats,omitempty"`
}

type LLMRuntimeStats struct {
	Model                 string  `json:"model"`
	TotalDurationSeconds  float64 `json:"total_duration_seconds"`
	LoadDurationSeconds   float64 `json:"load_duration_seconds"`
	PromptTokens          int64   `json:"prompt_tokens"`
	PromptTokensPerSecond float64 `json:"prompt_tokens_per_second"`
	OutputTokens          int64   `json:"output_tokens"`
	OutputTokensPerSecond float64 `json:"output_tokens_per_second"`
}

type ExecutionState struct {
	ID               string            `json:"id"`
	SessionID        string            `json:"session_id"`
	UserID           string            `json:"user_id,omitempty"`
	Intent           string            `json:"intent"`
	Pipeline         string            `json:"pipeline"`
	PipelineVersion  string            `json:"pipeline_version,omitempty"`
	PipelineSnapshot json.RawMessage   `json:"pipeline_snapshot,omitempty"`
	InputDigest      string            `json:"input_digest,omitempty"`
	DecisionID       string            `json:"decision_id,omitempty"`     // Prudence decision that allowed the run
	AiSystemID       string            `json:"ai_system_id,omitempty"`    // Prudence AI system it ran as (governed pipelines)
	CompletedSteps   []string          `json:"completed_steps,omitempty"` // IDs of completed steps
	Params           map[string]string `json:"params"`
	MissingParams    []string          `json:"missing_params"`
	StepResults      map[string]any    `json:"step_results,omitempty"`
	ReActHistory     []ReActStep       `json:"react_history,omitempty"`
	Gate             string            `json:"gate"` // "clarification", "human_approval", etc.
	CallbackToken    string            `json:"callback_token,omitempty"`
	CallbackData     map[string]any    `json:"callback_data,omitempty"`
	CallbackAt       *time.Time        `json:"callback_at,omitempty"`
	AwaitStep        string            `json:"await_step,omitempty"`
	AwaitDeadline    *time.Time        `json:"await_deadline,omitempty"`
	Status           string            `json:"status,omitempty"`
	Error            string            `json:"error,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
}

type StateStore interface {
	// Get returns an empty value and no error when the key does not exist or
	// its TTL has elapsed.
	Get(ctx context.Context, key string) (string, error)
	// Set stores a value until ttl elapses; ttl <= 0 means no expiration.
	Set(ctx context.Context, key string, val string, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	ListKeys(ctx context.Context, prefix string) ([]string, error)
}

// PersistentStateStore supports state records that must outlive cache/session
// TTLs, such as an execution paused while waiting for an external callback.
type PersistentStateStore interface {
	SetPersistent(ctx context.Context, key, val string) error
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
	key := "task:" + st.ID
	if persistent, ok := m.store.(PersistentStateStore); ok {
		return persistent.SetPersistent(ctx, key, string(b))
	}
	return m.store.Set(ctx, key, string(b), 0)
}

func (m *StateManager) Load(ctx context.Context, id string) (*ExecutionState, error) {
	val, err := m.store.Get(ctx, "task:"+id)
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
	return m.store.Delete(ctx, "task:"+id)
}

func (m *StateManager) ListExecutionStates(ctx context.Context) ([]*ExecutionState, error) {
	keys, err := m.store.ListKeys(ctx, "task:")
	if err != nil {
		return nil, err
	}

	var states []*ExecutionState
	for _, key := range keys {
		val, err := m.store.Get(ctx, key)
		if err != nil || val == "" {
			continue
		}
		var st ExecutionState
		if err := json.Unmarshal([]byte(val), &st); err == nil {
			states = append(states, &st)
		}
	}

	for i := 0; i < len(states); i++ {
		for j := i + 1; j < len(states); j++ {
			if states[i].CreatedAt.Before(states[j].CreatedAt) {
				states[i], states[j] = states[j], states[i]
			}
		}
	}

	return states, nil
}
