# AOS — Agentic Orchestration System

> **Project status: learning proof-of-concept.**
> AOS is a personal PoC built to explore how to orchestrate LLM-assisted, deterministic
> workflows in Go. The core runtime (bus, agents, YAML-driven pipelines, tool execution,
> human gates) works and is covered by tests (`go test ./...` passes, including `-race`).
> It is **not** production-ready and is not actively maintained as a product.
>
> **Implemented security features:**
> - **JWT authentication** (`internal/auth/jwt.go`) verifies token signatures when `JWT_SECRET` is set.
> - **RBAC** (`internal/rbac`) and the **audit log** (`internal/audit`) are wired into the request path and enforce role-based access control based on `permissions.yaml`.

## Introduction

This project is a **runtime orchestration framework for intent-driven pipelines**, where orchestration is defined as code and execution is strictly constrained by declarative specifications.

The orchestration layer is defined in YAML. A pipeline can call ordinary tools or delegate a bounded step to an external agent runtime; that agent owns its internal reasoning/tool loop, while AOS owns routing, step ordering, timeouts, events, and the final workflow result.

At the core of the system lies an **intent-based runtime**. A single Go binary exposes an API that receives natural-language input, extracts an intent using an NLM/LLM component, and then **resolves that intent against a predefined set of YAML specifications**. If an intent or execution path is not declared in YAML, it simply cannot run. There is no dynamic improvisation at runtime—only controlled execution.

### Orchestration as Code

The framework uses YAML to describe:

* **Pipelines**, each representing domain-specific orchestration (e.g., banking, shipping, CRM).
* **Steps**, which invoke tools, wait for callbacks, request approval, or delegate to an external agent.
* **Tools**, which in practice are API calls (internal or external).
* **Agents**, named runtime adapters that pipelines can invoke with a task and execution limits.
* **Intents**, mapping natural-language requests to allowed pipelines.

A pipeline might represent, for example, a banking transfer flow. Pipelines can run in two modes:
- **Linear**: (default) Step 1, then Step 2, then Step 3.
- **DAG** (Directed Acyclic Graph): Steps execute concurrently based on declared `depends_on` relationships.

Each step is explicit, auditable, and deterministic. It may also include `analyst: true` to trigger an LLM-based cognitive evaluation of the step's results.

### What This Is (and Is Not)

* This is **not** Kubernetes for agents.
* This is **not** a conversational multi-agent framework.
* This is **not** an autonomous system that invents behavior at runtime.

It **is**:

* Orchestration-as-code for AI-assisted systems.
* A deterministic runtime where **intents select pipelines**, not logic.
* A lightweight execution framework focused on **control, auditability, and domain isolation**.

If Kubernetes orchestrates containers, this framework orchestrates **decisions**.

And yes—every pipeline step is an API call in the end. We just make that explicit instead of pretending otherwise.

## Architecture

Read ARCHITECTURE.txt for an overview and a Mermaid diagram of the main components. Key packages:
- internal/agent: APIAgent (HTTP routes), Inspector, Planner, Verifier, Analyst
- internal/tools: template renderer and HTTP tool executor
- internal/config: loads YAML specs from definitions/
- internal/llm: LLM client(s) and helpers
- internal/app: HTTP server, wiring, and runtime state

The app boots agents and an HTTP server, then routes user requests through the bus: APIAgent → Inspector → Planner → Verifier → Analyst. The Planner reads YAML definitions to select the right pipeline and tools. It also handles **Resiliency**: if an execution gets stuck, it can request human clarification (`ClarificationModal`) or find fallback tools. The Verifier executes tools with guardrails, either linearly or as a concurrent DAG. The Analyst uses the LLM to generate human summaries.

The system also includes a **React-based Web UI** (Vite + React Flow) for real-time monitoring. It provides a chat interface, a chronological event timeline, a dynamic **Settings pane** (to swap LLM backends in real-time), and a **DAG Visualizer** to inspect the structure and live state of pipeline nodes.

See also:
- docs/architecture.mmd (source diagram)
- ARCHITECTURE.txt (ASCII and Mermaid with rendering instructions)

## Prerequisites

- Go 1.24+ (see go.mod)
- Optional: Redis (for persistent state). Otherwise, in-memory state is used.
- One LLM backend:
  - Ollama running locally (recommended for local dev), or
  - Google Gemini API credentials (if selecting Gemini engine)

## Configuration (YAML specs)

