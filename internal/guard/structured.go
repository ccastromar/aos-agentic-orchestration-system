package guard

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
)

// ValidateStructuredParams checks a caller-built request. The LLM path checks required params
// while detecting the intent; an explicit operation reaches no such check.
func ValidateStructuredParams(intent config.Intent, params map[string]string, nonString []string) error {
	if len(nonString) > 0 {
		slices.Sort(nonString)
		return fmt.Errorf("parameters must be strings: %s", strings.Join(nonString, ", "))
	}
	var missing []string
	for _, name := range intent.RequiredParams {
		if strings.TrimSpace(params[name]) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required parameters: %s", strings.Join(missing, ", "))
	}
	return nil
}
