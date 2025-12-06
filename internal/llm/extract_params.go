package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

func ExtractParams(ctx context.Context, client LLMClient, userMsg string, required []string) (map[string]string, error) {
	paramsJSON, _ := json.Marshal(required)

	prompt := fmt.Sprintf(`
Extract ONLY the required parameters from the user message.

Requirements:
- Output MUST be valid JSON.
- JSON MUST contain EXACTLY these keys:
  %s
- NO markdown.
- NO backticks.
- NO explanation.
- NO prefix.
- NO suffix.
- If missing, infer value from message.

User message: "%s"
`, string(paramsJSON), userMsg)

	raw, err := client.Chat(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("error en LLM: %w", err)
	}

	clean := sanitizeLLMOutput(raw)

	var tmp map[string]interface{}
	if err := json.Unmarshal([]byte(clean), &tmp); err != nil {
		return nil, fmt.Errorf("error parsing parameters JSON: %w; clean=%s", err, clean)
	}

	out := map[string]string{}
	for k, v := range tmp {
		out[k] = fmt.Sprintf("%v", v)
	}

	return out, nil
}

func sanitizeLLMOutput(s string) string {
	s = strings.TrimSpace(s)

	// 1) remover cualquier bloque ```xxx ... ```
	if strings.HasPrefix(s, "```") {
		// quitar primera línea (```json o ```)
		lines := strings.Split(s, "\n")
		if len(lines) > 1 {
			// quitar primera y última
			s = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	// 2) regex para sacar el primer objeto JSON válido
	re := regexp.MustCompile(`\{[\s\S]*\}`)
	match := re.FindString(s)
	if match != "" {
		s = match
	}

	// 3) reemplazar comillas curvas por comillas normales
	s = strings.ReplaceAll(s, "“", "\"")
	s = strings.ReplaceAll(s, "”", "\"")
	s = strings.ReplaceAll(s, "‘", "'")
	s = strings.ReplaceAll(s, "’", "'")

	return strings.TrimSpace(s)
}
