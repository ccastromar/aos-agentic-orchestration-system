//go:build interop

// Interop run: AOS's protocol steps against real servers built with the official SDKs
// (mcp_server.py, a2a_server.py; see README.md). Not part of the default test run:
//
//	go test -tags interop ./test/interop/ -v
package interop

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/config"
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/tools"
)

func init() {
	tools.SkipSSRF = true // the servers run on loopback
}

func url(env, fallback string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return fallback
}

var (
	mcpURL = url("INTEROP_MCP_URL", "http://127.0.0.1:18110/mcp")
	a2aURL = url("INTEROP_A2A_URL", "http://127.0.0.1:18120/a2a")
)

func run(t *testing.T, tool config.Tool, params map[string]string) (map[string]any, error) {
	t.Helper()
	tool.TimeoutMs = 15000
	return tools.ExecuteProtocolTool(context.Background(), tool, params)
}

func TestMCPCallWithTypedArgumentsAndRemoteName(t *testing.T) {
	out, err := run(t, config.Tool{Type: "mcp", Name: "crm.lookup", RemoteTool: "lookup_customer", Target: mcpURL},
		map[string]string{"customer_id": "C-7", "limit": "3"})
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	result := out["result"].(map[string]any)
	if !strings.Contains(stringify(result), "C-7") {
		t.Fatalf("unexpected result: %v", result)
	}
}

func TestMCPToolFailureFailsTheStep(t *testing.T) {
	// The SDK hides the exception's text; the step must fail with what the server said.
	_, err := run(t, config.Tool{Type: "mcp", Name: "broken", Target: mcpURL}, nil)
	if err == nil || !strings.Contains(err.Error(), "MCP tool broken failed") || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("want the tool's failure, got %v", err)
	}
}

func TestMCPUnknownToolFailsTheStep(t *testing.T) {
	if _, err := run(t, config.Tool{Type: "mcp", Name: "does_not_exist", Target: mcpURL}, nil); err == nil {
		t.Fatal("an unknown tool must fail the step")
	}
}

func TestA2A(t *testing.T) {
	for _, version := range []string{"0.3", "1.0"} {
		t.Run(version+"/completes", func(t *testing.T) {
			out, err := run(t, config.Tool{Type: "a2a", Target: a2aURL, Version: version}, map[string]string{"message": "review order ORD-7"})
			if err != nil {
				t.Fatalf("call failed: %v", err)
			}
			if !strings.Contains(stringify(out["result"]), "Compliance review approved") {
				t.Fatalf("no artifact in %v", out["result"])
			}
		})
		t.Run(version+"/input-required-fails", func(t *testing.T) {
			_, err := run(t, config.Tool{Type: "a2a", Target: a2aURL, Version: version}, map[string]string{"message": "this needs input"})
			if err == nil || !strings.Contains(err.Error(), "needs input") {
				t.Fatalf("want an input-required error, got %v", err)
			}
		})
	}
}

func stringify(v any) string {
	var b strings.Builder
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for _, e := range x {
				walk(e)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		case string:
			b.WriteString(x + " ")
		}
	}
	walk(v)
	return b.String()
}
