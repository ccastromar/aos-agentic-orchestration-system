package audit

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/metrics"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/logx"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

// KafkaShipper publishes AOS's audit journal to Ernest over Kafka, asynchronously.
//
// The journal (FileChain) is the outbox: every audit event is already durable and hash-chained
// on disk before anything is sent. The shipper reads complete records after a persisted
// cursor, publishes them, and advances the cursor only once Kafka has acknowledged them. So a
// pipeline never waits for (or fails because of) Ernest or Kafka, nothing is lost while they
// are down, and a replay after a crash is deduplicated by Ernest on sourceEventId.
type KafkaShipper struct {
	cfg    KafkaShipperConfig
	client *kgo.Client
}

type KafkaShipperConfig struct {
	JournalPath string
	CursorPath  string // default: JournalPath + ".kafka-cursor"
	Brokers     []string
	Topic       string
	Interval    time.Duration
	BatchSize   int
	Security    KafkaSecurity
	// AISystems maps a pipeline name to the Prudence AI system it runs as.
	AISystems map[string]string
	// EvidenceKey digests each record hash before it leaves AOS (LoadEvidenceKey).
	EvidenceKey []byte
}

// KafkaSecurity mirrors Ernest's consumer options: PLAINTEXT, SSL, SASL_PLAINTEXT or SASL_SSL,
// a CA file for TLS, and PLAIN / SCRAM-SHA-256 / SCRAM-SHA-512 credentials for SASL.
type KafkaSecurity struct {
	Protocol      string
	CAFile        string
	SASLMechanism string
	Username      string
	Password      string
}

type kafkaCursor struct {
	Offset   int64  `json:"offset"`
	Sequence uint64 `json:"sequence"`
}

func NewKafkaShipper(cfg KafkaShipperConfig) (*KafkaShipper, error) {
	if len(cfg.EvidenceKey) == 0 {
		return nil, errors.New("kafka evidence needs an evidence key (LoadEvidenceKey)")
	}
	if strings.TrimSpace(cfg.JournalPath) == "" {
		return nil, errors.New("Kafka evidence needs AUDIT_LOG_PATH: the audit journal is its outbox")
	}
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("Kafka evidence needs at least one broker")
	}
	if cfg.Topic == "" {
		cfg.Topic = "aos.executions"
	}
	if cfg.CursorPath == "" {
		cfg.CursorPath = cfg.JournalPath + ".kafka-cursor"
	}
	if cfg.Interval <= 0 {
		cfg.Interval = time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 500
	}
	opts, err := cfg.Security.options()
	if err != nil {
		return nil, err
	}
	opts = append(opts,
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.DefaultProduceTopic(cfg.Topic),
		kgo.RequiredAcks(kgo.AllISRAcks()), // idempotent producer (franz-go default) needs all ISRs
		kgo.ProducerLinger(20*time.Millisecond),
		// Let a broker with auto.create.topics.enable create the topic on first publish; franz-go
		// does not ask for it by default (a broker that forbids it still refuses).
		kgo.AllowAutoTopicCreation(),
	)
	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("create Kafka evidence producer: %w", err)
	}
	return &KafkaShipper{cfg: cfg, client: client}, nil
}