By default, the app loads definitions from the definitions/ directory:
- definitions/tools/*.yaml — tool templates (HTTP calls, headers, bodies) supporting Go-style templating
- definitions/agents/*.yaml — external agent runtime adapters available to pipeline steps
- definitions/pipelines/*.yaml — ordered steps that call tools
- definitions/intents/*.yaml — intents and slot extraction used by the Planner

You can add your own domain by adding new YAML files in those folders. Example domains are already provided (banking, crm, helpdesk, logistics, human-resources, etc.).

## Environment variables

Important environment variables and defaults (see internal/config/env.go):
- APP_ENV: dev
- PORT: 8080 (overrides HTTP port; CLI flag -port takes precedence)
- READ_TIMEOUT: 5s
- WRITE_TIMEOUT: 5s
- BUS_WORKERS: 4
- BUS_BUFFER: 100

LLM configuration:
- LLM_ENGINE: choose between "ollama" or "gemini" (note: default value in code is a placeholder; set this explicitly)
- LLM_MODEL: model name (e.g., qwen3:0.6b for Ollama, gemini-1.5-flash for Gemini)
- OLLAMA_BASE_URL: http://localhost:11434 (when LLM_ENGINE=ollama)

Optional state and logging:
- AOS_STATE_STORE: state backend (`memory`, `sqlite`, or `redis`). If unset, AOS preserves the legacy behavior: Redis when `REDIS_ADDR` is set, otherwise memory.
- AOS_SQLITE_PATH: SQLite database path when using `AOS_STATE_STORE=sqlite` (default `./data/aos-state.db`; parent directories are created automatically).
- REDIS_ADDR: host:port for the Redis state backend
- REDIS_PASSWORD: password for Redis
- Session values expire after 24 hours. Execution records, including callbacks awaiting a response, are stored without a TTL by all durable backends.
- The state backend is selected behind the `StateStore` interface, so additional adapters can be added without changing pipeline or agent code. Vector memory remains a separate `VectorStore` concern.
- LOG_LEVEL: info

For local durable state without a Redis service, set:
```bash
AOS_STATE_STORE=sqlite
AOS_SQLITE_PATH=./data/aos-state.db
```

Optional auth/identity (if you enable it in your environment):
- AOS_JWT_ISSUER, AOS_JWT_AUDIENCE, AOS_JWK_URL: JWT validation
- AOS_IAM_URL: external IAM integration (set to "disabled" to turn off)

You can place these in a .env file at the repo root; the binary loads .env automatically if present.

## Running

Run from source:
```bash
go run ./cmd/aos -port 8080
```

Without the flag, the server uses PORT from the environment (default 8080).

Docker:
```bash
docker build -t aos:local .
docker run --rm -p 8080:8080 \
  -e PORT=8080 \
  -e LLM_ENGINE=ollama \
  -e LLM_MODEL=qwen3:0.6b \
  -e OLLAMA_BASE_URL=http://host.docker.internal:11434 \
  aos:local
```

## Endpoints

- POST /ask — Asynchronous natural-language-like entrypoint
  - Body: { "lang": "en", "message": "..." }
  - Returns 202 with { id, status } and streams processing in the background
- POST /ask_structured — Synchronous, structured mode
  - Body: { "operation": "...", "params": { ... } }
  - Performs mapping directly without the intent step
- GET /task?id=... — Fetch task status/result
- GET /task/approve?id=... Approve task
- GET /task/reject?id
- GET /ui — Minimal UI to submit requests and observe events
- GET /health/live — Liveness
- GET /health/ready — Readiness (depends on loaded specs and LLM client state)
- GET /metrics — Prometheus metrics
- GET /static/* — Static assets

Note: Some routes may enforce auth if configured. APIAgent supports JWT/API-Key style authentication when enabled via environment/IAM settings.

## Using mock services and examples

The repository includes mock handlers under mocks/* (a dev harness, not part of the runtime) and example domain definitions under definitions/. Tool URLs in definitions/tools/*.yaml default to the mock server via `${AOS_MOCK_BASE_URL:-http://localhost:9000}`; set `AOS_MOCK_BASE_URL` to point the same definitions at another backend. You can:
- Start the orchestrator (see Running)
- Start the mocks api (listens on port 9000, override with `AOS_MOCK_PORT`):
```bash
go run ./cmd/mocks
```

- Use the UI at http://localhost:8080/ui
- Or call the API directly. Examples:

Asynchronous, natural-language-like (planner selects intent/pipeline):
```bash
curl -s -X POST http://localhost:8080/ask \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer ABCD' \
  -d '{"lang":"en","message":"Check logistics status for order 123"}'
```

Structured (operation + params directly):
```bash
curl -s -X POST http://localhost:8080/ask_structured \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer ABCD' \
  -d '{"operation":"crm.get_customer","params":{"customerId":"CUST-42"}}'
```

Fetch result:
```bash
curl -s 'http://localhost:8080/task?id=<ID_FROM_ASK>'
```

More natural-language-like messages:
- check the balance of my account 212111222
- query the shipment status for id 212111222 and customer 123
- query the vacation balance for employee 123
- add the note "customer not found" for the ticket 123
- close ticket 123 with the reason "customer not found"
- get the kubernetes-pod-234234234 status in DEV
- send 1 bitcoin to address 1Mzzzz with the concept "charity" 

the last one will be paused at a specific step, because it requires human interaction




## Secrets for tools (API keys, tokens)

When your YAML-defined tools need to call secured APIs, never hardcode secrets in the YAML. Keep secrets in environment variables and reference them from your tool templates.

How it works:
- Tool templates (URL, body, and headers) support the template function env "VAR_NAME" to read an environment variable at runtime.
- Tools can declare HTTP headers in YAML under headers: (optional).

Example YAML tool with a bearer token from environment:
```yaml
tools:
  - name: crm.get_customer
    type: http
    method: GET
    url: "https://api.example.com/customers/{{ .customerId }}"
    timeout: 5000
    headers:
      Authorization: "Bearer {{ env \"CRM_API_TOKEN\" }}"
```

Notes and best practices:
- Set environment variables before starting AOS, for example:
  - macOS/Linux: export CRM_API_TOKEN="..."
  - systemd: Environment=CRM_API_TOKEN=... in the unit file
  - Docker: pass via -e CRM_API_TOKEN=...
- Do not commit secrets to Git. Keep them out of YAML files; reference them via env() instead.
- You can combine env() with normal parameter templates.
- Default Content-Type is application/json unless overridden in headers:.

Security consideration:
- env() reads from the process environment. Prefer a secret store (Kubernetes Secrets, AWS/GCP/Azure Secret Manager, Docker/Compose secrets, etc.) and mount as env vars at runtime.

## Testing

Run unit and integration tests:
```bash
go test ./...
```

There are smoke and e2e tests under test/ that exercise the HTTP API and planner behavior.

## License

Licensed under the Apache License, Version 2.0. See LICENSE and NOTICE.

## Memory and execution history

Authenticated requests use the user ID as the conversation and vector-memory scope;
`session_id` supplied to `/ask` does not override that scope. Separate conversations
per user are not currently supported. The development authenticator still supplies
`demo-user` when configured for anonymous local use.

`GET /api/v1/tasks` lists only the authenticated user's executions.
`GET /api/v1/memories` lists their facts; `DELETE /api/v1/memories?id=...`
deletes a fact within the same owner scope. Listing follows Qdrant pagination.
Explicit conversational deletion requires the memory ID or exact fact text.
The in-memory vector store is a synchronized development/test store, not a semantic
search engine; use Qdrant and a compatible embedding model for semantic retrieval.

The UI's history **Re-run** action submits the saved intent and parameters as a new
structured request. It uses the current pipeline definitions and repeats normal
permission checks, validation and human gates; it is not replay of a versioned
snapshot. Re-running can repeat external actions.

See [TODO.md](TODO.md) for remaining PoC limitations and follow-up work.

## Protocol-backed steps

Tools can use `type: mcp`, `type: grpc`, or `type: a2a` while remaining ordinary
pipeline steps. MCP sends JSON-RPC `tools/call` requests to `target`; gRPC invokes
the configured fully-qualified method using a protobuf `Struct` request/response;
A2A sends a `message/send` JSON-RPC request and returns the remote task or message.
Each tool supports `timeout` and inherits the pipeline retry policy. See
`definitions/tools/protocols.yaml` and `definitions/pipelines/protocols_demo.yaml`.
The gRPC adapter assumes services accept `google.protobuf.Struct`; generated
protobuf clients should replace it when a service has a domain-specific contract.

## External agent steps

An agent step delegates one task to a named runtime. AOS normalizes its lifecycle
events (`agent.started`, `agent.message`, `agent.tool_call`, `agent.tool_result`,
`agent.completed`, `agent.failed`) onto the execution event stream and stores the
final result, session ID, usage, and status under `agent.<name>` in pipeline output.
The portable step parameters are `task`, `cwd`, `permission_mode`, `allowed_tools`,
`max_turns`, optional `max_budget_usd`, optional `session_id` for resuming, and
`load_project_config` to opt into loading project/user Claude hooks, MCP servers,
skills, and instructions. The adapter defaults to Claude's `--bare` mode so an
untrusted workspace cannot silently run hooks or connect MCP servers.

The first adapter is `claude_code_cli`, backed by Claude Code's documented
headless Agent SDK interface. Install and authenticate the `claude` CLI on the AOS
host. AOS invokes it without a shell, confines `cwd` to `AOS_WORKSPACE_ROOT`, and
rejects the `bypassPermissions` mode. This first version streams events and waits
synchronously for completion; it does not yet pause a running agent turn for a
human to answer a permission prompt. Configure the tool allowlist and permission
mode explicitly in the pipeline. See `definitions/agents/claude_code.yaml`,
`definitions/pipelines/agent_claude_code.yaml`, and the matching intent for a
working example.

The local mock server also exposes `POST /mock/mcp`, a deterministic CRM MCP
server. Start it with `go run ./cmd/mocks` and point `crm.mcp_lookup` at the
default `http://localhost:9000/mock/mcp` endpoint.

It also exposes the asynchronous A2A mock at `/mock/a2a/message:send` and
`/mock/a2a/tasks/{id}`. The mock returns `working` briefly and then completes
with a compliance artifact; the AOS A2A step polls it until completion or its
configured timeout.

Asynchronous providers can use `type: await_callback`. AOS exposes
`POST /api/v1/callbacks/{token}`; the token and callback URL are available as
`{{.callback_token}}` and `{{.callback_url}}` to preceding steps. A callback is
persisted before AOS returns acceptance, deduplicated while pending, and replayed
from state after restart before the pipeline resumes. Set
`AOS_CALLBACK_BASE_URL` to the externally reachable AOS base URL. See
`definitions/pipelines/callback_demo.yaml`.

Set `timeout_ms` on an `await_callback` step to persist a deadline. The verifier
expires overdue waits (including after restart), invalidates their callback
token, and exposes the task as `timed_out`. Execution records in Redis do not
expire; in-memory state is lost when the process exits. Callback waits without
`timeout_ms` remain open until a callback arrives. Compensating actions are not
yet supported.

For a tamper-evident audit journal, set `AUDIT_LOG_PATH` to a JSONL file. The
repository `run.sh` launcher enables `./data/aos-audit.jsonl` by default (set
`AUDIT_LOG_PATH` to an explicit empty value to disable it); when starting the
binary directly, configure the variable yourself. Intent
authorization, tool outcomes, and custody callback lifecycle events are
SHA-256 chained and fsynced. Each run stores a canonical, credential-redacted
snapshot of its pipeline and referenced tool/agent definitions, plus the
snapshot version and initial-input digest. Step events record whether a
condition caused a skip, the resolved redacted tool inputs and their digest,
and a digest of the safe result. ReAct events commit to each selected action
and its input digest without putting chain-of-thought into the journal. If an enabled journal cannot append an event, AOS
stops before invoking the next tool. Verify the journal with
`go run ./cmd/aos -verify-audit ./data/aos-audit.jsonl`, or verify one complete
execution (including its pipeline snapshot and recorded input/result digests)
with `go run ./cmd/aos -verify-audit ./data/aos-audit.jsonl -verify-trace <task-id>`.
Custody pipelines then
anchor the current trace hash through `blockchain.anchor_audit` when this journal
is enabled. The bundled `/mock/blockchain/anchors` endpoint is only a local
simulator that returns a receipt; it is not a real blockchain or smart contract.
A local hash chain is tamper-evident, but a real external anchor is needed to
make deletion or rewriting detectable by an independent party.

To export hash-only AOS audit evidence to Ernest, set `ERNEST_INGESTOR_URL` to
the Ernest event-ingestor base URL (for example `http://localhost:3011`) and
`ERNEST_INGESTOR_API_KEY` to Ernest's `EVENT_INGESTOR_API_KEY`. If Ernest has
`EVENT_PROVIDER_HMAC_SECRET` configured, set the same value as
`ERNEST_INGESTOR_HMAC_SECRET`; AOS signs the timestamp and exact request body
using Ernest's HMAC format. AOS posts each lifecycle record to `/events`; it sends the event digest, trace and workflow
identifiers, phase, and outcome, while keeping prompts, parameters, user IDs,
tool outputs, and error details in the local journal. Ernest responds when the
event is queued, then appends it asynchronously to its own hashchain. If the
integration is enabled and Ernest rejects or cannot accept an event, AOS stops
before continuing the audited operation. Outside development environments the
shared key is required. This export does not itself anchor the AOS trace hash
to a blockchain; Ernest's `/api/anchors` anchors the current Merkle root of its
whole provenance chain.

For Ernest running on the same host, start AOS with:

```bash
export ERNEST_INGESTOR_URL=http://127.0.0.1:3011
export ERNEST_INGESTOR_API_KEY='<same value as Ernest EVENT_INGESTOR_API_KEY>'
# Only when Ernest EVENT_PROVIDER_HMAC_SECRET is configured:
export ERNEST_INGESTOR_HMAC_SECRET='<same HMAC secret>'
./run.sh
```

When Ernest runs in Docker on a separate network, use a host-reachable URL or
attach AOS to Ernest's network and use `http://event-ingestor:3011`.

Natural-language memory facts must be verbatim excerpts of the current user message.
Ungrounded explicit facts trigger clarification; ungrounded implicit facts are discarded.
Memory operations are not stored as new memories. Their responses render confirmed
facts directly instead of asking an LLM to summarize success. Existing incorrect facts
are not automatically deleted; inspect them in Memory Management. Restarting with
`QDRANT_URL=memory` clears that temporary store.
