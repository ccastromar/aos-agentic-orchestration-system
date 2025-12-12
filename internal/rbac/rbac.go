package rbac

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Permissions map[string][]string `yaml:"permissions"`
	Defaults    Defaults            `yaml:"defaults"`
}

type Defaults struct {
	UnknownIntent string `yaml:"unknown_intent"` // "deny" | "allow"
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("rbac: read file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("rbac: unmarshal yaml: %w", err)
	}

	if cfg.Defaults.UnknownIntent == "" {
		cfg.Defaults.UnknownIntent = "deny"
	}

	return &cfg, nil
}

func (c *Config) IsAllowed(intent string, userRoles []string) (bool, []string) {
	allowedRoles, ok := c.Permissions[intent]
	if !ok {
		if c.Defaults.UnknownIntent == "allow" {
			return true, nil
		}
		// deny por defecto
		return false, nil
	}

	match := intersect(userRoles, allowedRoles)
	return len(match) > 0, match
}

func intersect(a, b []string) []string {
	m := make(map[string]struct{}, len(a))
	for _, x := range a {
		m[x] = struct{}{}
	}
	var res []string
	for _, y := range b {
		if _, ok := m[y]; ok {
			res = append(res, y)
		}
	}
	return res
}
