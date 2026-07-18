package config

import (
	"os"
	"regexp"
	"strings"
)

var envRefPattern = regexp.MustCompile(`\$\{[^}]+\}`)

// expandEnvRefs replaces ${VAR} and ${VAR:-default} references with the value
// of the environment variable, docker-compose style. Only the braced form is
// expanded, so Go template actions ({{ .param }}) and literal dollars are left
// untouched. An unset VAR without a default expands to the empty string.
func expandEnvRefs(s string) string {
	return envRefPattern.ReplaceAllStringFunc(s, func(ref string) string {
		key := ref[2 : len(ref)-1] // strip ${ and }
		name, def, hasDef := strings.Cut(key, ":-")
		if v, ok := os.LookupEnv(name); ok && v != "" {
			return v
		}
		if hasDef {
			return def
		}
		return ""
	})
}