func (s KafkaSecurity) options() ([]kgo.Opt, error) {
	protocol := strings.ToUpper(strings.TrimSpace(s.Protocol))
	if protocol == "" {
		protocol = "PLAINTEXT"
	}
	var opts []kgo.Opt
	switch protocol {
	case "PLAINTEXT":
	case "SSL", "SASL_SSL":
		tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
		if s.CAFile != "" {
			pem, err := os.ReadFile(s.CAFile)
			if err != nil {
				return nil, fmt.Errorf("read Kafka CA file: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, fmt.Errorf("Kafka CA file %s holds no PEM certificate", s.CAFile)
			}
			tlsCfg.RootCAs = pool
		}
		opts = append(opts, kgo.DialTLSConfig(tlsCfg))
	case "SASL_PLAINTEXT":
	default:
		return nil, fmt.Errorf("unsupported Kafka security protocol %q", s.Protocol)
	}
	if strings.HasPrefix(protocol, "SASL_") {
		if s.Username == "" || s.Password == "" {
			return nil, fmt.Errorf("Kafka %s needs a SASL username and password", protocol)
		}
		switch strings.ToUpper(strings.TrimSpace(s.SASLMechanism)) {
		case "", "SCRAM-SHA-512":
			opts = append(opts, kgo.SASL(scram.Auth{User: s.Username, Pass: s.Password}.AsSha512Mechanism()))
		case "SCRAM-SHA-256":
			opts = append(opts, kgo.SASL(scram.Auth{User: s.Username, Pass: s.Password}.AsSha256Mechanism()))
		case "PLAIN":
			opts = append(opts, kgo.SASL(plain.Auth{User: s.Username, Pass: s.Password}.AsMechanism()))
		default:
			return nil, fmt.Errorf("unsupported Kafka SASL mechanism %q", s.SASLMechanism)
		}
	}
	return opts, nil
}

// Run ships the journal until ctx ends. Failures are logged and retried with backoff; they
// never reach the pipelines that write the journal.
func (s *KafkaShipper) Run(ctx context.Context) error {
	defer s.client.Close()
	logx.Info("Evidence", "shipping audit journal %s to Kafka topic %s", s.cfg.JournalPath, s.cfg.Topic)
	wait := s.cfg.Interval
	for {
		sent, err := s.ShipOnce(ctx)
		switch {
		case err != nil:
			logx.Warn("Evidence", "Kafka evidence shipping failed (will retry): %v", err)
			if wait < 30*time.Second {
				wait *= 2
			}
		case sent > 0:
			wait = s.cfg.Interval
			continue // drain the backlog without pausing
		default:
			wait = s.cfg.Interval
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
}

// ShipOnce publishes up to one batch of complete journal records after the cursor and returns
// how many were acknowledged.
func (s *KafkaShipper) ShipOnce(ctx context.Context) (int, error) {
	cur, err := s.readCursor()
	if err != nil {
		return 0, err
	}
	file, err := os.Open(s.cfg.JournalPath)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil // nothing audited yet
	}
	if err != nil {
		return 0, fmt.Errorf("open audit journal: %w", err)
	}
	defer file.Close()
	var size int64 = -1
	if info, err := file.Stat(); err == nil {
		size = info.Size()
	}
	// What is left to ship, in bytes of journal, once this pass is done.
	shipped := cur.Offset
	defer func() {
		if size >= 0 {
			metrics.EvidenceBacklog.Set(map[string]string{"transport": "kafka"}, float64(max(size-shipped, 0)))
		}
	}()
	if size >= 0 && size < cur.Offset {
		// The journal was replaced. Ernest deduplicates on sourceEventId, so re-reading it is safe.
		logx.Warn("Evidence", "audit journal is shorter than the shipped offset; re-reading it from the start")
		cur = kafkaCursor{}
		shipped = 0
	}
	if _, err := file.Seek(cur.Offset, io.SeekStart); err != nil {
		return 0, fmt.Errorf("seek audit journal: %w", err)
	}

	reader := bufio.NewReader(file)
	offset := cur.Offset
	var records []*kgo.Record
	next := cur
	for len(records) < s.cfg.BatchSize {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			break // EOF, or a record still being written: ship only complete lines
		}
		offset += int64(len(line))
		trimmed := strings.TrimSpace(string(line))
		if trimmed == "" {
			next.Offset = offset
			continue
		}
		var rec Record
		if err := json.Unmarshal([]byte(trimmed), &rec); err != nil {
			// Never skip evidence silently: stop here until the journal is repaired.
			return 0, fmt.Errorf("audit journal record at byte %d is unreadable: %w", offset-int64(len(line)), err)
		}
		record, err := JournalRecord(rec, s.cfg.AISystems[rec.Event.Pipeline], s.cfg.EvidenceKey)
		if err != nil {
			return 0, err
		}
		records = append(records, record)
		next = kafkaCursor{Offset: offset, Sequence: rec.Sequence}
	}
	if len(records) == 0 {
		if next.Offset != cur.Offset {
			if err := s.writeCursor(next); err != nil {
				return 0, err
			}
			shipped = next.Offset
		}
		return 0, nil
	}
	produceCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := s.client.ProduceSync(produceCtx, records...).FirstErr(); err != nil {
		metrics.EvidenceShipped.Add(map[string]string{"transport": "kafka", "outcome": "failure"}, float64(len(records)))
		return 0, fmt.Errorf("publish %d audit records: %w", len(records), err)
	}
	metrics.EvidenceShipped.Add(map[string]string{"transport": "kafka", "outcome": "success"}, float64(len(records)))
	if err := s.writeCursor(next); err != nil {
		return 0, err
	}
	shipped = next.Offset
	return len(records), nil
}

func (s *KafkaShipper) readCursor() (kafkaCursor, error) {
	raw, err := os.ReadFile(s.cfg.CursorPath)
	if errors.Is(err, os.ErrNotExist) {
		return kafkaCursor{}, nil
	}
	if err != nil {
		return kafkaCursor{}, fmt.Errorf("read Kafka evidence cursor: %w", err)
	}
	var cur kafkaCursor
	if err := json.Unmarshal(raw, &cur); err != nil {
		return kafkaCursor{}, fmt.Errorf("Kafka evidence cursor %s is corrupt: %w", s.cfg.CursorPath, err)
	}
	return cur, nil
}

// writeCursor replaces the cursor atomically (write, fsync, rename).
func (s *KafkaShipper) writeCursor(cur kafkaCursor) error {
	raw, err := json.Marshal(cur)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.cfg.CursorPath), filepath.Base(s.cfg.CursorPath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("write Kafka evidence cursor: %w", err)
	}
	if _, err := tmp.Write(raw); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("write Kafka evidence cursor: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.cfg.CursorPath); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("write Kafka evidence cursor: %w", err)
	}
	return nil
}

