# AOS — Agent Orchestration System

> **Project status: learning proof-of-concept.**
> AOS is a personal PoC built to explore how to orchestrate LLM-assisted, deterministic
> workflows in Go. The core runtime (bus, agents, YAML-driven pipelines, tool execution,
> human gates) works and is covered by tests (`go test ./...` passes, including `-race`).
> It is **not** production-ready and is not actively maintained as a product.
>
> **Not implemented / demo-only — do not rely on for security:**
> - **JWT authentication** (`internal/auth/jwt.go`) is a stub: it does **not** verify token
>   signatures or claims and accepts any bearer token as an `admin` user. Enable the auth
>   chain only in trusted/local environments.
> - **RBAC** (`internal/rbac`) and the **audit log** (`internal/audit`) are implemented as
>   packages but are **not wired into the request path**; roles are not enforced.

## Introduction

This project is a **runtime orchestration framework for intent-driven pipelines**, where orchestration is defined as code and execution is strictly constrained by declarative specifications.

Despite the term *agents* being commonly used in the AI ecosystem, this framework does **not** define conversational or configurable agents at runtime. Instead, agents are **fixed, compiled goroutines written in Go**, designed to communicate through in-memory channels. What is configurable—and intentionally so—is the **orchestration layer**.

At the core of the system lies an **intent-based runtime**. A single Go binary exposes an API that receives natural-language input, extracts an intent using an NLM/LLM component, and then **resolves that intent against a predefined set of YAML specifications**. If an intent or execution path is not declared in YAML, it simply cannot run. There is no dynamic improvisation at runtime—only controlled execution.

### Orchestration as Code

The framework uses YAML to describe:

* **Pipelines**, each representing domain-specific orchestration (e.g., banking, shipping, CRM).
* **Steps**, where each step is effectively a tool invocation.
* **Tools**, which in practice are API calls (internal or external).
* **Intents**, mapping natural-language requests to allowed pipelines.

A pipeline might represent, for example, a banking transfer flow:

1. AML / compliance check
2. Balance retrieval
3. Risk scoring
4. Transfer execution

Each step is explicit, auditable, and deterministic.

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

The app boots agents and an HTTP server, then routes user requests through the bus: APIAgent → Inspector → Planner → Verifier → Analyst. The Planner reads YAML definitions to select the right pipeline and tools. The Verifier executes tools with guardrails. The Analyst uses the LLM to generate a human summary.

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
- REDIS_ADDR: host:port to enable Redis-backed state
- REDIS_PASSWORD: password for Redis
- LOG_LEVEL: info

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

The repository includes mock handlers under internal/mocks/* and example domain definitions under definitions/. You can:
- Start the orchestrator (see Running)
- Start the mocks api (listens on port 9000):
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
