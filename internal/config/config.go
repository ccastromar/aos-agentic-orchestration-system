package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/logx"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/rbac"
	"gopkg.in/yaml.v3"
)

type Tool struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description,omitempty"`
	Type        string            `yaml:"type"` // http, cli (todo)
	Method      string            `yaml:"method"`
	URL         string            `yaml:"url"`
	Mode        string            `yaml:"mode"` // read, write, dangerous
	TimeoutMs   int               `yaml:"timeout"`
	Body        map[string]string `yaml:"body"`
	Model       string            `yaml:"model"`
	Headers     map[string]string `yaml:"headers"`
	Target      string            `yaml:"target,omitempty"`    // gRPC address or MCP/A2A URL
	Service     string            `yaml:"service,omitempty"`   // gRPC fully-qualified service
	Operation   string            `yaml:"operation,omitempty"` // MCP operation or A2A method
	Agent       string            `yaml:"agent,omitempty"`     // A2A agent identifier
}

type PipelineStep struct {
	ID         string            `yaml:"id,omitempty" json:"id,omitempty"`
	Type       string            `yaml:"type,omitempty" json:"type,omitempty"`
	Agent      string            `yaml:"agent,omitempty" json:"agent,omitempty"`
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

// AgentDefinition names an external agent runtime adapter. Workflow-specific
// task, directory, limits, and permissions remain on the pipeline step.
type AgentDefinition struct {
	Name    string `yaml:"name" json:"name"`
	Runtime string `yaml:"runtime" json:"runtime"`
	Binary  string `yaml:"binary,omitempty" json:"binary,omitempty"`
}

type Pipeline struct {
	Name          string         `yaml:"name"`
	Description   string         `yaml:"description"`
	Mode          string         `yaml:"mode"` // linear, dag
	LLMModel      string         `yaml:"llm_model,omitempty" json:"llm_model,omitempty"`
	SkipSummary   bool           `yaml:"skip_summary,omitempty"`
	MaxIterations int            `yaml:"max_iterations,omitempty"`
	Steps         []PipelineStep `yaml:"steps"`
}

type Intent struct {
	Type           string   `yaml:"type"`
	Description    string   `yaml:"description"`
	Pipeline       string   `yaml:"pipeline"`
	RequiredParams []string `yaml:"required_params"`
	Examples       []string `yaml:"examples,omitempty" json:"examples,omitempty"`
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
	Agents    map[string]AgentDefinition
	Pipelines map[string]Pipeline
	Intents   map[string]Intent
	Plugins   map[string]Plugin
	RBAC      *rbac.Config
}

func LoadFromDir(base string) (*Config, error) {
	cfg := &Config{
		Tools:     make(map[string]Tool),
		Agents:    make(map[string]AgentDefinition),
		Pipelines: make(map[string]Pipeline),
		Intents:   make(map[string]Intent),
		Plugins:   make(map[string]Plugin),
	}

	if err := loadToolsDir(filepath.Join(base, "tools"), cfg); err != nil {
		return nil, err
	}
	if err := loadAgentsDir(filepath.Join(base, "agents"), cfg); err != nil && !os.IsNotExist(err) {
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

	rbacPath := filepath.Join(base, "rbac", "permissions.yaml")
	if rbacCfg, err := rbac.LoadConfig(rbacPath); err == nil {
		cfg.RBAC = rbacCfg
		logx.Info("Config", "Loaded RBAC from %s", rbacPath)
	} else if !os.IsNotExist(err) {
		logx.Error("Config", "Failed to load RBAC: %v", err)
	} else {
		// Provide default config if file is missing
		cfg.RBAC = &rbac.Config{
			Defaults: rbac.Defaults{UnknownIntent: "deny"},
		}
	}

	return cfg, nil
}

func loadAgentsDir(dir string, cfg *Config) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var raw struct {
			Agents []AgentDefinition `yaml:"agents"`
		}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		for _, agent := range raw.Agents {
			if agent.Name == "" || agent.Runtime == "" {
				return fmt.Errorf("agent in %s must define name and runtime", path)
			}
			cfg.Agents[agent.Name] = agent
		}
	}
	return nil
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
		data = []byte(expandEnvRefs(string(data)))
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
			if it.Type == "" {
				logx.Warn("Config", "skipping intent with empty type in %s (wrong schema?)", path)
				continue
			}
			cfg.Intents[it.Type] = it
		}
	}
	return nil
}
