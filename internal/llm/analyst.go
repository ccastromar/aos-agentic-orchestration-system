package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ccastromar/aos-agent-orchestration-system/internal/logx"
)

// ---- PUBLIC API ----

func SummarizeResult(
	ctx context.Context,
	c LLMClient,
	intentType string,
	rawResult map[string]any,
	targetLanguage string,
) (string, error) {

	prompt, err := buildSummarizePrompt(intentType, rawResult, targetLanguage)
	if err != nil {
		return "", err
	}

	logx.Debug("Analyst", prompt)

	out, err := c.Chat(ctx, prompt)
	if err != nil {
		return "", err
	}

	return out, nil
}

// ---- PROMPT COMPOSITION ----

func buildSummarizePrompt(
	intentType string,
	rawResult map[string]any,
	targetLanguage string,
) (string, error) {

	rawJSON, err := json.Marshal(rawResult)
	if err != nil {
		return "", err
	}

	basePrompt := summarizeBasePrompt(intentType, string(rawJSON))

	renderingRules, err := renderingPrompt(targetLanguage)
	if err != nil {
		return "", err
	}

	return basePrompt + "\n" + renderingRules, nil
}

// ---- SEMANTIC CORE (LANGUAGE-AGNOSTIC) ----

func summarizeBasePrompt(intentType string, rawJSON string) string {
	return fmt.Sprintf(`
You are a multi-domain assistant (banking, devops, CRM, helpdesk, healthcare).
You have executed a workflow with intent: "%s".

Very important:
1. Each step includes an "executed" field set to true or false.
2. If "executed": false on the MAIN operation step (for example: transfer, payment, deployment, etc.),
   then the operation WAS NOT completed.
3. In that case, you must clearly explain that the operation did NOT occur and MUST NOT state that it was successful.
4. Secondary steps such as notifications, logs, or validations may have executed even if the main operation did not.
5. Interpret the JSON strictly as provided. DO NOT invent behavior or outcomes.

Here are the raw execution results (JSON):

%s

The summary must explain:
- whether the main operation was completed or not,
- what happened during the workflow,
- whether any steps were skipped or conditions prevented continuation,
- any relevant warnings, risks, or notifications.

Return PLAIN TEXT ONLY.
Do NOT return JSON.
Do NOT use lists.
`, intentType, rawJSON)
}

// ---- RENDERING (LAST STEP, LANGUAGE-SPECIFIC) ----

func renderingPrompt(lang string) (string, error) {
	switch lang {

	case "es":
		return `
OUTPUT LANGUAGE REQUIREMENT:
- Write the final summary entirely in Spanish.
- Write as a native Spanish speaker.
- Do NOT translate literally from English.
- Use clear, professional, and natural Spanish.
- DO NOT use any other language.
- If you cannot comply, return an empty response.
`, nil

	case "en":
		return `
OUTPUT LANGUAGE REQUIREMENT:
- Write the final summary entirely in English.
- Write as a native English speaker.
- Use clear, professional language.
- DO NOT use any other language.
- If you cannot comply, return an empty response.
`, nil

	default:
		return "", fmt.Errorf("unsupported language: %s", lang)
	}
}
