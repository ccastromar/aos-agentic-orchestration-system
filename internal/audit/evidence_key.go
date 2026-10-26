package audit

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/logx"
)

// MinEvidenceKeyBytes is the shortest evidence key accepted outside development.
const MinEvidenceKeyBytes = 32

// EvidenceDigest is what leaves AOS in place of an audit record's hash: HMAC-SHA256 of the hash
// under the installation's evidence key. The record hash covers who ran what (user, roles,
// parameters), and everything else that goes into it travels to Ernest, so a bare hash could be
// confirmed by guessing a few users and values. With the key it cannot; an auditor given the key
// can still match every local record with its evidence in Ernest (contract/audit-events).
func EvidenceDigest(key []byte, recordHash string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(recordHash))
	return hex.EncodeToString(mac.Sum(nil))
}

// LoadEvidenceKey returns the evidence key: AOS_EVIDENCE_KEY (the key itself) or
// AOS_EVIDENCE_KEY_FILE (a file holding it, as a key vault or a container secret mounts it),
// never both. Outside development one of them is required and must be at least
// MinEvidenceKeyBytes long. In development, without either, the key is generated once and kept
// next to the journal (journalPath + ".evidence-key"), or only in memory without a journal.
//
// The key must not change: Ernest deduplicates replays by the digest, and an auditor needs the
// key that was in use to match old records. Rotate it only by keeping the old one for them.
func LoadEvidenceKey(key, keyFile, journalPath string, development bool) ([]byte, error) {
	key = strings.TrimSpace(key)
	keyFile = strings.TrimSpace(keyFile)
	var out []byte
	switch {
	case key != "" && keyFile != "":
		return nil, errors.New("set AOS_EVIDENCE_KEY or AOS_EVIDENCE_KEY_FILE, not both")
	case key != "":
		out = []byte(key)
	case keyFile != "":
		raw, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, fmt.Errorf("read AOS_EVIDENCE_KEY_FILE: %w", err)
		}
		out = []byte(strings.TrimSpace(string(raw)))
		if len(out) == 0 {
			return nil, fmt.Errorf("AOS_EVIDENCE_KEY_FILE %s is empty", keyFile)
		}
	case !development:
		return nil, errors.New("AOS_EVIDENCE_KEY or AOS_EVIDENCE_KEY_FILE is required outside development when evidence leaves AOS (Ernest ingestor or Kafka)")
	default:
		return developmentEvidenceKey(journalPath)
	}
	if !development && len(out) < MinEvidenceKeyBytes {
		return nil, fmt.Errorf("the evidence key must be at least %d bytes outside development (got %d)", MinEvidenceKeyBytes, len(out))
	}
	return out, nil
}

func developmentEvidenceKey(journalPath string) ([]byte, error) {
	if journalPath == "" {
		logx.Warn("Audit", "no evidence key set: using one for this process only (development); set AOS_EVIDENCE_KEY or AOS_EVIDENCE_KEY_FILE to keep digests stable")
		return randomKey()
	}
	path := journalPath + ".evidence-key"
	if raw, err := os.ReadFile(path); err == nil {
		if k := strings.TrimSpace(string(raw)); k != "" {
			return []byte(k), nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read development evidence key: %w", err)
	}
	key, err := randomKey()
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create development evidence key: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(key, '\n')); err != nil {
		return nil, fmt.Errorf("write development evidence key: %w", err)
	}
	logx.Warn("Audit", "generated a development evidence key at %s; outside development set AOS_EVIDENCE_KEY or AOS_EVIDENCE_KEY_FILE", path)
	return key, nil
}

func randomKey() ([]byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("generate evidence key: %w", err)
	}
	return []byte(hex.EncodeToString(raw)), nil
}
