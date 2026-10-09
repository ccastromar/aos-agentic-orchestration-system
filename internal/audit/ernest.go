package audit

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ErnestLogger forwards hash-only AOS audit evidence to Ernest's generic
// event-ingestion endpoint. Ernest queues accepted events asynchronously.
type ErnestLogger struct {
	endpoint string
	apiKey   string
	hmacKey  string
	client   *http.Client

	mu      sync.Mutex
	lastErr error
}

func NewErnestLogger(baseURL, apiKey, hmacSecret string, timeout time.Duration) (*ErnestLogger, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("invalid Ernest ingestor URL %q", baseURL)
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/events"
	return &ErnestLogger{
		endpoint: parsed.String(),
		apiKey:   strings.TrimSpace(apiKey),
		hmacKey:  strings.TrimSpace(hmacSecret),
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (l *ErnestLogger) Log(event Event) {
	err := l.send(event)
	l.mu.Lock()
	l.lastErr = err
	l.mu.Unlock()
}

func (l *ErnestLogger) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastErr
}

func (l *ErnestLogger) Close() error {
	l.client.CloseIdleConnections()
	return nil
}

func (l *ErnestLogger) send(event Event) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	} else {
		event.Timestamp = event.Timestamp.UTC()
	}
	canonical, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal AOS audit evidence: %w", err)
	}
	eventSum := sha256.Sum256(canonical)
	eventHash := hex.EncodeToString(eventSum[:])
	eventType := "aos.audit.event"
	if phase := strings.Trim(strings.TrimSpace(event.Phase), "."); phase != "" {
		eventType += "." + phase
	}
	sourceEventID := strings.Join([]string{event.TraceID, event.Phase, event.Step, eventHash}, ":")
	if event.TraceID == "" {
		sourceEventID = eventHash
	}

	// Send only identifiers, status, and a digest. Prompts, params, tool outputs,
	// user IDs, and error messages stay in AOS's local audit journal.
	metadata := map[string]any{
		"producer":      "aos-agentic-orchestration-system",
		"schemaVersion": "1",
		"eventHash":     eventHash,
		"phase":         event.Phase,
		"traceId":       event.TraceID,
		"intent":        event.Intent,
		"pipeline":      event.Pipeline,
		"result":        event.Result,
		"latencyMs":     event.LatencyMs,
		"occurredAt":    event.Timestamp.Format(time.RFC3339Nano),
	}
	if event.Step != "" {
		metadata["step"] = event.Step
	}
	body, err := json.Marshal(map[string]any{
		"source":        "aos",
		"eventType":     eventType,
		"sourceEventId": sourceEventID,
		"occurredAt":    event.Timestamp.Format(time.RFC3339Nano),
		"modelId":       "aos-agentic-orchestration-system",
		"metadata":      metadata,
	})
	if err != nil {
		return fmt.Errorf("marshal Ernest event: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), l.client.Timeout)
	defer cancel()
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.endpoint, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create Ernest event request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Ernest-Event-Source", "aos")
		req.Header.Set("X-Ernest-Event-Type", eventType)
		req.Header.Set("X-Ernest-Source-Event-Id", sourceEventID)
		if l.apiKey != "" {
			req.Header.Set("X-Ernest-Ingest-Key", l.apiKey)
		}
		if l.hmacKey != "" {
			timestamp := fmt.Sprintf("%d", time.Now().Unix())
			mac := hmac.New(sha256.New, []byte(l.hmacKey))
			_, _ = mac.Write([]byte(timestamp))
			_, _ = mac.Write([]byte("."))
			_, _ = mac.Write(body)
			req.Header.Set("X-Ernest-Provider-Timestamp", timestamp)
			req.Header.Set("X-Ernest-Provider-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		}
		resp, requestErr := l.client.Do(req)
		if requestErr == nil {
			responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			if readErr != nil {
				lastErr = fmt.Errorf("read Ernest response: %w", readErr)
			} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				lastErr = fmt.Errorf("Ernest event ingestion returned %s: %s", resp.Status, strings.TrimSpace(string(responseBody)))
				if resp.StatusCode >= 400 && resp.StatusCode < 500 {
					return lastErr
				}
			} else {
				return nil
			}
		} else {
			lastErr = fmt.Errorf("send event to Ernest: %w", requestErr)
		}
		if attempt == 0 {
			select {
			case <-ctx.Done():
				return lastErr
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("Ernest event ingestion timed out: %w", lastErr)
	}
	return lastErr
}
