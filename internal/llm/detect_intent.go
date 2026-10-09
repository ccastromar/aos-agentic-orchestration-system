package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/logx"
)

type DetectedIntent struct {
	Type string `json:"intent"`
	//RequiredParams []string          `json:"required_params"`
	Params map[string]string
}

type DetectedIntentAndParams struct {
	Type       string
	Params     map[string]string
	Confidence float64
	Language   string
	Errors     []string
	NewFacts   []string
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

	// General fallback: extract the first {...} JSON object properly using bracket matching
	start := strings.Index(raw, "{")
	if start >= 0 {
		depth := 0
		for i := start; i < len(raw); i++ {
			if raw[i] == '{' {
				depth++
			} else if raw[i] == '}' {
				depth--
				if depth == 0 {
					raw = raw[start : i+1]
					break
				}
			}
		}
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
You are an intent classifier for a multi-domain (banking, devops, CRM, Helpdesk) Augmented Orchestration System (AOS).

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

	raw, err := chatForIntent(ctx, c, prompt)
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
	sessionContext string,
) (*DetectedIntentAndParams, error) {

	// Build JSON of all intents + required params + descriptions + examples
	intentDefs := make(map[string]any)
	for name, def := range intents {
		// Fallback is selected by the planner only after no concrete intent matches.
		if name == "" || name == "system.fallback" {
			continue
		}
		entry := map[string]any{
			"description":     def.Description,
			"required_params": def.RequiredParams,
		}
		if len(def.Examples) > 0 {
			entry["examples"] = def.Examples
		}
		intentDefs[name] = entry
	}
	intentsJSON, _ := json.MarshalIndent(intentDefs, "", "  ")

	prompt := fmt.Sprintf(`
You are the NLU module of the AOS system.

Your task:
1. Detect which of the DEFINED INTENTS best matches the user message.
2. Extract ALL parameters defined in required_params for that intent.
3. If a parameter does not appear in the message, return it as an empty string "".
4. Use EXACTLY the parameter names as defined in required_params.
5. DO NOT invent parameter names. DO NOT use placeholders like "param1", "param2", etc.
6. Select a concrete action before considering "unknown". Examples in the catalog are positive matches, not text to ignore.
7. An explicit request to remember a fact is a memory-writing action, not a general question. If memory.add is defined, select it and copy the fact into "fact". Do not substitute new_facts for this action.
8. For example, "Remember that my dog's name is Rex" means memory.add with fact "My dog's name is Rex", language "en", errors []. Only use this intent when it is present in the catalog.
9. Missing parameters require the matching intent plus errors for the missing fields, not "unknown".

DEFINED INTENTS (DO NOT invent new ones. If no intent matches, return "unknown"):
%s

RECENT CONVERSATION CONTEXT:
Use this to resolve ambiguous or missing parameters if the user refers to previous context (e.g. "and now transfer 20 to that account").
%s

The user message below may be in any language.

LANGUAGE DETECTION:
- Detect the language of the USER MESSAGE ONLY.
- Do NOT infer the language from this system prompt.
- Return the ISO-639-1 code.
- If uncertain, return "und".

MEMORY EXTRACTION:
- Facts MUST be copied verbatim from USER MESSAGE, never from examples or conversation context.
- Do not paraphrase, translate, change pronouns, or add facts.
- For memory.add, "fact" MUST be an exact non-empty excerpt of USER MESSAGE.
- For memory.* actions, return new_facts: []; the memory tool handles the operation.
- new_facts is only for durable information about the user's identity, preferences, or circumstances that they state about themselves.
- Never save task requests, instructions, project requirements, code changes, test requests, or facts about artifacts being worked on as new_facts.
- If no durable personal fact is explicitly stated, leave "new_facts" empty: [].

WORKSPACE INTENT ROUTING:
- workspace.agent_code is for changing an existing project: adding endpoints/features, editing project files, adding tests, running project tests, or fixing project code. A language or file path is not required; the agent must inspect the project to discover them.
- workspace.generate_code is only for generating one standalone source file when the user explicitly names BOTH a programming language and a destination source-file path in the message.
- A URL/API route such as /api/tasks/count is not a source-file path. Never invent a language or file path.
- If the user asks for a project change and the model selected workspace.generate_code, select workspace.agent_code and set its request parameter to the complete USER MESSAGE.

USER MESSAGE:
"%s"

RESPOND WITH A SINGLE VALID JSON OBJECT ONLY. DO NOT include any extra text.
The fields "intent", "confidence", "language", "parameters", "errors", and "new_facts"
MUST ALWAYS be present in the output JSON, even if empty.

Example format (NOT literal):

{
  "intent": "banking.get_balance",
  "confidence": 0.8,
  "language": "en",
  "parameters": {
    "accountId": "123456"
  },
  "errors": [],
  "new_facts": []
}

RULES:
- DO NOT use "..."
- DO NOT omit keys or use placeholders like "etc"
- If there are no parameters, return {}.
- If there are no errors, return [].
- If there are no new facts, return [].
- "new_facts": only exact excerpts of personal facts stated in USER MESSAGE. Otherwise [].

`, string(intentsJSON), sessionContext, text)

	raw, err := chatForIntent(ctx, c, prompt)
	if err != nil {
		return nil, err
	}

	clean := sanitizeLLMOutput(raw)
	logx.Debug("Planner", "sanitized raw LLM JSON response: %s", clean)
	// Unmarshal generic
	var tmp any
	if err := json.Unmarshal([]byte(clean), &tmp); err != nil {
		return nil, fmt.Errorf("invalid JSON from LLM: %w", err)
	}

	// Explicit memory tools own persistence. Ignore the unused auxiliary extraction
	// field even when a model returns objects there instead of strings.
	if obj, ok := tmp.(map[string]any); ok {
		if intent, _ := obj["intent"].(string); strings.HasPrefix(intent, "memory.") {
			obj["new_facts"] = []any{}
			normalized, err := json.Marshal(obj)
			if err != nil {
				return nil, err
			}
			clean = string(normalized)
		}
	}
	// Schema validation
	if err := intentSchema.Validate(tmp); err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	// Parse JSON strictly
	var out struct {
		Intent     string            `json:"intent"`
		Confidence float64           `json:"confidence"`
		Language   string            `json:"language"`
		Params     map[string]string `json:"parameters"`
		Errors     []string          `json:"errors"`
		NewFacts   []string          `json:"new_facts"`
	}

	if err := json.Unmarshal([]byte(clean), &out); err != nil {
		return nil, fmt.Errorf("invalid JSON from LLM: %w | raw=%s", err, raw)
	}
	if out.Intent == "workspace.generate_code" && isExistingProjectChange(text) {
		if _, exists := intents["workspace.agent_code"]; exists {
			out.Intent = "workspace.agent_code"
			out.Params = map[string]string{"request": text}
			out.Errors = nil
			out.NewFacts = nil
		}
	}

	// Model-generated facts are untrusted: only retain text grounded in this message.
	grounded := []string{}
	for _, fact := range out.NewFacts {
		if factFromMessage(text, fact) {
			grounded = append(grounded, fact)
		}
	}
	out.NewFacts = grounded
	if out.Intent == "memory.add" && !factFromMessage(text, out.Params["fact"]) {
		if out.Params == nil {
			out.Params = map[string]string{}
		}
		out.Params["fact"] = ""
	}
	// Validate intent exists. The empty string is never a valid intent even
	// if a malformed definition registered one under "".
	intentCfg, ok := intents[out.Intent]
	if !ok || out.Intent == "" {
		return &DetectedIntentAndParams{
			Type:     "",
			Params:   map[string]string{},
			Errors:   []string{"invalid_intent"},
			NewFacts: out.NewFacts,
			Raw:      clean,
		}, nil
	}

	// Ensure params exist even if omitted
	if out.Params == nil {
		out.Params = map[string]string{}
	}

	for _, req := range intentCfg.RequiredParams {
		if val, ok := out.Params[req]; !ok || val == "" {
			out.Params[req] = ""
			out.Errors = append(out.Errors, req)
		}
	}

	return &DetectedIntentAndParams{
		Type:       out.Intent,
		Params:     out.Params,
		Confidence: out.Confidence,
		Language:   out.Language,
		Errors:     out.Errors,
		NewFacts:   out.NewFacts,
		Raw:        clean,
	}, nil
}

func chatForIntent(ctx context.Context, client LLMClient, prompt string) (string, error) {
	if direct, ok := client.(interface {
		ChatForIntent(context.Context, string, int) (string, error)
	}); ok {
		return direct.ChatForIntent(ctx, prompt, 512)
	}
	return client.Chat(ctx, prompt)
}

// A conservative source check prevents examples and model inventions from being persisted.
// Paraphrases must be clarified instead of silently accepted as user facts.
func factFromMessage(message, fact string) bool {
	normalize := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	f := normalize(fact)
	return f != "" && strings.Contains(normalize(message), f)
}

func isExistingProjectChange(message string) bool {
	text := strings.ToLower(message)
	projectContext := []string{"proyecto", "project", "código existente", "existing code", "en el repo", "in the repo", "en este repo", "in this repo", "controller", "controlador", "endpoint", "api/tasks", "test de", "tests del", "project tests"}
	changeRequest := []string{"añade", "agrega", "crea", "modifica", "actualiza", "corrige", "implementa", "add ", "create ", "modify ", "update ", "fix ", "implement "}
	hasContext, hasChange := false, false
	for _, marker := range projectContext {
		if strings.Contains(text, marker) {
			hasContext = true
			break
		}
	}
	for _, marker := range changeRequest {
		if strings.Contains(text, marker) {
			hasChange = true
			break
		}
	}
	return hasContext && hasChange
}
