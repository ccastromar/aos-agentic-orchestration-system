package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExpandEnvRefs(t *testing.T) {
	t.Setenv("AOS_TEST_BASE", "http://mocks:8080")

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"set var", "${AOS_TEST_BASE}/mock/x", "http://mocks:8080/mock/x"},
		{"set var ignores default", "${AOS_TEST_BASE:-http://localhost:9000}/mock/x", "http://mocks:8080/mock/x"},
		{"unset var uses default", "${AOS_TEST_UNSET:-http://localhost:9000}/mock/x", "http://localhost:9000/mock/x"},
		{"unset var no default", "${AOS_TEST_UNSET}/mock/x", "/mock/x"},
		{"go template untouched", "${AOS_TEST_UNSET:-http://localhost:9000}/mock/x?id={{ .id }}", "http://localhost:9000/mock/x?id={{ .id }}"},
		{"plain string untouched", "http://localhost:9000/mock/x", "http://localhost:9000/mock/x"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := expandEnvRefs(tc.in); got != tc.want {
				t.Errorf("expandEnvRefs(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestPipelineExpandsEnvironmentReferences(t *testing.T) {
	t.Setenv("AOS_TEST_SYSTEM", "system-123")
	dir := t.TempDir()
	for _, name := range []string{"tools", "pipelines", "intents"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	data := []byte("pipelines:\n  - name: demo\n    ai_system: ${AOS_TEST_SYSTEM}\n    steps: []\n")
	if err := os.WriteFile(filepath.Join(dir, "pipelines", "demo.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Pipelines["demo"].AiSystem; got != "system-123" {
		t.Fatalf("AiSystem = %q, want system-123", got)
	}
}

func TestGovernedPipelineFlagIsLoaded(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"tools", "pipelines", "intents"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	data := []byte("pipelines:\n  - name: governed\n    ai_system: sys-1\n    governed: true\n    steps: []\n  - name: plain\n    steps: []\n")
	if err := os.WriteFile(filepath.Join(dir, "pipelines", "demo.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Pipelines["governed"].Governed {
		t.Fatal("governed: true was not loaded")
	}
	if cfg.Pipelines["plain"].Governed {
		t.Fatal("a pipeline without the flag must not be governed")
	}
}

// The live-demo pipelines must stay governed and bound to their own Prudence system: without
// the flag, an unset env var would silently make them run ungoverned.
func TestGovernanceDemoPipelinesAreGoverned(t *testing.T) {
	want := map[string]string{
		"pipeline_aos_support_lookup":       "AOS_PRUDENCE_DEMO_SYSTEM_ID",
		"pipeline_aos_credit_pre_screening": "AOS_PRUDENCE_CREDIT_SYSTEM_ID",
		"pipeline_aos_social_scoring":       "AOS_PRUDENCE_SOCIAL_SCORING_SYSTEM_ID",
		"pipeline_aos_translate_message":    "AOS_PRUDENCE_TRANSLATOR_SYSTEM_ID",
	}
	for _, env := range want {
		t.Setenv(env, "id-from-"+env)
	}
	// Resolve from this file, not the cwd: another test in this package chdirs to the repo root.
	_, file, _, _ := runtime.Caller(0)
	cfg, err := LoadFromDir(filepath.Join(filepath.Dir(file), "..", "..", "definitions"))
	if err != nil {
		t.Fatal(err)
	}
	for name, env := range want {
		pipe, ok := cfg.Pipelines[name]
		if !ok {
			t.Errorf("%s: pipeline missing from definitions", name)
			continue
		}
		if !pipe.Governed {
			t.Errorf("%s: must declare governed: true", name)
		}
		if pipe.AiSystem != "id-from-"+env {
			t.Errorf("%s: ai_system = %q, want it bound to ${%s}", name, pipe.AiSystem, env)
		}
	}
}
