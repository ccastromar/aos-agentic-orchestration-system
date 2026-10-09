package state

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/logx"
)

type Memory struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	Text      string         `json:"text"`
	Embedding []float32      `json:"embedding"`
	Metadata  map[string]any `json:"metadata"`
	Timestamp time.Time      `json:"timestamp"`
}

// IsPersonalFact separates user facts from execution records and preferences,
// including legacy records created before memory kinds were introduced.
func (m Memory) IsPersonalFact() bool {
	if m.Metadata["execution_id"] != nil || m.Metadata["intent"] != nil {
		return false
	}
	return !strings.HasPrefix(m.Text, "Preference:") && !strings.HasPrefix(m.Text, "Intent:")
}

type VectorStore interface {
	AddMemory(ctx context.Context, mem Memory) error
	SearchMemories(ctx context.Context, sessionID string, embedding []float32, topK int) ([]Memory, error)
	ListMemories(ctx context.Context, sessionID string) ([]Memory, error)
	DeleteMemory(ctx context.Context, sessionID string, memoryID string) error
	ClearMemories(ctx context.Context, sessionID string) error
}

// MemoryVectorStore implements VectorStore entirely in memory (useful for testing)
type MemoryVectorStore struct {
	mu       sync.RWMutex
	memories []Memory
}

func NewMemoryVectorStore() *MemoryVectorStore {
	return &MemoryVectorStore{memories: make([]Memory, 0)}
}

func (m *MemoryVectorStore) AddMemory(ctx context.Context, mem Memory) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memories = append(m.memories, mem)
	return nil
}

func (m *MemoryVectorStore) SearchMemories(ctx context.Context, sessionID string, embedding []float32, topK int) ([]Memory, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// Dummy search: return exact session match regardless of embedding, just for testing
	// A real implementation would compute cosine similarity.
	var res []Memory
	for _, mem := range m.memories {
		if mem.SessionID == sessionID {
			res = append(res, mem)
		}
	}
	return res, nil
}

func (m *MemoryVectorStore) ListMemories(ctx context.Context, sessionID string) ([]Memory, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var res []Memory
	for _, mem := range m.memories {
		if mem.SessionID == sessionID {
			res = append(res, mem)
		}
	}
	return res, nil
}

