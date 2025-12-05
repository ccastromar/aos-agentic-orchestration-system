package state

import (
	"context"
	"sync"
)

type MemoryStore struct {
	mu     sync.RWMutex
	states map[string]*ExecutionState
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		states: make(map[string]*ExecutionState),
	}
}

func (m *MemoryStore) Save(ctx context.Context, st *ExecutionState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[st.ID] = st
	return nil
}

func (m *MemoryStore) Load(ctx context.Context, id string) (*ExecutionState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.states[id], nil
}

func (m *MemoryStore) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.states, id)
	return nil
}
