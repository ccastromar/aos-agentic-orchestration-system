package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ccastromar/aos-agent-orchestration-system/internal/logx"
)

type DetectedIntent struct {
	Type string `json:"intent"`
	//RequiredParams []string          `json:"required_params"`
	Params map[string]string // parámetros extraídos (puede empezar vacío)

}

type IntentSchema struct {
	Description string   `json:"description"`
	Params      []string `json:"params"`
}

func DetectIntent(ctx context.Context, c LLMClient, text string, validIntents map[string]any) (*DetectedIntent, error) {
	// Build intent list for the prompt
	keys := make([]string, 0, len(validIntents))
	for k := range validIntents {
		keys = append(keys, k)
	}
	intentsJSON, _ := json.Marshal(keys)

	//TODO first prompt for just intent detection
	prompt := fmt.Sprintf(`
You are an intent classifier for a multi-domain (banking, devops, CRM, Helpdesk) Agent Orchestration System (AOS).

Valid intents (choose exactly one, output must be EXACTLY the key):

%s

Rules:
- Output ONLY the intent key (like devops.get_service_status).
- Do NOT explain or add text.
- Do NOT create new intents.
- Prefer devops.* for infrastructure messages.
- Prefer banking.* for financial messages.
- Prefer crm.* for customer relationship messages.
- Prefer helpdesk.* for support-related messages.

User message:
"%s"
`, intentsJSON, text)

	//TODO second prompt for intent detection and params extraction from LLM
	_ = fmt.Sprintf(`You are the NLU module for the Agent Orchestration System (AOS).

Your task: from the user's message, detect the correct intent and extract all relevant structured parameters.

Valid intents (choose exactly one):
%s

Output rules:
1. You MUST output ONLY a JSON object.
2. No explanations, no markdown, no text outside the JSON.
3. The JSON MUST match exactly this schema:

{
  "intent": "string",
  "confidence": float,
  "parameters": { ... },
  "errors": [ "string" ]
}

Definitions:
- "intent": one of the valid intent keys above.
- "confidence": number between 0 and 1.
- "parameters": dictionary with extracted fields. Omit fields you cannot derive reliably.
- "errors": list of missing or ambiguous fields that the user may need to clarify. Use [] if none.

Extraction policies:
- For banking.transfer extract:
  amount (number), currency (string), toAccount (string), concept (string).
- Convert currencies to ISO codes when possible (e.g., "euros" -> "EUR").
- Do NOT guess fields. If unsure, omit and list the field name under "errors".

User message:
"%s"
`, intentsJSON, text)

	raw, err := c.Chat(ctx, prompt)
	if err != nil {
		return nil, err
	}
	logx.Debug("Planner", "raw response from LLM %s", raw)
	clean := strings.TrimSpace(raw)
	//logx.Debug("Planner", "clean is %s", clean)
	//logx.Debug("Planner", "validIntents %w", validIntents)
	// Validate
	if _, ok := validIntents[clean]; !ok {
		return nil, fmt.Errorf("DetectIntent invalid JSON : unknown intent; raw=%s", clean)
	}

	return &DetectedIntent{
		Type:   clean,
		Params: map[string]string{},
	}, nil
}

// DetectIntent recibe el mensaje usuario + todos los intents del YAML
func DetectIntentOld(ctx context.Context, client LLMClient, userMsg string, intents map[string]IntentSchema) (*DetectedIntent, error) {

	// preparar JSON para el prompt (el LLM verá todos los intents disponibles)
	intentsJSON, _ := json.Marshal(intents)

	prompt := fmt.Sprintf(`
You are an intent classifier for a multi-domain Agent Orchestration System (AOS).

Here is the full list of valid intents you MUST choose from (and ONLY these):

%s

Your job:
1. Read the user message.
2. Select EXACTLY one intent.
3. Answer ONLY with the intent key (e.g. "devops.get_service_status").

Rules:
- Do not invent intents.
- Prefer devops.* for infrastructure/service questions.
- Prefer banking.* ONLY when explicitly financial.
- Do NOT rewrite or explain.
- Output must be one of the valid intent keys.

User message:
"%s"
`, intentsJSON, userMsg)

	raw, err := client.Chat(ctx, prompt)
	if err != nil {
		return nil, err
	}

	raw = strings.TrimSpace(raw)

	var out DetectedIntent
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("DetectIntent JSON inválido: %w; raw=%s", err, raw)
	}

	// sanity check
	if out.Type == "" {
		return nil, fmt.Errorf("DetectIntent: intent vacío; raw=%s", raw)
	}

	return &out, nil
}
