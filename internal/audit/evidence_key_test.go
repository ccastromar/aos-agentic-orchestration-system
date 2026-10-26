package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvidenceDigestIsKeyed(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	a, b := EvidenceDigest([]byte("key-a"), hash), EvidenceDigest([]byte("key-b"), hash)
	if a == b || a == hash || len(a) != 64 {
		t.Fatalf("digests %s / %s of %s", a, b, hash)
	}
	if EvidenceDigest([]byte("key-a"), hash) != a {
		t.Fatal("the digest must be stable for a key")
	}
}

func TestLoadEvidenceKey(t *testing.T) {
	long := strings.Repeat("k", MinEvidenceKeyBytes)
	dir := t.TempDir()
	file := filepath.Join(dir, "key")
	if err := os.WriteFile(file, []byte(long+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if k, err := LoadEvidenceKey(long, "", "", false); err != nil || string(k) != long {
		t.Fatalf("from the variable: %q, %v", k, err)
	}
	if k, err := LoadEvidenceKey("", file, "", false); err != nil || string(k) != long {
		t.Fatalf("from a mounted file: %q, %v", k, err)
	}
	if _, err := LoadEvidenceKey(long, file, "", false); err == nil {
		t.Fatal("both set must be refused")
	}
	if _, err := LoadEvidenceKey("", "", filepath.Join(dir, "journal.jsonl"), false); err == nil {
		t.Fatal("outside development a key is required")
	}
	if _, err := LoadEvidenceKey("short", "", "", false); err == nil {
		t.Fatal("outside development a short key must be refused")
	}
	empty := filepath.Join(dir, "empty")
	_ = os.WriteFile(empty, []byte("\n"), 0o600)
	if _, err := LoadEvidenceKey("", empty, "", true); err == nil {
		t.Fatal("an empty key file must be refused")
	}
}

func TestDevelopmentEvidenceKeyIsKeptNextToTheJournal(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "audit.jsonl")
	first, err := LoadEvidenceKey("", "", journal, true)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(journal + ".evidence-key")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode %v, want 0600", info.Mode().Perm())
	}
	again, err := LoadEvidenceKey("", "", journal, true)
	if err != nil || string(again) != string(first) {
		t.Fatalf("a restart must reuse the key: %q vs %q (%v)", again, first, err)
	}
}

func TestEvidenceCannotLeaveWithoutAKey(t *testing.T) {
	if _, err := JournalRecord(Record{Hash: "h"}, "", nil); err == nil {
		t.Fatal("JournalRecord without a key")
	}
	if _, err := HTTPEvidence(Event{}, "", nil); err == nil {
		t.Fatal("HTTPEvidence without a key")
	}
	if _, err := NewKafkaShipper(KafkaShipperConfig{JournalPath: "j", Brokers: []string{"b"}}); err == nil {
		t.Fatal("a Kafka shipper without a key")
	}
}
