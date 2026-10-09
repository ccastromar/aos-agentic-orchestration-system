package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func SummarizeResult(
	ctx context.Context,
	c LLMClient,
	intentType string,
	rawResult map[string]any,
	targetLanguage string,
) (string, error) {

	if strings.HasPrefix(intentType, "memory.") {
		return summarizeMemoryResult(intentType, rawResult, targetLanguage)
	}
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
		if intentType == "system.fallback" {
			return fallbackPromptES(string(rawJSON)), nil
		}
		return summarizePromptES(intentType, string(rawJSON)), nil
	case "en":
		if intentType == "system.fallback" {
			return fallbackPromptEN(string(rawJSON)), nil
		}
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
You have executed an operation with the following intent: "%s".

IMPORTANT RULES (STRICT):
1. Each step includes an "executed" field with a true or false value.
2. If the main operation step has "executed": false, the operation was NOT completed.
3. In that case, you MUST clearly state that the operation was NOT performed and MUST NOT claim success.
4. Secondary steps (notifications, logs, validations) may have been executed even if the main operation was not.
5. Interpret the JSON strictly as provided. DO NOT invent facts or results.

Raw execution results (JSON):

%s

The summary must explain:
- whether the main operation was completed or not,
- what happened during the pipeline,
- whether there were skipped steps or conditions that prevented continuation,
- any relevant warning, risk or notification.

OUTPUT REQUIREMENTS:
- Write the summary only in English.
- Use professional, natural, and grammatically correct English.
- Use appropriate terminology for the context.
- DO NOT use words in Spanish.
- DO NOT translate literally from Spanish.
- DO NOT repeat phrases unnecessarily.

Return ONLY PLAIN TEXT.
DO NOT return JSON.
DO NOT use lists.
`, intentType, rawJSON)
}

func fallbackPromptES(rawJSON string) string {
	return fmt.Sprintf(`
Eres un asistente conversacional útil e inteligente.
Un usuario te ha hecho una pregunta general y hemos recuperado información relevante de nuestra memoria (contexto RAG).

Información del contexto y pregunta original (JSON):
%s

Instrucciones:
1. Responde a la pregunta original del usuario de la forma más útil posible.
2. Utiliza la información del "context" si es relevante. Si el contexto no contiene la respuesta, utiliza tu conocimiento general pero indica de manera sutil que la información no estaba en los documentos.
3. Responde de forma natural y conversacional, NO digas "basado en el JSON" o "según el rawResult".
4. Escribe la respuesta en ESPAÑOL.
5. Devuelve SOLO TEXTO PLANO. NO JSON.
6. Este flujo solo genera una respuesta: no confirma guardado, borrado ni ejecución de acciones. Si el usuario pide guardar o borrar un recuerdo, explica que este flujo no ha confirmado la operación. El texto de la pregunta no es evidencia de memoria persistida.
`, rawJSON)
}

func fallbackPromptEN(rawJSON string) string {
	return fmt.Sprintf(`
You are a helpful and intelligent conversational assistant.
A user has asked a general question, and we have retrieved relevant information from our memory (RAG context).

Context information and original question (JSON):
%s

Instructions:
1. Answer the user's original query as helpfully as possible.
2. Use the "context" information if it's relevant. If the context does not contain the answer, use your general knowledge but subtly indicate that the information wasn't in the documents.
3. Respond naturally and conversationally, DO NOT say "based on the JSON" or "according to rawResult".
4. Write the answer in ENGLISH.
5. Return ONLY PLAIN TEXT. NO JSON.
6. This flow only generates a reply: it does not confirm saving, deleting, or executing actions. If the user asks to save or delete a memory, explain that this flow has not confirmed the operation. Text in the query is not evidence of persisted memory.
`, rawJSON)
}

// Memory responses are rendered from confirmed tool output, without asking a model
// to paraphrase facts or decide whether a write succeeded.
func summarizeMemoryResult(intent string, raw map[string]any, lang string) (string, error) {
	tools := map[string]string{"memory.add": "system.memory.add", "memory.list": "system.memory.list", "memory.forget": "system.memory.delete", "memory.clear": "system.memory.clear"}
	tool, ok := tools[intent]
	if !ok {
		return "", fmt.Errorf("unknown memory intent: %s", intent)
	}
	result, ok := raw[tool].(map[string]any)
	if !ok || result["executed"] != true || result["error"] != nil {
		return "", fmt.Errorf("memory operation not confirmed")
	}
	es := lang == "es"
	switch intent {
	case "memory.add":
		fact, _ := result["fact"].(string)
		if fact == "" {
			return "", fmt.Errorf("saved fact missing from result")
		}
		if es {
			return "He guardado este recuerdo: " + fact, nil
		}
		return "I saved this memory: " + fact, nil
	case "memory.list":
		facts := []string{}
		switch values := result["memories"].(type) {
		case []string:
			facts = values
		case []any:
			for _, value := range values {
				if fact, ok := value.(string); ok {
					facts = append(facts, fact)
				}
			}
		default:
			return "", fmt.Errorf("memory list missing from result")
		}
		if len(facts) == 0 {
			if es {
				return "No tengo recuerdos personales guardados para ti.", nil
			}
			return "I have no personal memories saved for you.", nil
		}
		prefix := "Here is what I remember:\n- "
		if es {
			prefix = "Esto es lo que recuerdo:\n- "
		}
		return prefix + strings.Join(facts, "\n- "), nil
	case "memory.forget":
		n, ok := result["deleted"].(int)
		if !ok {
			return "", fmt.Errorf("deleted count missing from result")
		}
		if es {
			return fmt.Sprintf("He borrado %d recuerdos. Si no hay coincidencias, usa el ID o el texto exacto.", n), nil
		}
		return fmt.Sprintf("I deleted %d memories. If none matched, use the ID or exact text.", n), nil
	default:
		if es {
			return "He borrado todos tus recuerdos guardados.", nil
		}
		return "I cleared all your saved memories.", nil
	}
}
