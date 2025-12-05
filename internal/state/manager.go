package state

import (
	"context"
	"time"
)

type ExecutionState struct {
	ID          string            `json:"id"`
	Intent      string            `json:"intent"`
	Pipeline    string            `json:"pipeline"`
	StepIndex   int               `json:"step_index"`
	Params      map[string]string `json:"params,omitempty"`
	StepResults map[string]any    `json:"step_results,omitempty"`
	Gate        string            `json:"gate,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
}

type StateStore interface {
	Save(ctx context.Context, st *ExecutionState) error
	Load(ctx context.Context, id string) (*ExecutionState, error)
	Delete(ctx context.Context, id string) error
}

type StateManager struct {
	store StateStore
}

func NewStateManager(store StateStore) *StateManager {
	return &StateManager{store: store}
}

func (m *StateManager) Save(ctx context.Context, st *ExecutionState) error {
	if st.CreatedAt.IsZero() {
		st.CreatedAt = time.Now().UTC()
	}
	return m.store.Save(ctx, st)
}

func (m *StateManager) Load(ctx context.Context, id string) (*ExecutionState, error) {
	return m.store.Load(ctx, id)
}

func (m *StateManager) Delete(ctx context.Context, id string) error {
	return m.store.Delete(ctx, id)
}

func (m *StateManager) AdvanceStep(ctx context.Context, st *ExecutionState) error {
	st.StepIndex++
	return m.Save(ctx, st)
}
