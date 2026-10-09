package audit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestErnestLoggerSendsHashOnlyEventWithSharedKey(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/events" {
			t.Errorf("request = %s %s, want POST /events", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Ernest-Ingest-Key"); got != "integration-secret" {
			t.Errorf("ingestor key = %q, want shared key", got)
		}
		if got := r.Header.Get("X-Ernest-Event-Source"); got != "aos" {
			t.Errorf("event source = %q, want aos", got)
		}
		timestamp := r.Header.Get("X-Ernest-Provider-Timestamp")
		seconds, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil || time.Since(time.Unix(seconds, 0)) > 5*time.Minute {
			t.Errorf("invalid or stale Ernest HMAC timestamp %q", timestamp)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if strings.Contains(string(body), "private prompt") || strings.Contains(string(body), "sensitive output") {
			t.Errorf("raw audit data leaked to Ernest: %s", body)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("invalid JSON payload: %v", err)
		}
		metadata, _ := payload["metadata"].(map[string]any)
		if metadata["eventHash"] == "" || metadata["traceId"] != "trace-123" || metadata["phase"] != "tool.completed" {
			t.Errorf("missing AOS evidence metadata: %#v", metadata)
		}
		if payload["sourceEventId"] == "" || payload["eventType"] != "aos.audit.event.tool.completed" {
			t.Errorf("missing event identity: %#v", payload)
		}
		mac := hmac.New(sha256.New, []byte("provider-secret"))
		_, _ = mac.Write([]byte(timestamp + "."))
		_, _ = mac.Write(body)
		wantSignature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if got := r.Header.Get("X-Ernest-Provider-Signature"); got != wantSignature {
			t.Errorf("provider signature = %q, want %q", got, wantSignature)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"accepted"}`))
	}))
	defer server.Close()

	logger, err := NewErnestLogger(server.URL+"/", "integration-secret", "provider-secret", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	logger.Log(Event{
		Timestamp: time.Date(2026, 10, 9, 12, 0, 0, 0, time.FixedZone("CEST", 2*60*60)),
		TraceID:   "trace-123",
		Phase:     "tool.completed",
		Step:      "review",
		Intent:    "custody.transfer",
		Pipeline:  "pipeline-custody",
		Result:    "success",
		Params:    map[string]interface{}{"message": "private prompt"},
		Details:   map[string]interface{}{"output": "sensitive output"},
	})
	if err := logger.Err(); err != nil {
		t.Fatalf("sending event failed: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestErnestLoggerReportsNonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad shared key", http.StatusUnauthorized)
	}))
	defer server.Close()
	logger, err := NewErnestLogger(server.URL, "wrong-key", "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	logger.Log(Event{TraceID: "trace-fail", Phase: "intent.accepted"})
	if err := logger.Err(); err == nil || !strings.Contains(err.Error(), "401 Unauthorized") {
		t.Fatalf("expected Ernest 401 error, got %v", err)
	}
}

func TestErnestLoggerDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected.Store(true)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()
	logger, err := NewErnestLogger(source.URL, "secret", "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	logger.Log(Event{TraceID: "redirect-test", Phase: "test"})
	if err := logger.Err(); err == nil || !strings.Contains(err.Error(), "302 Found") {
		t.Fatalf("expected redirect to fail, got %v", err)
	}
	if redirected.Load() {
		t.Fatal("Ernest logger followed a redirect and may have forwarded credentials")
	}
}

func TestNewErnestLoggerValidatesURL(t *testing.T) {
	for _, raw := range []string{"", "file:///tmp/events", "http://example.test/events?token=x", "//example.test", "https://user:pass@example.test"} {
		if _, err := NewErnestLogger(raw, "", "", time.Second); err == nil {
			t.Errorf("NewErnestLogger(%q) unexpectedly succeeded", raw)
		}
	}
}
