package state

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestMemoryStoreConcurrentIsolation(t *testing.T) {
	s := NewMemoryVectorStore()
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprint(i)
			s.AddMemory(ctx, Memory{ID: id, SessionID: "alice"})
			s.ListMemories(ctx, "alice")
			s.SearchMemories(ctx, "alice", nil, 3)
			s.DeleteMemory(ctx, "bob", id)
			s.ClearMemories(ctx, "bob")
		}(i)
	}
	wg.Wait()
	got, _ := s.ListMemories(ctx, "alice")
	if len(got) != 30 {
		t.Fatalf("cross-user operation changed memories: %d", len(got))
	}
	s.ClearMemories(ctx, "alice")
	got, _ = s.ListMemories(ctx, "alice")
	if len(got) != 0 {
		t.Fatal("clear failed")
	}
}

func TestQdrantDeleteScopesOwnerAndWaits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("wait") != "true" {
			t.Error("delete must wait for persistence")
		}
		var body struct {
			Filter struct {
				Must []map[string]json.RawMessage `json:"must"`
			} `json:"filter"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body.Filter.Must) != 2 {
			t.Error("expected ID and owner predicates")
			return
		}
		if string(body.Filter.Must[0]["has_id"]) != `["memory-id"]` || string(body.Filter.Must[1]["match"]) != `{"value":"alice"}` {
			t.Errorf("unexpected filter: %+v", body)
		}
		w.Write([]byte(`{"result":{"status":"completed"}}`))
	}))
	defer server.Close()
	q := &QdrantVectorStore{baseURL: server.URL, collection: "test", client: server.Client()}
	if err := q.DeleteMemory(context.Background(), "alice", "memory-id"); err != nil {
		t.Fatal(err)
	}
}

func TestQdrantListFollowsPagesAndPreservesTimestamp(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if calls == 1 {
			w.Write([]byte(`{"result":{"points":[{"id":"first","payload":{"session_id":"alice","text":"fact","timestamp":"2026-07-18T10:00:00Z"}}],"next_page_offset":"first"}}`))
			return
		}
		if body["offset"] != "first" {
			t.Error("missing next offset")
		}
		w.Write([]byte(`{"result":{"points":[{"id":"second","payload":{"session_id":"alice"}}],"next_page_offset":null}}`))
	}))
	defer server.Close()
	q := &QdrantVectorStore{baseURL: server.URL, collection: "test", client: server.Client()}
	got, err := q.ListMemories(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(got) != 2 {
		t.Fatalf("pagination failed: %d %v", calls, got)
	}
	if !got[0].Timestamp.Equal(time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)) {
		t.Fatal("timestamp lost")
	}
}
