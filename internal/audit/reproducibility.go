package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
)

// PipelineSnapshot is the sanitized, canonical configuration that identifies
// the behavior of one pipeline run. Credentials are removed before hashing.
type PipelineSnapshot struct {
	SchemaVersion int                               `json:"schema_version"`
	Pipeline      config.Pipeline                   `json:"pipeline"`
	Tools         map[string]config.Tool            `json:"tools,omitempty"`
	Agents        map[string]config.AgentDefinition `json:"agents,omitempty"`
}

// BuildPipelineSnapshot captures a pipeline and the definitions it can invoke.
// The returned version is stable for semantically identical YAML definitions.
func BuildPipelineSnapshot(pipeline config.Pipeline, cfg *config.Config) (json.RawMessage, string, error) {
	snapshot := PipelineSnapshot{SchemaVersion: 1, Pipeline: pipeline, Tools: map[string]config.Tool{}, Agents: map[string]config.AgentDefinition{}}
	if cfg != nil {
		for _, step := range pipeline.Steps {
			if step.Tool != "" {
				if tool, ok := cfg.Tools[step.Tool]; ok {
					snapshot.Tools[step.Tool] = tool
				}
			}
			if step.Agent != "" {
				if agent, ok := cfg.Agents[step.Agent]; ok {
					snapshot.Agents[step.Agent] = agent
				}
			}
		}
	}
	clean, err := sanitizeValue(snapshot)
	if err != nil {
		return nil, "", err
	}
	encoded, err := json.Marshal(clean)
	if err != nil {
		return nil, "", err
	}
	return json.RawMessage(encoded), DigestBytes(encoded), nil
}

// Digest returns a SHA-256 digest of the JSON representation of value.
func Digest(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return DigestBytes(encoded)
}

func DigestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// SanitizeMap returns a copy safe for local audit evidence. Secret-like fields
// are replaced recursively; values remain in AOS state but are not duplicated
// in the append-only journal.
func SanitizeMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	clean, err := sanitizeValue(value)
	if err != nil {
		return map[string]any{"_serialization_error": err.Error()}
	}
	result, _ := clean.(map[string]any)
	return result
}

func sanitizeValue(value any) (any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal audit evidence: %w", err)
	}
	var normalized any
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return nil, fmt.Errorf("normalize audit evidence: %w", err)
	}
	return sanitizeJSON(normalized), nil
}

func sanitizeJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		clean := make(map[string]any, len(typed))
		for key, child := range typed {
			lower := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), " ", "_"))
			if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "api_key") || strings.Contains(lower, "apikey") || strings.Contains(lower, "authorization") || lower == "private_key" {
				clean[key] = "[REDACTED]"
				continue
			}
			if raw, ok := child.(string); ok && (lower == "url" || lower == "base_url" || lower == "target") {
				clean[key] = sanitizeURL(raw)
				continue
			}
			clean[key] = sanitizeJSON(child)
		}
		return clean
	case []any:
		clean := make([]any, len(typed))
		for i, child := range typed {
			clean[i] = sanitizeJSON(child)
		}
		return clean
	default:
		return value
	}
}

func sanitizeURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return raw
	}
	if parsed.User != nil {
		parsed.User = url.UserPassword("[REDACTED]", "[REDACTED]")
	}
	query := parsed.Query()
	for key := range query {
		lower := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "api_key") || strings.Contains(lower, "apikey") || lower == "key" || strings.Contains(lower, "authorization") {
			query.Set(key, "[REDACTED]")
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
