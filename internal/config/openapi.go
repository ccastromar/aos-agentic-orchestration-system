package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/logx"
	"gopkg.in/yaml.v3"
)

// openAPISpec is a minimal struct to extract paths and methods from an OpenAPI JSON/YAML.
type openAPISpec struct {
	Servers []struct {
		URL string `json:"url" yaml:"url"`
	} `json:"servers" yaml:"servers"`
	Paths map[string]map[string]struct {
		OperationID string `json:"operationId" yaml:"operationId"`
	} `json:"paths" yaml:"paths"`
}

func loadPluginsDir(dir string, cfg *Config) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		logx.Error("Config", "loading plugins dir: %v", err)
		return fmt.Errorf("loading plugins dir: %w", err)
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
			Plugins []Plugin `yaml:"plugins"`
		}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			logx.Error("Config", "parsing plugins dir: %v", err)
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		for _, p := range raw.Plugins {
			cfg.Plugins[p.Name] = p
			if p.Type == "openapi" {
				if err := loadOpenAPIPlugin(dir, p, cfg); err != nil {
					logx.Error("Config", "error loading openapi plugin %s: %v", p.Name, err)
				}
			}
		}
	}
	return nil
}

// openAPIParamRegex matches {param} in URL paths
var openAPIParamRegex = regexp.MustCompile(`\{([^}]+)\}`)

func loadOpenAPIPlugin(baseDir string, plugin Plugin, cfg *Config) error {
	if plugin.File == "" {
		return fmt.Errorf("openapi plugin requires a 'file' property")
	}

	specPath := filepath.Join(baseDir, plugin.File)
	data, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("reading openapi spec %s: %w", specPath, err)
	}

	var spec openAPISpec
	// Try JSON first, fallback to YAML
	if err := json.Unmarshal(data, &spec); err != nil {
		if err2 := yaml.Unmarshal(data, &spec); err2 != nil {
			return fmt.Errorf("parsing openapi spec (not valid json or yaml): %w", err2)
		}
	}

	baseURL := plugin.BaseURL
	if baseURL == "" && len(spec.Servers) > 0 {
		baseURL = spec.Servers[0].URL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	for path, methods := range spec.Paths {
		for method, op := range methods {
			// skip non-HTTP methods like "parameters"
			methodLower := strings.ToLower(method)
			if methodLower != "get" && methodLower != "post" && methodLower != "put" && methodLower != "delete" && methodLower != "patch" {
				continue
			}

			opID := op.OperationID
			if opID == "" {
				// generate fallback operation ID like "get_users" from "/users"
				sanitizedPath := strings.ReplaceAll(path, "/", "_")
				sanitizedPath = strings.ReplaceAll(sanitizedPath, "{", "")
				sanitizedPath = strings.ReplaceAll(sanitizedPath, "}", "")
				sanitizedPath = strings.Trim(sanitizedPath, "_")
				opID = methodLower + "_" + sanitizedPath
			}

			toolName := plugin.Name + "." + opID

			// Convert OpenAPI path params: /users/{id} -> /users/{{ .id }}
			renderedPath := openAPIParamRegex.ReplaceAllString(path, "{{ .$1 }}")
			if !strings.HasPrefix(renderedPath, "/") {
				renderedPath = "/" + renderedPath
			}

			tool := Tool{
				Name:    toolName,
				Type:    "http",
				Method:  strings.ToUpper(method),
				URL:     baseURL + renderedPath,
				Mode:    "read", // by default, could be derived from method (GET=read, POST=write)
				Headers: make(map[string]string),
			}

			if tool.Method != "GET" {
				tool.Mode = "write"
			}

			for k, v := range plugin.Headers {
				tool.Headers[k] = v
			}

			// Add the tool to config
			cfg.Tools[toolName] = tool
			logx.Debug("Config", "Loaded dynamic tool: %s (%s %s)", toolName, tool.Method, tool.URL)
		}
	}

	return nil
}
