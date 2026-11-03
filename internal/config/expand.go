package config

import (
	"os"
	"regexp"
	"strings"
)

// envRefPattern matches ${NAME} and ${NAME:-default} where NAME is a valid environment variable
// name. Anything else in ${...} is left alone: a step condition such as `when: ${amount > 1000}`
// uses the same braces, and expanding it as a variable would erase the condition.
var envRefPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-[^}]*)?\}`)

// expandEnvRefs replaces ${VAR} and ${VAR:-default} references with the value
// of the environment variable, docker-compose style. Only the braced form is
// expanded, so Go template actions ({{ .param }}) and literal dollars are left
// untouched. An unset VAR without a default expands to the empty string.
func expandEnvRefs(s string) string {
	return envRefPattern.ReplaceAllStringFunc(s, func(ref string) string {
		m := envRefPattern.FindStringSubmatch(ref)
		name, def := m[1], strings.TrimPrefix(m[2], ":-")
		if v, ok := os.LookupEnv(name); ok && v != "" {
			return v
		}
		return def
	})
}
