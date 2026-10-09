package audit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileChainAppendsVerifiableRecordsAndDetectsTampering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	chain, err := NewFileChain(path)
	if err != nil {
		t.Fatal(err)
	}
	chain.Log(Event{Timestamp: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), TraceID: "trace-1", Phase: "intent.accepted", Result: "success"})
	firstHash := chain.HashForTrace("trace-1")
	if len(firstHash) != 64 {
		t.Fatalf("expected SHA-256 hash, got %q", firstHash)
	}
	chain.Log(Event{Timestamp: time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC), TraceID: "trace-1", Phase: "callback.received", Result: "success"})
	secondHash := chain.HashForTrace("trace-1")
	if secondHash == firstHash {
		t.Fatal("expected trace head to advance")
	}
	if err := chain.Close(); err != nil {
		t.Fatal(err)
	}

	verified, err := VerifyFile(path)
	if err != nil || !verified.Valid || verified.Records != 2 || verified.HeadHash != secondHash {
		t.Fatalf("unexpected verification result: %#v, %v", verified, err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := range contents {
		if contents[i] == 's' {
			contents[i] = 'x'
			break
		}
	}
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyFile(path); err == nil {
		t.Fatal("expected tampered audit chain to fail verification")
	}
}

func TestFileChainReopensAndContinuesPreviousHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	first, err := NewFileChain(path)
	if err != nil {
		t.Fatal(err)
	}
	first.Log(Event{TraceID: "one", Phase: "start", Result: "success"})
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := NewFileChain(path)
	if err != nil {
		t.Fatal(err)
	}
	second.Log(Event{TraceID: "two", Phase: "finish", Result: "success"})
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyFile(path)
	if err != nil || verified.Records != 2 {
		t.Fatalf("expected a valid two-record chain: %#v, %v", verified, err)
	}
}
