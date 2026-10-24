# Protocol interop: AOS against real MCP and A2A servers

AOS's own tests use small fakes. This run checks its MCP and A2A steps against servers built
with the **official SDKs**, which is where a wrong field or a missing handshake shows up. gRPC
needs no external server: `internal/tools/grpc_test.go` already runs real grpc-go servers
(the standard Health service, gRPC's interop TestService and a `Struct` service) in-process.

| Server | Built with | What it exercises |
|---|---|---|
| `mcp_server.py` (:18110, `/mcp`) | MCP Python SDK 2.x, Streamable HTTP | `initialize` and session (`Mcp-Session-Id`), SSE responses, `tools/list` schemas (an `integer` argument), a tool that fails (`isError`), an unknown tool |
| `a2a_server.py` (:18120, `/a2a`) | A2A Python SDK 1.x, JSON-RPC binding, 0.3 and 1.0 on one endpoint | `message/send` / `SendMessage` non-blocking, polling `tasks/get` / `GetTask` until `completed`, a task that stops in `input-required` |

## Run

```bash
python3 -m venv .venv-interop && .venv-interop/bin/pip install -r test/interop/requirements.txt
.venv-interop/bin/python test/interop/mcp_server.py &
.venv-interop/bin/python test/interop/a2a_server.py &
go test -tags interop ./test/interop/ -v
```

Other addresses: `INTEROP_MCP_URL`, `INTEROP_A2A_URL`. The build tag keeps this out of
`go test ./...` and CI, which have no Python servers.

Against the request shapes AOS sent before this work, both servers refused the first call:
MCP with `Missing session ID` (no `initialize`), A2A with `messageId: Field required`.

## The whole pipeline

`protocols.customer_review` runs `pipeline_protocols_demo` (MCP → gRPC → A2A) against the
local mocks, with the SSRF check on:

```bash
AOS_MOCK_PORT=19000 AOS_MOCK_GRPC_PORT=19002 go run ./cmd/mocks &
MOCK_OLLAMA_PORT=11435 go run ./cmd/mock-ollama &   # the final summary needs a model
AOS_STATE_STORE=memory ALLOW_LOCAL_TOOLS=false AOS_TOOL_INTERNAL_HOSTS=127.0.0.1 \
  AOS_MOCK_BASE_URL=http://127.0.0.1:19000 AOS_MOCK_GRPC_TARGET=127.0.0.1:19002 \
  LLM_ENGINE=ollama OLLAMA_BASE_URL=http://127.0.0.1:11435 \
  LLM_MODEL=aos-demo:deterministic OLLAMA_INTENT_MODEL=aos-demo:deterministic \
  go run ./cmd/aos -port 18080 &
curl -s -X POST http://127.0.0.1:18080/ask_structured -H 'Content-Type: application/json' \
  -H 'X-Mock-Role: admin' -d '{"operation":"protocols.customer_review","params":{"customerId":"CUST-42"}}'
```

All three steps answer for `CUST-42`: the CRM lookup over MCP, a risk score from the gRPC mock,
and an approved compliance review from the A2A agent.
