package llm

import (
	"context"
	"encoding/json"
	"fmt"
)

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

	//logx.Debug("Analyst", prompt)

	out, err := c.Chat(ctx, prompt)
	if err != nil {
		return "", err
	}

	return out, nil
}

// =======================
// PROMPT COMPOSITION
// =======================

func buildSummarizePrompt(
	intentType string,
	rawResult map[string]any,
	targetLanguage string,
) (string, error) {

	rawJSON, err := json.Marshal(rawResult)
	if err != nil {
		return "", err
	}

	switch targetLanguage {
	case "es":
		return summarizePromptES(intentType, string(rawJSON)), nil
	case "en":
		return summarizePromptEN(intentType, string(rawJSON)), nil
	default:
		return "", fmt.Errorf("unsupported language: %s", targetLanguage)
	}
}

// =======================
// PROMPTS (LANGUAGE-SPECIFIC)
// =======================

func summarizePromptES(intentType string, rawJSON string) string {
	return fmt.Sprintf(`
Eres un asistente multi-dominio (banca, devops, CRM, helpdesk, healthcare).
Has ejecutado un workflow con el siguiente propósito: "%s".

REGLAS IMPORTANTES (ESTRICTAS):
1. Cada paso incluye un campo "executed" con valor true o false.
2. Si el paso principal de la operación tiene "executed": false, la operación NO se completó.
3. En ese caso, DEBES indicar claramente que la operación NO fue realizada y NO debes afirmar éxito.
4. Los pasos secundarios (notificaciones, logs, validaciones) pueden haberse ejecutado aunque la operación principal no lo hiciera.
5. Interpreta el JSON estrictamente como se proporciona. NO inventes hechos ni resultados.

Resultados brutos de la ejecución (JSON):

%s

El resumen debe explicar:
- si la operación principal se completó o no,
- qué ocurrió durante el pipeline,
- si hubo pasos omitidos o condiciones que impidieron continuar,
- cualquier advertencia, riesgo o notificación relevante.

REQUISITOS DE SALIDA:
- Escribe el resumen únicamente en español.
- Usa español profesional, natural y gramaticalmente correcto.
- Usa terminología adecuada al contexto.
- NO uses palabras en inglés.
- NO traduzcas literalmente desde el inglés.
- NO repitas frases innecesariamente.

Devuelve SOLO TEXTO PLANO.
NO devuelvas JSON.
NO uses listas.
`, intentType, rawJSON)
}

func summarizePromptEN(intentType string, rawJSON string) string {
	return fmt.Sprintf(`
You are a multi-domain assistant (banking, devops, CRM, helpdesk, healthcare).
You have executed a workflow with the following intent: "%s".

IMPORTANT RULES (STRICT):
1. Each step includes an "executed" field set to true or false.
2. If the MAIN operation step has "executed": false, the operation was NOT completed.
3. In that case, you MUST clearly state that the operation did NOT occur and MUST NOT claim success.
4. Secondary steps (notifications, logs, validations) may have executed even if the main operation did not.
5. Interpret the JSON strictly as provided. DO NOT invent facts or outcomes.

Raw execution results (JSON):

%s

The summary must explain:
- whether the main operation was completed or not,
- what happened during the workflow,
- whether any steps were skipped or blocked,
- any relevant warnings, risks, or notifications.

OUTPUT REQUIREMENTS:
- Write the summary entirely in English.
- Use clear, professional, native English.
- Do NOT use any other language.
- Do NOT repeat phrases unnecessarily.

Return PLAIN TEXT ONLY.
Do NOT return JSON.
Do NOT use lists.
`, intentType, rawJSON)
}
