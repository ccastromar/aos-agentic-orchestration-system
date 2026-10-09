package state

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type persistentStoreSpy struct {
	MemoryStore
	persistentWrites int
	regularTTL       time.Duration
}

func (s *persistentStoreSpy) Set(ctx context.Context, key, val string, ttl time.Duration) error {
	s.regularTTL = ttl
	return s.MemoryStore.Set(ctx, key, val, ttl)
}

func (s *persistentStoreSpy) SetPersistent(ctx context.Context, key, val string) error {
	s.persistentWrites++
	return s.MemoryStore.Set(ctx, key, val, 0)
}

func TestExecutionStateUsesPersistentStoreWrite(t *testing.T) {
	store := &persistentStoreSpy{MemoryStore: *NewMemoryStore()}
	manager := NewStateManager(store)
	require.NoError(t, manager.Save(context.Background(), &ExecutionState{ID: "durable-task"}))
	require.Equal(t, 1, store.persistentWrites)
	require.Zero(t, store.regularTTL)
	got, err := manager.Load(context.Background(), "durable-task")
	require.NoError(t, err)
	require.NotNil(t, got)
}
