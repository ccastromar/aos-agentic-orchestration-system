package audit

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Record is one tamper-evident entry in the local audit journal. Hash commits
// to the sequence, previous hash, and complete event payload.
type Record struct {
	Sequence uint64 `json:"sequence"`
	Previous string `json:"previous_hash,omitempty"`
	Event    Event  `json:"event"`
	Hash     string `json:"hash"`
}

type hashInput struct {
	Sequence uint64 `json:"sequence"`
	Previous string `json:"previous_hash,omitempty"`
	Event    Event  `json:"event"`
}

type Verification struct {
	Valid      bool   `json:"valid"`
	Records    uint64 `json:"records"`
	HeadHash   string `json:"head_hash,omitempty"`
	BrokenLine int    `json:"broken_line,omitempty"`
}

// ExecutionVerification validates that one trace is present in a valid journal
// and that its pipeline snapshot and recorded step inputs match their digests.
type ExecutionVerification struct {
	Valid           bool   `json:"valid"`
	TraceID         string `json:"trace_id"`
	Records         int    `json:"records"`
	PipelineVersion string `json:"pipeline_version,omitempty"`
	ToolCalls       int    `json:"tool_calls"`
	Error           string `json:"error,omitempty"`
}

// HashProvider exposes the most recent audit hash associated with one trace.
// It lets a workflow anchor the evidence accumulated so far to an external
// ledger while keeping the audit implementation pluggable.
type HashProvider interface {
	HashForTrace(traceID string) string
}

type FileChain struct {
	mu          sync.Mutex
	file        *os.File
	path        string
	sequence    uint64
	head        string
	traceHashes map[string]string
	lastErr     error
}

func NewFileChain(path string) (*FileChain, error) {
	if path == "" {
		return nil, errors.New("audit chain path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	verified, err := VerifyFile(path)
	if err != nil {
		return nil, fmt.Errorf("verify existing audit chain: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	chain := &FileChain{file: file, path: path, sequence: verified.Records, head: verified.HeadHash, traceHashes: map[string]string{}}
	if err := chain.loadTraceHeads(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return chain, nil
}

func (c *FileChain) Log(event Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastErr != nil {
		return
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	} else {
		event.Timestamp = event.Timestamp.UTC()
	}
	sequence := c.sequence + 1
	hash, err := hashRecord(sequence, c.head, event)
	if err != nil {
		c.lastErr = err
		log.Printf("AUDIT chain hash error: %v", err)
		return
	}
	record := Record{Sequence: sequence, Previous: c.head, Event: event, Hash: hash}
	encoded, err := json.Marshal(record)
	if err != nil {
		c.lastErr = err
		log.Printf("AUDIT chain marshal error: %v", err)
		return
	}
	if c.file == nil {
		c.lastErr = errors.New("audit chain is closed")
		return
	}
	if _, err := c.file.Write(append(encoded, '\n')); err != nil {
		c.lastErr = err
		log.Printf("AUDIT chain write error: %v", err)
		return
	}
	if err := c.file.Sync(); err != nil {
		c.lastErr = err
		log.Printf("AUDIT chain sync error: %v", err)
		return
	}
	c.sequence, c.head = sequence, hash
	if event.TraceID != "" {
		c.traceHashes[event.TraceID] = hash
	}
	log.Printf("AUDIT_CHAIN %s", string(encoded))
}

func (c *FileChain) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastErr
}

func (c *FileChain) HashForTrace(traceID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.traceHashes[traceID]
}

func (c *FileChain) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.file == nil {
		return nil
	}
	err := c.file.Close()
	c.file = nil
	return err
}

func VerifyFile(path string) (Verification, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Verification{Valid: true}, nil
	}
	if err != nil {
		return Verification{}, err
	}
	defer file.Close()
	return Verify(file)
}

func VerifyExecutionFile(path, traceID string) (ExecutionVerification, error) {
	result := ExecutionVerification{TraceID: traceID}
	chain, err := VerifyFile(path)
	if err != nil || !chain.Valid {
		return result, fmt.Errorf("audit chain verification failed: %w", err)
	}
	file, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 8<<20)
	manifestFound := false
	for scanner.Scan() {
		var record Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return result, err
		}
		if record.Event.TraceID != traceID {
			continue
		}
		result.Records++
		if record.Event.Phase == "pipeline.started" {
			version, _ := record.Event.Details["pipeline_version"].(string)
			snapshot := record.Event.Details["pipeline_snapshot"]
			if version == "" || Digest(snapshot) != version {
				return invalidExecution(result, "pipeline snapshot does not match its version digest")
			}
			inputs, _ := record.Event.Details["inputs"].(map[string]any)
			inputDigest, _ := record.Event.Details["input_digest"].(string)
			if inputDigest == "" || Digest(inputs) != inputDigest {
				return invalidExecution(result, "pipeline inputs do not match their digest")
			}
			if manifestFound && result.PipelineVersion != version {
				return invalidExecution(result, "trace contains conflicting pipeline versions")
			}
			manifestFound = true
			result.PipelineVersion = version
		}
		if record.Event.Phase == "tool.started" {
			inputs, _ := record.Event.Details["inputs"].(map[string]any)
			inputDigest, _ := record.Event.Details["input_digest"].(string)
			if inputDigest == "" || Digest(inputs) != inputDigest {
				return invalidExecution(result, "tool inputs do not match their digest")
			}
			result.ToolCalls++
		}
		if record.Event.Phase == "tool.completed" {
			output, _ := record.Event.Details["result"].(map[string]any)
			outputDigest, _ := record.Event.Details["output_digest"].(string)
			if outputDigest == "" || Digest(output) != outputDigest {
				return invalidExecution(result, "tool result does not match its digest")
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	if !manifestFound {
		return invalidExecution(result, "pipeline manifest not found for trace")
	}
	result.Valid = true
	return result, nil
}

func invalidExecution(result ExecutionVerification, message string) (ExecutionVerification, error) {
	result.Valid = false
	result.Error = message
	return result, errors.New(message)
}

func Verify(reader io.Reader) (Verification, error) {
	result := Verification{Valid: true}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 8<<20)
	previous := ""
	line := 0
	for scanner.Scan() {
		line++
		var record Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return Verification{Valid: false, Records: result.Records, HeadHash: result.HeadHash, BrokenLine: line}, fmt.Errorf("invalid audit record at line %d: %w", line, err)
		}
		if record.Sequence != result.Records+1 || record.Previous != previous {
			return Verification{Valid: false, Records: result.Records, HeadHash: result.HeadHash, BrokenLine: line}, fmt.Errorf("audit chain link mismatch at line %d", line)
		}
		want, err := hashRecord(record.Sequence, record.Previous, record.Event)
		if err != nil {
			return Verification{}, err
		}
		if record.Hash != want {
			return Verification{Valid: false, Records: result.Records, HeadHash: result.HeadHash, BrokenLine: line}, fmt.Errorf("audit record hash mismatch at line %d", line)
		}
		result.Records++
		previous = record.Hash
		result.HeadHash = record.Hash
	}
	if err := scanner.Err(); err != nil {
		return Verification{}, err
	}
	return result, nil
}

func hashRecord(sequence uint64, previous string, event Event) (string, error) {
	encoded, err := json.Marshal(hashInput{Sequence: sequence, Previous: previous, Event: event})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func (c *FileChain) loadTraceHeads() error {
	file, err := os.Open(c.path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 8<<20)
	for scanner.Scan() {
		var record Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return err
		}
		if record.Event.TraceID != "" {
			c.traceHashes[record.Event.TraceID] = record.Hash
		}
	}
	return scanner.Err()
}
