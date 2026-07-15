package guard

import (
	"fmt"
	"os"
	"regexp"
	"strconv"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
)

func isValidPhoneNumber(s string) bool {
	re := regexp.MustCompile(`^[0-9+][0-9]{5,14}$`)
	return re.MatchString(s)
}

func ValidateIntentPermissions(intent config.Intent, pipeline config.Pipeline, tools map[string]config.Tool) error {
	for _, step := range pipeline.Steps {

		if step.Tool == "" {
			continue
		}

		t, ok := tools[step.Tool]
		if !ok {
			return fmt.Errorf("tool %s not found", step.Tool)
		}

		if t.Mode == "dangerous" && !intent.AllowDangerous {
			return fmt.Errorf("intent '%s' not allowed this tool (tool=%s)", intent.Type, t.Name)
		}
	}
	return nil
}

func ValidateDangerousParams(intent config.Intent, params map[string]string) error {
	if !intent.AllowDangerous {
		return nil
	}

	if intent.RequiresAmount {
		raw := params["amount"]
		if raw == "" {
			return fmt.Errorf("required parameter: amount")
		}
		amount, err := strconv.ParseFloat(raw, 64)
		if err != nil || amount <= 0 {
			return fmt.Errorf("invalid amount: %s", raw)
		}
		if intent.MaxAmount > 0 && amount > intent.MaxAmount {
			return fmt.Errorf("amount exceeds allowed limit: %v > %v", amount, intent.MaxAmount)
		}
	}

	if intent.RequiresPhone {
		phone := params["toPhone"]
		if phone == "" {
			return fmt.Errorf("required parameter: toPhone")
		}
		if !isValidPhoneNumber(phone) {
			return fmt.Errorf("toPhone is not valid: %s", phone)
		}
	}

	return nil
}

func ValidateDangerousChain(pipeline config.Pipeline, tools map[string]config.Tool) error {
	if os.Getenv("ALLOW_LOCAL_TOOLS") == "true" {
		return nil
	}

	dangerousSeen := false

	for _, step := range pipeline.Steps {

		if step.Tool == "" {
			continue
		}

		t, ok := tools[step.Tool]
		if !ok {
			return fmt.Errorf("tool %s not found in chain check", step.Tool)
		}

		if t.Mode == "dangerous" {
			if dangerousSeen {
				return fmt.Errorf("pipeline '%s' is chaining danger tools", pipeline.Name)
			}
			dangerousSeen = true
		}
	}
	return nil
}

func ValidateAll(intent config.Intent, pipeline config.Pipeline, params map[string]string, tools map[string]config.Tool) error {
	if err := ValidateIntentPermissions(intent, pipeline, tools); err != nil {
		return err
	}
	if err := ValidateDangerousParams(intent, params); err != nil {
		return err
	}
	if err := ValidateDangerousChain(pipeline, tools); err != nil {
		return err
	}
	return nil
}
