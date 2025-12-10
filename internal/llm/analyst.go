package llm

import (
	"context"
	"encoding/json"
	"fmt"
)

func SummarizeResult(ctx context.Context, c LLMClient, intentType string, rawResult map[string]any) (string, error) {
	rawJSON, _ := json.Marshal(rawResult)

	prompt := fmt.Sprintf(`
Eres un asistente multidominio (banking, devops, CRM, helpdesk, salud) experto.
Has ejecutado un flujo con intent: "%s".

Muy importante:
1. Cada paso incluye un campo "executed" true/false.
2. Si "executed": false en el paso principal de la operación (por ejemplo, transferencia, bizum, pago, despliegue, etc.),
   entonces **la operación NO se ha realizado**.
3. En ese caso debes explicarlo claramente y NO debes afirmar que la operación se realizó con éxito.
4. Si existen pasos secundarios como notificaciones, logs o validaciones, pueden haberse ejecutado aunque la operación principal no lo haya hecho.
5. Interpreta el JSON tal cual, sin inventar comportamientos.

Aquí tienes los resultados en bruto (JSON):

%s

Escribe un resumen corto en español para el usuario final, explicando:
- si la operación principal se realizó o no,
- qué ha ocurrido en el flujo,
- si hubo pasos omitidos o condiciones que impidieron continuar,
- cualquier detalle relevante (riesgo, avisos, notificaciones).

Devuelve SOLO texto plano, sin JSON, sin listas.
`, intentType, string(rawJSON))

	out, err := c.Chat(ctx, prompt)
	if err != nil {
		return "", err
	}
	return out, nil
}
