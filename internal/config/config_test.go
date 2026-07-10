package config

import (
    "os"
    "path/filepath"
    "runtime"
    "testing"
)

// chdirToRepoRoot ensures relative paths like "definitions/..." resolve during tests
func chdirToRepoRoot(t *testing.T) {
    t.Helper()
    _, file, _, _ := runtime.Caller(0)
    // internal/config/config_test.go -> repo root is two levels up
    root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
    if err := os.Chdir(root); err != nil {
        t.Fatalf("chdir to repo root: %v", err)
    }
}

func TestLoadFromDir_Success(t *testing.T) {
    chdirToRepoRoot(t)
    cfg, err := LoadFromDir("definitions")
    if err != nil {
        t.Fatalf("LoadFromDir returned error: %v", err)
    }

    // Basic presence
    if len(cfg.Tools) == 0 || len(cfg.Pipelines) == 0 || len(cfg.Intents) == 0 {
        t.Fatalf("expected non-empty tools/pipelines/intents, got: %d/%d/%d", len(cfg.Tools), len(cfg.Pipelines), len(cfg.Intents))
    }

    // Known tool from repo
    tb, ok := cfg.Tools["banking.core_get_balance"]
    if !ok {
        t.Fatalf("expected tool banking.core_get_balance to be loaded")
    }
    if tb.Method != "GET" || tb.Mode != "read" {
        t.Fatalf("unexpected tool fields: %+v", tb)
    }

    // Known pipeline
    pb, ok := cfg.Pipelines["pipeline_send_bizum"]
    if !ok {
        t.Fatalf("expected pipeline_send_bizum to be loaded")
    }
    if len(pb.Steps) < 2 {
        t.Fatalf("pipeline_send_bizum should have multiple steps: %+v", pb)
    }
    // Ensure it contains the dangerous payment tool as defined
    found := false
    for _, s := range pb.Steps {
        if s.Tool == "banking.payments_bizum_send" {
            found = true
            break
        }
    }
    if !found {
        t.Fatalf("pipeline_send_bizum missing expected tool banking.payments_bizum_send")
    }

    // Known intent
    ib, ok := cfg.Intents["banking.send_bizum"]
    if !ok {
        t.Fatalf("expected intent banking.send_bizum to be loaded")
    }
    if !ib.AllowDangerous || !ib.RequiresAmount || !ib.RequiresPhone || ib.MaxAmount != 100 {
        t.Fatalf("unexpected intent fields: %+v", ib)
    }
}

func TestLoadFromDir_NotFound(t *testing.T) {
    chdirToRepoRoot(t)
    if _, err := LoadFromDir("non-existent-dir-12345"); err == nil {
        t.Fatalf("expected error when loading from non-existent dir")
    }
}
