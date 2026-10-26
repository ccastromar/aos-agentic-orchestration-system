# AOS — Agentic Orchestration System

> **Project status: MVP — the "operate" vertex of the Chestertown trilogy.**
> AOS operates, [Prudence](https://github.com/ccastromar/prudence-governance) governs and
> [Ernest](https://github.com/ccastromar/ai-blockchain-provenance) proves. The three run together as a deployed MVP aligned with the EU AI Act: before a
> governed pipeline runs, AOS asks Prudence's Policy Decision Point and obeys the verdict;
> Prudence seals each decision in Ernest, and AOS ships its own hash-only execution evidence
> there asynchronously over Kafka. It is a first real version, not a hardened product;
> see [TODO.md](TODO.md) for known limitations. The runtime (bus, agents, YAML-driven
> pipelines, tool execution, human gates, governance gate) is covered by tests in CI
> (`go test -race ./...`).
>
> **Implemented security features:**
> - **JWT authentication** (`internal/auth/jwt.go`): tokens validated against an OIDC issuer's
>   JWKS (`AOS_JWT_ISSUER`, `AOS_JWK_URL`) or signed with a shared `JWT_SECRET`.
> - **RBAC** (`internal/rbac`) and the **audit log** (`internal/audit`) are wired into the request path and enforce role-based access control based on `permissions.yaml`.
> - **Governance gate** (`internal/governance`): Prudence's PDP decides; AOS fails closed when it
>   cannot get a verdict (see [Governance with Prudence](#governance-with-prudence-pep)).

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

- Go 1.26+ (see go.mod)
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
- LOG_LEVEL: info. At `info` and above AOS logs ids, intents, pipelines and parameter **names**, never
  what users type or tools return. `debug` also logs messages, LLM responses and tool outputs:
  for local development only, never where logs are collected.

For local durable state without a Redis service, set:
```bash
AOS_STATE_STORE=sqlite
AOS_SQLITE_PATH=./data/aos-state.db
```

Optional auth/identity (if you enable it in your environment):
- AOS_JWT_ISSUER, AOS_JWT_AUDIENCE, AOS_JWK_URL: JWT validation
- AOS_IAM_URL: external IAM integration (set to "disabled" to turn off)
- AOS_ROLE_CLAIM, AOS_ROLE_MAP: the claim with the roles and how the IdP's names translate to AOS roles
- AOS_GOVERNANCE_MODEL: the organization's governance model (Chestertown governance functions, contract v1 in `internal/governance/contract/`); empty loads the contract's default model, which reproduces the roles above. An invalid model stops AOS at start

Optional tracing (OpenTelemetry, standard variables):
- OTEL_EXPORTER_OTLP_ENDPOINT (or OTEL_EXPORTER_OTLP_TRACES_ENDPOINT): the collector, OTLP over
  HTTP (`http://otel-collector:4318`). Unset, AOS records no spans but still passes an incoming
  `traceparent` on to Prudence.
- OTEL_SERVICE_NAME (default `aos`), OTEL_RESOURCE_ATTRIBUTES (`deployment.environment.name=production`),
  OTEL_TRACES_SAMPLER, OTEL_SDK_DISABLED.
- Spans carry ids, routes, pipeline and tool names, never prompts, parameters or tool outputs.
  The task id travels as the correlation id (`correlation_id`), not as a trace id.

You can place these in a .env file at the repo root; the binary loads .env automatically if present.

## Running

Run from source:
```bash
go run ./cmd/aos -port 8080
```

Without the flag, the server uses PORT from the environment (default 8080).

The web console lives in [`ui/`](ui/README.md) (React + Vite). `./run.sh all` starts the mock
tool servers, AOS and the console together (http://localhost:5173), installing the console's
dependencies on first run; `./run.sh demo` does the same with the deterministic mock model
instead of a real LLM.

Docker (the image listens on 9090; set `PORT` to change it):
```bash
docker build -t aos:local .
docker run --rm -p 9090:9090 \
  -e LLM_ENGINE=ollama \
  -e LLM_MODEL=qwen3:0.6b \
  -e OLLAMA_BASE_URL=http://host.docker.internal:11434 \
  aos:local
```

### Live demos without a model (mock Ollama)

`./run.sh demo` starts the same stack as `./run.sh all`, but points AOS at `cmd/mock-ollama`
(port 11435): a **deterministic** Ollama-compatible server, not a language model. It
advertises the model `aos-demo:deterministic` so UI stats and evidence never suggest a real
LLM. It recognises the fixed AOS prompts (intent, parameter extraction, analyst summary,
ReAct, code generation) and answers them by rule: demo phrases pinned in
`mocks/ollama/scenarios.yaml` first, then keyword overlap with the intent catalog AOS already
sends in its prompt. Summaries only report what the step results contain and never claim
success for `executed: false`.

The same answers are served OpenAI-style under `/v1` (`/v1/models`, `/v1/chat/completions`,
`/v1/embeddings`), so a deployment that uses `LLM_ENGINE=mock` with
`LLM_BASE_URL=http://<mock>:11435/v1` gets the identical demo script. On `/v1` it also does
OpenAI-style tool calling (it picks the best-matching tool, then answers with the tool's
result), and scenarios can pin a `reply` for a free-form prompt. Prudence's Pydantic AI and
LangGraph framework demos use it that way. The Docker image ships it as
`/usr/local/bin/aos-mock-llm`.

For presenting failures live, type a marker in the chat message: `#mock-fail:500`,
`#mock-fail:badjson` (malformed model output) or `#mock-fail:slow`. Tuning:
`MOCK_OLLAMA_PORT`, `MOCK_OLLAMA_MODEL`, `MOCK_OLLAMA_TOKEN_DELAY_MS`, `MOCK_OLLAMA_SLOW_MS`,
`MOCK_OLLAMA_SCENARIOS`.

## Governance with Prudence (PEP)

AOS is a **Policy Enforcement Point** for Prudence. A pipeline opts in by naming the AI system it
runs as (registered in Prudence's registry); `${VAR}` references are expanded from the
environment:

```yaml
- name: pipeline_aos_credit_pre_screening
  ai_system: "${AOS_PRUDENCE_CREDIT_SYSTEM_ID}"
  governed: true
  steps: [...]
```

At pipeline entry the planner calls `POST $PRUDENCE_PDP_URL/api/policy/decisions` with the
subject, pipeline, AI system and action:

| Verdict | What AOS does |
|---|---|
| `ALLOW` (status 200, with a `decisionId`) | Runs the pipeline. |
| `DENY` (403) | Stops before any step; the task ends as `governance_blocked`. |
| `APPROVAL_REQUIRED` (202) | Same: approval happens only in Prudence's review workflow, never via a local AOS click. |
| PDP unreachable, unreadable or unexpected answer (an `ALLOW` under any other status, no `decisionId`, `ERROR`) | Stops with an `error` (fail-closed). |

The gate runs Prudence's Decision API contract in its tests: the vectors in
`internal/governance/decision-contract/`, copied from prudence-governance `contract/decision/`
by its `sync.sh` and checked against their SHA-256 sums.

A `governance_blocked` result carries the verdict (`decision`, `reason`, `approver`,
`aiSystemId`, `pipeline`, `message`) and is persisted, so `/task` still shows it after the
one-shot result is consumed; the UI renders it as a policy outcome, not as a failure.
`governed: true` makes a pipeline **fail closed** when `ai_system` (e.g. an unset env var) or
`PRUDENCE_PDP_URL` is missing; without the flag such a pipeline would run ungoverned.

| Variable | Purpose |
|---|---|
| `PRUDENCE_PDP_URL` | Prudence base URL; the gate is off when empty (except for `governed` pipelines). |
| `PRUDENCE_PDP_TOKEN_URL`, `PRUDENCE_PDP_CLIENT_ID`, `PRUDENCE_PDP_CLIENT_SECRET`, `PRUDENCE_PDP_SCOPE` | OAuth2 client-credentials for a secured PDP (scope `pdp.decide`); failing to get a token fails closed. |
| `PRUDENCE_PDP_SUBJECT` | Service identity sent as subject (a secured PDP takes it from the token anyway). Defaults to the signed-in user. |

The live demo has four governed pipelines, one per EU AI Act case, each bound to a seeded
Prudence system:

| Pipeline | System id variable | Seeded system | First verdict |
|---|---|---|---|
| `pipeline_aos_support_lookup` | `AOS_PRUDENCE_DEMO_SYSTEM_ID` | Customer-support assistant (limited) | DENY until one approval |
| `pipeline_aos_credit_pre_screening` | `AOS_PRUDENCE_CREDIT_SYSTEM_ID` | Consumer-credit pre-screening (high) | APPROVAL_REQUIRED until two independent approvals |
| `pipeline_aos_social_scoring` | `AOS_PRUDENCE_SOCIAL_SCORING_SYSTEM_ID` | Social scoring (prohibited) | Always DENY (OPA does not grant it either) |
| `pipeline_aos_translate_message` | `AOS_PRUDENCE_TRANSLATOR_SYSTEM_ID` | Translator (suspended) | DENY until resubmitted and approved |

`prudence-governance/scripts/aos-demo-env.sh` prints those variables from a running Prudence,
and `scripts/aos-demo-rearm.sh` returns the four systems to their starting verdicts to replay
the demo. The step-by-step script is `prudence-governance/docs/functional/06-demo-aos-en-vivo.md`.

## Endpoints

- POST /ask — Asynchronous natural-language-like entrypoint
  - Body: { "lang": "en", "message": "..." }
  - Returns 202 with { id, status } and streams processing in the background
- POST /ask_structured — Synchronous, structured mode
  - Body: { "operation": "...", "params": { ... } }
  - Performs mapping directly without the intent step
- GET /task?id=... — Fetch task status/result
- POST /task/approve?id=...&gate=... and POST /task/reject?id=...&gate=... — Decide a human step
  (`oversight`, or the requester unless the governance model makes them incompatible)
- POST /task/reply?id=... — Answer a clarification (the requester only)
- GET /api/v1/tasks — Your tasks (everyone's with `oversight` or `audit`)
- GET /api/v1/events/stream — Server-sent events for the console
- GET /intents, GET /api/v1/memories, POST /api/v1/callbacks/{token}
- GET /health/live — Liveness
- GET /health/ready — Readiness (depends on loaded specs and LLM client state)
- GET /metrics — Prometheus metrics

The server has no UI of its own: the console is in [`ui/`](ui/README.md).

Note: Some routes may enforce auth if configured. APIAgent supports JWT/API-Key style authentication when enabled via environment/IAM settings.

## Using mock services and examples

The repository includes mock handlers under mocks/* (a dev harness, not part of the runtime) and example domain definitions under definitions/. Tool URLs in definitions/tools/*.yaml default to the mock server via `${AOS_MOCK_BASE_URL:-http://localhost:9000}`; set `AOS_MOCK_BASE_URL` to point the same definitions at another backend. You can:
- Start the orchestrator (see Running)
- Start the mocks api (listens on port 9000, override with `AOS_MOCK_PORT`):
```bash
go run ./cmd/mocks
```

- Use the console (`./run.sh all`, http://localhost:5173)
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




## Tool network access (SSRF protection)

HTTP tools cannot reach private, loopback or link-local addresses: a tool URL that resolves to an
internal IP is refused (`ssrf prevention: access to internal IP … is denied`). Two switches:

| Variable | Effect |
|---|---|
| `AOS_TOOL_INTERNAL_HOSTS` | Comma-separated tool hostnames that may resolve to internal IPs, matched exactly (e.g. `aos-tools-mock` on a Docker network). Everything else stays protected. |
| `ALLOW_LOCAL_TOOLS=true` | Turns the protection off entirely (and relaxes the dangerous-chain guard). Local development only. |

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

See [TODO.md](TODO.md) for known limitations and follow-up work.

## Protocol-backed steps

Tools can use `type: mcp`, `type: grpc`, or `type: a2a` while remaining ordinary
pipeline steps. Each supports `timeout`, inherits the pipeline retry policy, and goes through
the same SSRF check as HTTP tools: an internal address is refused unless its host is in
`AOS_TOOL_INTERNAL_HOSTS`. See `definitions/tools/protocols.yaml`,
`definitions/pipelines/protocols_demo.yaml` and the intent `protocols.customer_review`.

- **MCP** (Streamable HTTP): AOS sends `initialize` (protocol `2025-11-25`), keeps the
  session the server assigns (`Mcp-Session-Id`, `MCP-Protocol-Version`), reads the tool's
  input schema from `tools/list` so string parameters arrive as the declared types (integer,
  number, boolean, object, array), calls `tools/call`, accepts a JSON or an SSE answer, and
  closes the session. `remote_tool` names the server's tool when it differs from the AOS tool
  name. A result with `isError: true` fails the step. A minimal server without `initialize`
  is called without a session.
- **A2A** (JSON-RPC binding): `protocol_version: "0.3"` (default: `message/send`,
  `tasks/get`) or `"1.0"` (`SendMessage`, `GetTask`), announced in `A2A-Version`. The message
  carries `messageId`; it is sent non-blocking and the task is polled on the same endpoint,
  backing off from 200 ms to 2 s, while it is submitted or working. Only `completed` (or a
  direct message reply) succeeds; `failed`, `canceled`, `rejected`, `input-required` and
  `auth-required` fail the step, since a pipeline step cannot answer the agent.
- **gRPC**: TLS unless the tool sets `insecure: true` (a local or in-cluster plaintext
  service). When the server offers gRPC server reflection, AOS calls the method with its real
  request and response types (string parameters converted to the fields' types; messages,
  lists and maps given as JSON) and returns the response as JSON. Without a descriptor it
  falls back to `google.protobuf.Struct` in and out. Streaming methods are refused.

Checked against servers built with the official MCP and A2A SDKs: `test/interop/`.

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

It also exposes an asynchronous A2A agent at `/mock/a2a` (JSON-RPC: `message/send`
and `tasks/get`), and a gRPC risk service (`risk.RiskService/Score`, `Struct` in and out) on
`AOS_MOCK_GRPC_PORT` (default 9002; `off` disables it). The task is `working` briefly and then completes with a
compliance artifact; the AOS A2A step polls it until completion or its
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

### The evidence key (required to send evidence)

Whatever leaves AOS for Ernest identifies each audit record by `eventDigest`, an HMAC-SHA256 of
the record's hash under the installation's evidence key, never by the hash itself: the hash covers
the user and the parameters, and a bare one could be confirmed by guessing
(`contract/audit-events/README.md`). Set one of:

| Variable | Purpose |
|---|---|
| `AOS_EVIDENCE_KEY` | The key, at least 32 bytes, e.g. injected from a key vault (Azure Key Vault reference, External Secrets Operator). |
| `AOS_EVIDENCE_KEY_FILE` | A file holding it, e.g. a Kubernetes CSI secret or a Docker Compose secret. |

Outside development AOS refuses to start without it when the Kafka shipper or the HTTP export is
on. In development it generates one next to the journal (`<AUDIT_LOG_PATH>.evidence-key`, mode
0600). Never change it: Ernest deduplicates replays by the digest, and an auditor needs the key
in use at the time to match records; keep the previous key if you ever rotate.

### Execution evidence to Ernest over Kafka (recommended)

Set `AOS_EVIDENCE_KAFKA_BROKERS` and AOS publishes its audit journal to Ernest asynchronously.
The journal (`AUDIT_LOG_PATH`, required) is the outbox: every event is durable and hash-chained
on disk first; a background shipper reads complete records after a persisted cursor
(`<journal>.kafka-cursor`), publishes them to `AOS_EVIDENCE_KAFKA_TOPIC` (default
`aos.executions`) and advances the cursor only once Kafka acknowledges them. Pipelines never wait
for or fail because of Kafka or Ernest; the backlog ships when they are back, and replays are
deduplicated by Ernest on `sourceEventId`. Records carry the same hash-only body as the HTTP path,
plus `journalSequence`. The record's chain hash travels only as a keyed digest (`eventDigest`,
see below), so whoever holds the evidence key can match Ernest's evidence with the local journal
and nobody else can test guesses about who ran what.

Events of a pipeline that declares `ai_system` carry `metadata.aiSystemId`, and the
`pipeline.started` event (recorded only after the governance gate allowed the run) is tagged
`stage: execution`. That is what Ernest counts per AI system and what Prudence's drift check reads.

| Variable | Purpose |
|---|---|
| `AOS_EVIDENCE_KAFKA_BROKERS` | Comma-separated brokers; empty disables the shipper. |
| `AOS_EVIDENCE_KAFKA_TOPIC` | Topic (default `aos.executions`); Ernest's Kafka consumer must subscribe to it. |
| `AOS_EVIDENCE_KAFKA_SECURITY_PROTOCOL` | `PLAINTEXT` (development only), `SSL`, `SASL_PLAINTEXT` or `SASL_SSL`. Outside development AOS refuses to start unless it is `SSL` or `SASL_SSL`. |
| `AOS_EVIDENCE_KAFKA_TLS_CA_FILE` | PEM CA for TLS. |
| `AOS_EVIDENCE_KAFKA_SASL_MECHANISM`, `_SASL_USERNAME`, `_SASL_PASSWORD` | `SCRAM-SHA-512` (default), `SCRAM-SHA-256` or `PLAIN`. |
| `AOS_EVIDENCE_KAFKA_CURSOR`, `AOS_EVIDENCE_KAFKA_INTERVAL` | Cursor file and polling interval (default `1s`). |

### Execution evidence to Ernest over HTTP (synchronous)

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
