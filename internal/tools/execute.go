package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/logx"
	"net"
)

var SkipSSRF bool

var safeClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if SkipSSRF {
				return net.Dial(network, addr)
			}
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.LookupIP(host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
					return nil, fmt.Errorf("ssrf prevention: access to internal IP %s is denied", ip.String())
				}
			}
			return net.Dial(network, net.JoinHostPort(ips[0].String(), port))
		},
	},
}

func ExecuteTool(t config.Tool, params map[string]string) (map[string]any, error) {
	return ExecuteToolCtx(context.Background(), t, params)
}

func ExecuteToolCtx(ctx context.Context, t config.Tool, params map[string]string) (map[string]any, error) {

	// render URL
	finalURL, err := RenderTemplateString(t.URL, params)
	if err != nil {
		return nil, fmt.Errorf("error rendering URL: %w", err)
	}

	// render body
	bodyParams := map[string]string{}
	for k, v := range t.Body {
		rendered, err := RenderTemplateString(v, params)
		if err != nil {
			return nil, fmt.Errorf("error rendering body: %w", err)
		}
		bodyParams[k] = rendered
	}

	// render headers (optional)
	renderedHeaders, err := RenderTemplateMap(t.Headers, params)
	if err != nil {
		return nil, fmt.Errorf("error rendering headers: %w", err)
	}

	// serialize body
	var payload []byte
	if len(bodyParams) > 0 {
		payload, err = json.Marshal(bodyParams)
		if err != nil {
			return nil, fmt.Errorf("error serializing body JSON: %w", err)
		}
	} else {
		payload = []byte("{}")
	}

	logx.Debug("Execute", "Tool's final URL=%s", finalURL)

	// create request
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, t.Method, finalURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	if _, ok := renderedHeaders["Content-Type"]; !ok {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range renderedHeaders {
		req.Header.Set(k, v)
	}

	// effective timeout
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
	client := &http.Client{
		Transport: safeClient.Transport,
		Timeout:   effTimeout,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error executing HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response: %w", err)
	}

	out := map[string]any{
		"ok":         resp.StatusCode >= 200 && resp.StatusCode < 300,
		"statusCode": resp.StatusCode,
	}

	if len(respBody) > 0 {
		var body map[string]any
		if err := json.Unmarshal(respBody, &body); err == nil {
			for k, v := range body {
				out[k] = v
			}
		} else {
			out["raw"] = string(respBody)
		}
	}
	// if HTTP error status, return an error including the status code
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	// success 2xx
	return out, nil
}
