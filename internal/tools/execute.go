package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/logx"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/telemetry"
	"net"
)

var SkipSSRF bool

// internalHosts are tool hostnames allowed to resolve to private/loopback addresses (e.g. a
// mock-tools container on the Docker network). Every other host keeps the SSRF check.
var internalHosts = map[string]bool{}

// SetInternalHosts replaces the hostnames exempted from the internal-IP check. Matching is
// exact and case-insensitive on the URL host (no wildcards, no IP ranges).
func SetInternalHosts(hosts []string) {
	allowed := map[string]bool{}
	for _, h := range hosts {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			allowed[h] = true
		}
	}
	internalHosts = allowed
}

// guardedDial is the SSRF check every outbound tool connection goes through (HTTP, MCP, A2A
// and gRPC alike): a host that resolves to a private, loopback or otherwise internal address
// is refused unless it is listed in internalHosts. It dials the address it checked, so a DNS
// answer cannot change between the check and the connection.
func guardedDial(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	if SkipSSRF {
		return d.DialContext(ctx, network, addr)
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("ssrf prevention: %s does not resolve", host)
	}
	if !internalHosts[strings.ToLower(host)] {
		for _, ip := range ips {
			if ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
				return nil, fmt.Errorf("ssrf prevention: access to internal IP %s is denied", ip.String())
			}
		}
	}
	return d.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
}

var safeClient = &http.Client{
	Timeout: 30 * time.Second,
	// Spans and correlation headers on top; the SSRF-guarded dialer underneath.
	Transport: telemetry.OutboundTransport(&http.Transport{DialContext: guardedDial}),
}

func ExecuteTool(t config.Tool, params map[string]string) (map[string]any, error) {
	return ExecuteToolCtx(context.Background(), t, params)
}

func ExecuteToolCtx(ctx context.Context, t config.Tool, params map[string]string) (map[string]any, error) {
	if t.Type == "local" {
		return executeLocalFileTool(ctx, t, params)
	}
	if t.Type == "mcp" || t.Type == "a2a" || t.Type == "grpc" {
		return ExecuteProtocolTool(ctx, t, params)
	}

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
