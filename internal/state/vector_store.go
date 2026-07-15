package state

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

type VectorStore interface {
	AddMemory(ctx context.Context, mem Memory) error
	SearchMemories(ctx context.Context, sessionID string, embedding []float32, topK int) ([]Memory, error)
}

// MemoryVectorStore implements VectorStore entirely in memory (useful for testing)
type MemoryVectorStore struct {
	memories []Memory
}

func NewMemoryVectorStore() *MemoryVectorStore {
	return &MemoryVectorStore{memories: make([]Memory, 0)}
}

func (m *MemoryVectorStore) AddMemory(ctx context.Context, mem Memory) error {
	m.memories = append(m.memories, mem)
	return nil
}

func (m *MemoryVectorStore) SearchMemories(ctx context.Context, sessionID string, embedding []float32, topK int) ([]Memory, error) {
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
	
	req, err := http.NewRequestWithContext(ctx, "PUT", q.baseURL+"/collections/"+q.collection+"/points", bytes.NewReader(b))
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
		"vector": embedding,
		"limit":  topK,
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
