package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ccastromar/aos-agent-orchestration-system/internal/logx"
	"gopkg.in/yaml.v3"
)

type Tool struct {
	Name      string            `yaml:"name"`
	Type      string            `yaml:"type"` // http, cli, etc (solo http en v2)
	Method    string            `yaml:"method"`
	URL       string            `yaml:"url"`
	Mode      string            `yaml:"mode"` // read, write, dangerous
	TimeoutMs int               `yaml:"timeout"`
	Body      map[string]string `yaml:"body"`
	Model     string            `yaml:"model"`
	Headers   map[string]string `yaml:"headers"`
}

type PipelineStep struct {
	Tool       string            `yaml:"tool"`
	WithParams map[string]string `yaml:"with_params"`
	Analyst    bool              `yaml:"analyst"`
	HumanGate  string            `yaml:"human_approval,omitempty" json:"human_approval,omitempty"`
	When       string            `yaml:"when,omitempty" json:"when,omitempty"`
}

type Pipeline struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Steps       []PipelineStep `yaml:"steps"`
}

type Intent struct {
	Type           string   `yaml:"type"`
	Description    string   `yaml:"description"`
	Pipeline       string   `yaml:"pipeline"`
	RequiredParams []string `yaml:"required_params"`
	// ----- Guard-Rails -----
	AllowDangerous bool    `yaml:"allow_dangerous"` // puede ejecutar tools peligrosas
	RequiresAmount bool    `yaml:"requires_amount"` // debe venir "amount"
	RequiresPhone  bool    `yaml:"requires_phone"`  // debe venir "toPhone"
	MaxAmount      float64 `yaml:"max_amount"`      // límite permitido por operación
	ShadowMode     bool    `yaml:"shadow_mode"`     // ejecutar en modo simulación

}

type Config struct {
	Tools     map[string]Tool
	Pipelines map[string]Pipeline
	Intents   map[string]Intent
}

func LoadFromDir(base string) (*Config, error) {
	cfg := &Config{
		Tools:     make(map[string]Tool),
		Pipelines: make(map[string]Pipeline),
		Intents:   make(map[string]Intent),
	}

	if err := loadToolsDir(filepath.Join(base, "tools"), cfg); err != nil {
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
