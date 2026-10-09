package state

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteStorePersistsExecutionStateAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewStateManager(store)
	want := &ExecutionState{ID: "execution-1", Intent: "custody.transfer", Params: map[string]string{"amount": "1"}}
	if err := manager.Save(ctx, want); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := NewStateManager(store).Load(ctx, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Intent != want.Intent || got.Params["amount"] != "1" {
		t.Fatalf("state after reopening = %#v, want persisted state", got)
	}
}

func TestSQLiteStoreTTLAndPersistentValues(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Set(ctx, "session:expired", "old", time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if value, err := store.Get(ctx, "session:expired"); err != nil || value != "" {
		t.Fatalf("expired Get = %q, %v", value, err)
	}
	if err := store.SetPersistent(ctx, "task:waiting", "callback"); err != nil {
		t.Fatal(err)
	}
	keys, err := store.ListKeys(ctx, "task:")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != "task:waiting" {
		t.Fatalf("ListKeys = %#v", keys)
	}
	if err := store.Delete(ctx, "task:waiting"); err != nil {
		t.Fatal(err)
	}
	if value, err := store.Get(ctx, "task:waiting"); err != nil || value != "" {
		t.Fatalf("deleted Get = %q, %v", value, err)
	}
}
