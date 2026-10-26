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
	// aiSystems maps a pipeline name to the Prudence AI system it runs as.
	aiSystems map[string]string
	client    *http.Client

	mu          sync.Mutex
	lastErr     error
	evidenceKey []byte
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

// WithEvidenceKey sets the key event hashes are digested with before they leave AOS
// (LoadEvidenceKey). Without it the logger sends nothing.
func (l *ErnestLogger) WithEvidenceKey(key []byte) *ErnestLogger {
	l.evidenceKey = key
	return l
}

// WithAISystems tags each event with the Prudence AI system of its pipeline (pipeline name ->
// system id), so Ernest can attribute governed executions.
func (l *ErnestLogger) WithAISystems(aiSystems map[string]string) *ErnestLogger {
	l.aiSystems = aiSystems
	return l
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
	}
	evidence, err := HTTPEvidence(event, l.aiSystems[event.Pipeline], l.evidenceKey)
	if err != nil {
		return err
	}
	eventType, sourceEventID, body := evidence.EventType, evidence.SourceEventID, evidence.Body

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
			req.Header.Set("X-Ernest-Provider-Timestamp", timestamp)
			req.Header.Set("X-Ernest-Provider-Signature", ProviderSignature(l.hmacKey, timestamp, body))
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

// HTTPEvidence is the body AOS posts to Ernest's ingestor for one audit event
// (contract/audit-events). Over HTTP there is no journal record, so the event is identified by
// the EvidenceDigest of the SHA-256 of its JSON form in UTC.
func HTTPEvidence(event Event, aiSystemID string, evidenceKey []byte) (Evidence, error) {
	if len(evidenceKey) == 0 {
		return Evidence{}, errors.New("no evidence key: audit events cannot leave AOS undigested")
	}
	event.Timestamp = event.Timestamp.UTC()
	canonical, err := json.Marshal(event)
	if err != nil {
		return Evidence{}, fmt.Errorf("marshal AOS audit evidence: %w", err)
	}
	eventSum := sha256.Sum256(canonical)
	return BuildEvidence(event, EvidenceDigest(evidenceKey, hex.EncodeToString(eventSum[:])), aiSystemID, nil)
}

// ProviderSignature is X-Ernest-Provider-Signature: HMAC-SHA256 over "<timestamp>.<body>".
func ProviderSignature(key, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