func (m *MemoryVectorStore) DeleteMemory(ctx context.Context, sessionID string, memoryID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, mem := range m.memories {
		if mem.SessionID == sessionID && mem.ID == memoryID {
			m.memories = append(m.memories[:i], m.memories[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *MemoryVectorStore) ClearMemories(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var kept []Memory
	for _, mem := range m.memories {
		if mem.SessionID != sessionID {
			kept = append(kept, mem)
		}
	}
	m.memories = kept
	return nil
}

// QdrantVectorStore implements VectorStore using Qdrant REST API
type QdrantVectorStore struct {
	baseURL    string
	collection string
	client     *http.Client
}

func NewQdrantVectorStore(baseURL string) *QdrantVectorStore {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		baseURL = "http://localhost:6333"
	}
	vs := &QdrantVectorStore{
		baseURL:    baseURL,
		collection: "aos_memories",
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}

	// Create collection asynchronously (fire and forget initialization)
	go func() {
		_ = vs.createCollection()
	}()
	return vs
}

func (q *QdrantVectorStore) createCollection() error {
	payload := map[string]any{
		"vectors": map[string]any{
			"size":     1536, // default to typical embedding sizes (OpenAI ada-002, or general)
			"distance": "Cosine",
		},
	}
	// For Nomic embed text, the size is 768.
	// To be truly robust, size should be configurable or we should support dynamic creation.
	// We'll leave it at 1536 as a standard, but local models might fail if dimension doesn't match.
	// Actually, Qdrant allows configuring vectors later, but let's assume 768 for nomic or 1536 for OpenAI.
	// We'll just configure it as 768 by default since local Ollama is standard here, and openai is 1536.
	// Ideally we pass dimension. Let's use 768 as it's the size for nomic-embed-text and gemini text-embedding-004.
	payload["vectors"].(map[string]any)["size"] = 768

	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest("PUT", q.baseURL+"/collections/"+q.collection, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")

	resp, err := q.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (q *QdrantVectorStore) AddMemory(ctx context.Context, mem Memory) error {
	payload := map[string]any{
		"points": []map[string]any{
			{
				"id":     mem.ID, // UUID string
				"vector": mem.Embedding,
				"payload": map[string]any{
					"session_id": mem.SessionID,
					"text":       mem.Text,
					"metadata":   mem.Metadata,
					"timestamp":  mem.Timestamp.Format(time.RFC3339),
				},
			},
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", q.baseURL+"/collections/"+q.collection+"/points?wait=true", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := q.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("qdrant add error %d: %s", resp.StatusCode, string(respBody))
	}

	logx.Info("VectorStore", "Added memory %s for session %s", mem.ID, mem.SessionID)
	return nil
}

func (q *QdrantVectorStore) SearchMemories(ctx context.Context, sessionID string, embedding []float32, topK int) ([]Memory, error) {
	payload := map[string]any{
		"vector":       embedding,
		"limit":        topK,
		"with_payload": true,
	}

	if sessionID != "" {
		payload["filter"] = map[string]any{
			"must": []map[string]any{
				{
					"key": "session_id",
					"match": map[string]any{
						"value": sessionID,
					},
				},
			},
		}
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", q.baseURL+"/collections/"+q.collection+"/points/search", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := q.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("qdrant search error %d: %s", resp.StatusCode, string(respBody))
	}

	var res struct {
		Result []struct {
			Id      string         `json:"id"`
			Score   float32        `json:"score"`
			Payload map[string]any `json:"payload"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	var memories []Memory
	for _, p := range res.Result {
		// Minimum similarity threshold (e.g. > 0.7 for cosine)
		if p.Score < 0.7 {
			continue
		}
		m := Memory{
			ID: p.Id,
		}
		if text, ok := p.Payload["text"].(string); ok {
			m.Text = text
		}
		if sid, ok := p.Payload["session_id"].(string); ok {
			m.SessionID = sid
		}
		if meta, ok := p.Payload["metadata"].(map[string]any); ok {
			m.Metadata = meta
		}
		memories = append(memories, m)
	}

	return memories, nil
}

func (q *QdrantVectorStore) ListMemories(ctx context.Context, sessionID string) ([]Memory, error) {
	payload := map[string]any{
		"filter": map[string]any{
			"must": []map[string]any{
				{
					"key": "session_id",
					"match": map[string]any{
						"value": sessionID,
					},
				},
			},
		},
		"with_payload": true,
		"limit":        100, // Reasonable limit for listing memories
	}

	memories := []Memory{}
	for {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, "POST", q.baseURL+"/collections/"+q.collection+"/points/scroll", bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := q.client.Do(req)
		if err != nil {
			return nil, err
		}
		var res struct {
			Result struct {
				Points []struct {
					ID      string         `json:"id"`
					Payload map[string]any `json:"payload"`
				} `json:"points"`
				Next json.RawMessage `json:"next_page_offset"`
			} `json:"result"`
		}
		err = json.NewDecoder(resp.Body).Decode(&res)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("qdrant list error %d", resp.StatusCode)
		}
		if err != nil {
			return nil, err
		}
		for _, p := range res.Result.Points {
			m := Memory{ID: p.ID}
			m.Text, _ = p.Payload["text"].(string)
			m.SessionID, _ = p.Payload["session_id"].(string)
			m.Metadata, _ = p.Payload["metadata"].(map[string]any)
			if stamp, ok := p.Payload["timestamp"].(string); ok {
				m.Timestamp, _ = time.Parse(time.RFC3339, stamp)
			}
			memories = append(memories, m)
		}
		if len(res.Result.Next) == 0 || string(res.Result.Next) == "null" {
			return memories, nil
		}
		payload["offset"] = res.Result.Next
	}
}

func (q *QdrantVectorStore) DeleteMemory(ctx context.Context, sessionID string, memoryID string) error {
	payload := map[string]any{
		"filter": map[string]any{"must": []map[string]any{
			{"has_id": []string{memoryID}},
			{"key": "session_id", "match": map[string]any{"value": sessionID}},
		}},
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", q.baseURL+"/collections/"+q.collection+"/points/delete?wait=true", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := q.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("qdrant delete error %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (q *QdrantVectorStore) ClearMemories(ctx context.Context, sessionID string) error {
	payload := map[string]any{
		"filter": map[string]any{
			"must": []map[string]any{
				{
					"key": "session_id",
					"match": map[string]any{
						"value": sessionID,
					},
				},
			},
		},
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", q.baseURL+"/collections/"+q.collection+"/points/delete?wait=true", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := q.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("qdrant clear error %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
