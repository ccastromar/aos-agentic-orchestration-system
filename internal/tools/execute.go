package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/ccastromar/aos-agent-orchestration-system/internal/config"
)

func ExecuteTool(t config.Tool, params map[string]string) (map[string]any, error) {
	return ExecuteToolCtx(context.Background(), t, params)
}

func ExecuteToolCtx(ctx context.Context, t config.Tool, params map[string]string) (map[string]any, error) {

	// render URL
	finalURL, err := RenderTemplateString(t.URL, params)
	if err != nil {
		return nil, fmt.Errorf("error renderizando URL: %w", err)
	}

	// render body
	bodyParams := map[string]string{}
	for k, v := range t.Body {
		rendered, err := RenderTemplateString(v, params)
		if err != nil {
			return nil, fmt.Errorf("error renderizando body: %w", err)
		}
		bodyParams[k] = rendered
	}

	// render headers (optional)
	renderedHeaders, err := RenderTemplateMap(t.Headers, params)
	if err != nil {
		return nil, fmt.Errorf("error renderizando headers: %w", err)
	}

	// serialize body
	var payload []byte
	if len(bodyParams) > 0 {
		payload, err = json.Marshal(bodyParams)
		if err != nil {
			return nil, fmt.Errorf("error serializando body JSON: %w", err)
		}
	} else {
		payload = []byte("{}")
	}

	log.Printf("[Execute][DEBUG] finalURL=%s", finalURL)

	// create request
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, t.Method, finalURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("error creando request: %w", err)
	}

	if _, ok := renderedHeaders["Content-Type"]; !ok {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range renderedHeaders {
		req.Header.Set(k, v)
	}

	// send request
	// compute effective timeout: min(ctx deadline leftover, tool timeout)
	effTimeout := time.Duration(t.TimeoutMs) * time.Millisecond
	if deadline, ok := ctx.Deadline(); ok {
		rem := time.Until(deadline)
		if rem > 0 && (effTimeout == 0 || rem < effTimeout) {
			effTimeout = rem
		}
	}
	if effTimeout <= 0 {
		effTimeout = 30 * time.Second
	}
	client := &http.Client{Timeout: effTimeout}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error ejecutando HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// get response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error leyendo respuesta: %w", err)
	}

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("[HTTP %d] %s", resp.StatusCode, string(respBody))
	}

	// parse JSON
	out := map[string]any{}
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &out); err != nil {
			return nil, fmt.Errorf("error parseando JSON respuesta: %w", err)
		}
	}

	return out, nil
}
