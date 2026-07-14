package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ccastromar/aos-agent-orchestration-system/internal/logx"
	"gopkg.in/yaml.v3"
)

type Tool struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description,omitempty"`
	Type        string            `yaml:"type"` // http, cli (todo)
	Method    string            `yaml:"method"`
	URL       string            `yaml:"url"`
	Mode      string            `yaml:"mode"` // read, write, dangerous
	TimeoutMs int               `yaml:"timeout"`
	Body      map[string]string `yaml:"body"`
	Model     string            `yaml:"model"`
	Headers   map[string]string `yaml:"headers"`
}

type PipelineStep struct {
	ID         string            `yaml:"id,omitempty" json:"id,omitempty"`
	DependsOn  []string          `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
	Tool       string            `yaml:"tool" json:"tool,omitempty"`
	WithParams map[string]string `yaml:"with_params" json:"with_params,omitempty"`
	Analyst    bool              `yaml:"analyst" json:"analyst,omitempty"`
	HumanGate  string            `yaml:"human_approval,omitempty" json:"human_approval,omitempty"`
	When       string            `yaml:"when,omitempty" json:"when,omitempty"`
	Retries    int               `yaml:"retries,omitempty" json:"retries,omitempty"`
	TimeoutMs  int               `yaml:"timeout_ms,omitempty" json:"timeout_ms,omitempty"`
	BackoffMs  int               `yaml:"backoff_ms,omitempty" json:"backoff_ms,omitempty"`
}

type Pipeline struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Mode        string         `yaml:"mode"` // linear, dag
	Steps       []PipelineStep `yaml:"steps"`
}

type Intent struct {
	Type           string   `yaml:"type"`
	Description    string   `yaml:"description"`
	Pipeline       string   `yaml:"pipeline"`
	RequiredParams []string `yaml:"required_params"`
	// ----- Guard-Rails -----
	AllowDangerous bool    `yaml:"allow_dangerous"`
	RequiresAmount bool    `yaml:"requires_amount"`
	RequiresPhone  bool    `yaml:"requires_phone"`
	MaxAmount      float64 `yaml:"max_amount"`
	ShadowMode     bool    `yaml:"shadow_mode"`
}

type Plugin struct {
	Name    string            `yaml:"name"`
	Type    string            `yaml:"type"` // "openapi"
	File    string            `yaml:"file"` // Relative path to definition file
	BaseURL string            `yaml:"base_url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty"`
}

type Config struct {
	Tools     map[string]Tool
	Pipelines map[string]Pipeline
	Intents   map[string]Intent
	Plugins   map[string]Plugin
}

func LoadFromDir(base string) (*Config, error) {
	cfg := &Config{
		Tools:     make(map[string]Tool),
		Pipelines: make(map[string]Pipeline),
		Intents:   make(map[string]Intent),
		Plugins:   make(map[string]Plugin),
	}

	if err := loadToolsDir(filepath.Join(base, "tools"), cfg); err != nil {
		return nil, err
	}
	if err := loadPluginsDir(filepath.Join(base, "plugins"), cfg); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err := loadPipelinesDir(filepath.Join(base, "pipelines"), cfg); err != nil {
		return nil, err
	}
	if err := loadIntentsDir(filepath.Join(base, "intents"), cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func loadToolsDir(dir string, cfg *Config) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		logx.Error("Config", "loading tools dir: %v", err)
		return fmt.Errorf("loading tools dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var raw struct {
			Tools []Tool `yaml:"tools"`
		}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			logx.Error("Config", "parsing tools dir: %v", err)
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		for _, t := range raw.Tools {
			cfg.Tools[t.Name] = t
		}
	}
	return nil
}

func loadPipelinesDir(dir string, cfg *Config) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		logx.Error("Config", "loading pipelines dir: %v", err)
		return fmt.Errorf("loading pipelines dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var raw struct {
			Pipelines []Pipeline `yaml:"pipelines"`
		}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			logx.Error("Config", "parsing pipelines dir: %v", err)
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		for _, p := range raw.Pipelines {
			cfg.Pipelines[p.Name] = p
		}
	}
	return nil
}

func loadIntentsDir(dir string, cfg *Config) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		logx.Error("Config", "loading intents dir: %v", err)
		return fmt.Errorf("loading intents dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var raw struct {
			Intents []Intent `yaml:"intents"`
		}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			logx.Error("Config", "parsing tools dir: %v", err)
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		for _, it := range raw.Intents {
			cfg.Intents[it.Type] = it
		}
	}
	return nil
}
