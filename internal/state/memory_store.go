package state

import (
	"context"
	"sync"
	"time"
)

type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]memoryEntry
}

type memoryEntry struct {
	value     string
	expiresAt time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		data: make(map[string]memoryEntry),
	}
}

func (m *MemoryStore) Get(ctx context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry, ok := m.data[key]; ok {
		if !entry.expiresAt.IsZero() && !entry.expiresAt.After(time.Now()) {
			delete(m.data, key)
			return "", nil
		}
		return entry.value, nil
	}
	return "", nil
}

func (m *MemoryStore) Set(ctx context.Context, key string, val string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := memoryEntry{value: val}
	if ttl > 0 {
		entry.expiresAt = time.Now().Add(ttl)
	}
	m.data[key] = entry
	return nil
}

func (m *MemoryStore) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *MemoryStore) ListKeys(ctx context.Context, prefix string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	now := time.Now()
	for k, entry := range m.data {
		if !entry.expiresAt.IsZero() && !entry.expiresAt.After(now) {
			delete(m.data, k)
			continue
		}
		if prefix == "" || len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			keys = append(keys, k)
		}
	}
	return keys, nil
}
