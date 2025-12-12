package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/ccastromar/aos-agent-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agent-orchestration-system/internal/logx"
)

type DetectedIntent struct {
	Type string `json:"intent"`
	//RequiredParams []string          `json:"required_params"`
	Params map[string]string // parámetros extraídos (puede empezar vacío)

}

type DetectedIntentAndParams struct {
	Type       string
	Params     map[string]string
	Confidence float64
	Errors     []string
	Raw        string
}

//	type IntentSchema struct {
//		Description string   `json:"description"`
//		Params      []string `json:"params"`
//	}
//
// ------------------------------------------------------------
// sanitizeLLMOutput removes Markdown fences, noise, and extracts
// the first clean JSON object inside the LLM response.
// ------------------------------------------------------------
func sanitizeLLMOutput(raw string) string {
	raw = strings.TrimSpace(raw)

	// Remove Markdown-style ```json ... ```
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSpace(raw)

		if strings.HasSuffix(raw, "```") {
			raw = strings.TrimSuffix(raw, "```")
			raw = strings.TrimSpace(raw)
		}
	}

	// Regex to extract ```json ... ``` blocks anywhere
	fenced := regexp.MustCompile("(?s)```json(.*?)```")
	if m := fenced.FindStringSubmatch(raw); len(m) == 2 {
		raw = strings.TrimSpace(m[1])
	}

	// General fallback: extract the first {...} JSON object
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		raw = raw[start : end+1]
	}

	return strings.TrimSpace(raw)
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

func DetectIntentAndParams(
	ctx context.Context,
	c LLMClient,
	text string,
	intents map[string]config.Intent,
) (*DetectedIntentAndParams, error) {

	// Build JSON of all intents + required params + descriptions
	intentDefs := make(map[string]any)
	for name, def := range intents {
		intentDefs[name] = map[string]any{
			"description":     def.Description,
			"required_params": def.RequiredParams,
		}
	}
	intentsJSON, _ := json.MarshalIndent(intentDefs, "", "  ")

	prompt := fmt.Sprintf(`
Eres el módulo NLU del sistema AOS.

Tarea:
1. Detectar cuál de los INTENTS definidos es el adecuado.
2. Extraer TODOS los parámetros definidos en required_params.
3. Si un parámetro no aparece en el mensaje, devolverlo como "".
4. Usa EXACTAMENTE los nombres de parámetros que aparecen en required_params.
5. NO inventes nombres, NO uses "param1", "param2", etc.

INTENTS DEFINIDOS (NO inventar otros):
%s

Mensaje del usuario:
"%s"

RESPONDE SOLO UN JSON VÁLIDO, SIN TEXTO FUERA.

Ejemplo de formato (NO literal):

{
  "intent": "banking.get_balance",
  "confidence": 0.8,
  "parameters": {
    "accountId": "123456"
  },
  "errors": []
}

REGLAS:
- NO uses "..."
- NO omitas claves con "etc"
- Si no hay parámetros, usa {}.
- Si no hay errores, usa [].

`, string(intentsJSON), text)

	_ = fmt.Sprintf(`
Eres el módulo NLU del sistema AOS (Agent Orchestration System).
Tu tarea es:

1) Detectar cuál INTENT de la lista encaja mejor con el mensaje del usuario.
2) Extraer los parámetros requeridos por ese INTENT.
3) NO inventar datos. Si falta un parámetro requerido, devolverlo como "".

INTENTS DEFINIDOS:
%s

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

Mensaje del usuario:
"%s"

REGLAS DE SALIDA:
- Responde SOLO un JSON válido.
- Nada de texto fuera del JSON.

`, string(intentsJSON), text)

	raw, err := c.Chat(ctx, prompt)
	if err != nil {
		return nil, err
	}

	clean := sanitizeLLMOutput(raw)
	logx.Debug("Planner", "sanitized raw LLM JSON response: %s", clean)
	// Unmarshal genérico
	var tmp any
	if err := json.Unmarshal([]byte(clean), &tmp); err != nil {
		return nil, fmt.Errorf("invalid JSON from LLM: %w", err)
	}

	// Schema validation (AQUÍ ESTÁ LA CLAVE)
	if err := intentSchema.Validate(tmp); err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	// Parse JSON strictly
	var out struct {
		Intent     string            `json:"intent"`
		Confidence float64           `json:"confidence"`
		Params     map[string]string `json:"parameters"`
		Errors     []string          `json:"errors"`
	}

	if err := json.Unmarshal([]byte(clean), &out); err != nil {
		return nil, fmt.Errorf("invalid JSON from LLM: %w | raw=%s", err, raw)
	}

	// Validate intent exists
	// Intent semántic (NO téch error	)
	intentCfg, ok := intents[out.Intent]
	if !ok {
		return &DetectedIntentAndParams{
			Type:   "",
			Params: map[string]string{},
			Errors: []string{"invalid_intent"},
			Raw:    clean,
		}, nil
	}

	// Ensure params exist even if omitted
	if out.Params == nil {
		out.Params = map[string]string{}
	}

	for _, req := range intentCfg.RequiredParams {
		if _, ok := out.Params[req]; !ok {
			out.Params[req] = ""
			out.Errors = append(out.Errors, req)
		}
	}

	return &DetectedIntentAndParams{
		Type:       out.Intent,
		Params:     out.Params,
		Confidence: out.Confidence,
		Errors:     out.Errors,
		Raw:        clean,
	}, nil
}
