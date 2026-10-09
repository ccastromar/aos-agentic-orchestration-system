package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/app"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// This opt-in test uses the real remote model but an isolated in-memory AOS instance.
func TestLiveMemoryRoundTrip(t *testing.T) {
	url := os.Getenv("AOS_TEST_OLLAMA_URL")
	model := os.Getenv("AOS_TEST_OLLAMA_MODEL")
	if url == "" || model == "" {
		t.Skip("set AOS_TEST_OLLAMA_URL and AOS_TEST_OLLAMA_MODEL")
	}
	chdirToRepoRoot(t)
	t.Setenv("OLLAMA_BASE_URL", url)
	t.Setenv("LLM_ENGINE", "ollama")
	t.Setenv("LLM_MODEL", model)
	t.Setenv("OLLAMA_EMBED_MODEL", "nomic-embed-text")
	t.Setenv("QDRANT_URL", "memory")
	t.Setenv("REDIS_ADDR", "")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("API_KEY", "")
	t.Setenv("AOS_IAM_URL", "disabled")
	a, err := app.New()
	if err != nil {
		t.Fatal(err)
	}
	stop := a.StartAgents(context.Background())
	defer stop()
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	client := &http.Client{Timeout: 120 * time.Second}
	post := func(path, body string) map[string]any {
		t.Helper()
		res, err := client.Post(server.URL+path, "application/json", bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		if res.StatusCode >= 300 {
			t.Fatalf("%s: %d %s", path, res.StatusCode, data)
		}
		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	accepted := post("/ask", `{"message":"Remember that my dog's name is Rex","lang":"en"}`)
	id, ok := accepted["id"].(string)
	if !ok {
		t.Fatalf("no task: %v", accepted)
	}
	deadline := time.Now().Add(120 * time.Second)
	completed := false
	for time.Now().Before(deadline) {
		res, err := client.Get(server.URL + "/task?id=" + id)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("task poll: %d %s", res.StatusCode, data)
		}
		var got map[string]any
		json.Unmarshal(data, &got)
		status, _ := got["status"].(string)
		if status == "error" || strings.HasPrefix(status, "await_") {
			t.Fatalf("memory was not saved: %s", data)
		}
		if status == "completed" || status == "success" {
			if !bytes.Contains(data, []byte("Rex")) {
				t.Fatalf("saved wrong fact: %s", data)
			}
			completed = true
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !completed {
		t.Fatal("memory add timed out")
	}
	for i := 0; i < 2; i++ {
		got := post("/ask_structured", `{"operation":"memory.list","params":{}}`)
		data, _ := json.Marshal(got)
		if !bytes.Contains(data, []byte("Rex")) || bytes.Contains(data, []byte("peanuts")) {
			t.Fatalf("wrong list: %s", data)
		}
	}
	res, err := client.Get(server.URL + "/api/v1/memories")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var memories []struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(res.Body).Decode(&memories); err != nil {
		t.Fatal(err)
	}
	if len(memories) != 1 || !strings.Contains(memories[0].Text, "Rex") {
		t.Fatalf("memory polluted: %+v", memories)
	}
	t.Logf("Confirmed one persisted fact after two list operations: %s", memories[0].Text)
}