// JournalRecord is the Kafka record one journal record becomes (contract/audit-events): the
// hash-only evidence body identified by the record's EvidenceDigest, keyed by trace so a trace's
// events stay in order on one partition, with the headers Ernest's ingestor reads first.
func JournalRecord(rec Record, aiSystemID string, evidenceKey []byte) (*kgo.Record, error) {
	if len(evidenceKey) == 0 {
		return nil, errors.New("no evidence key: audit records cannot leave AOS undigested")
	}
	digest := EvidenceDigest(evidenceKey, rec.Hash)
	evidence, err := BuildEvidence(rec.Event, digest, aiSystemID, map[string]any{
		"journalSequence": rec.Sequence,
		"transport":       "kafka",
	})
	if err != nil {
		return nil, err
	}
	key := rec.Event.TraceID
	if key == "" {
		key = digest
	}
	headers := []kgo.RecordHeader{
		{Key: "source", Value: []byte("aos")},
		{Key: "eventType", Value: []byte(evidence.EventType)},
		{Key: "sourceEventId", Value: []byte(evidence.SourceEventID)},
	}
	// The flow and the trace context that wrote the entry: the ingestor's processing is a new
	// span linked to it.
	if evidence.CorrelationID != "" {
		headers = append(headers, kgo.RecordHeader{Key: "correlationId", Value: []byte(evidence.CorrelationID)})
	}
	if evidence.Traceparent != "" {
		headers = append(headers, kgo.RecordHeader{Key: "traceparent", Value: []byte(evidence.Traceparent)})
	}
	return &kgo.Record{Key: []byte(key), Value: evidence.Body, Headers: headers}, nil
}
