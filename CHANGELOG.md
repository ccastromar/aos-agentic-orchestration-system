# Changelog

All notable changes to AOS are documented here, starting with 0.4.0. Earlier versions are
described in their GitHub releases.

The format follows the spirit of Keep a Changelog, and AOS uses semantic versioning.

## [Unreleased]

AOS 0.4.0 ships in Chestertown 0.1.0, with Prudence (govern) and Ernest (prove). The version is
higher than the suite's because v0.3.0 is already public.

### Added

- **Audit events contract with Ernest** (`contract/audit-events/`): the exact Kafka record each
  journal entry becomes and what Ernest's event-ingestor must make of it (deduplication id,
  model, `stage: execution` for Prudence's drift). AOS's test pins the bytes and that no user
  id, parameter or error leaves AOS; Ernest runs the same vectors on a copy made by `sync.sh`.
- **Prudence governance gate** (policy enforcement point): a pipeline with an `ai_system` asks
  Prudence's PDP before it starts and enforces `ALLOW`, `DENY` or `APPROVAL_REQUIRED`; the last
  one routes into the human-approval step. Active when `PRUDENCE_PDP_URL` is set, fail-closed
  when the PDP is unreachable, authenticated to a secured PDP with client credentials
  (`PRUDENCE_PDP_*`). Ungoverned pipelines are unchanged.
- **Execution reporting to Prudence** (Decision API v1): when a governed pipeline starts, AOS
  posts `POST /api/executions` citing the decision, in the background with retries; it never
  blocks a run. `PRUDENCE_REPORT_EXECUTIONS=false` turns it off.
- **Execution evidence to Ernest over Kafka**: the audit journal is the outbox; a background
  shipper publishes complete records (idempotent, all-ISR acks, TLS and SASL) and advances its
  cursor only on acknowledgement (`AOS_EVIDENCE_KAFKA_*`). Governed runs report as their AI
  system. Pipelines never wait for Kafka or Ernest.
- **Protocol steps that interoperate**: MCP over Streamable HTTP (sessions, typed tool
  parameters, `isError`), A2A 0.3 and 1.0, gRPC with typed calls through server reflection and
  TLS. Every outbound step goes through the SSRF check; named internal hosts can be allowed
  without disabling it.
- `GET /task` returns the Prudence AI system and decision of a governed run, so the console links
  a run to Prudence and Ernest.
- Governed demo pipelines (support lookup, consumer credit, social scoring, translator) and a
  deterministic mock LLM (OpenAI-style, with tool calling) shipped in the image for live demos.
- Long-term memory: the Analyst keeps structured facts from step results, and `system.recall`
  answers questions about the user's own session history.

### Changed

- **OpenTelemetry tracing** (Chestertown observability conventions). With
  `OTEL_EXPORTER_OTLP_ENDPOINT` set, AOS exports spans over OTLP: each HTTP request (named by its
  route), the planning of a task (`aos.plan`), its run (`aos.run`), each step (`aos.step`) and
  the calls to Prudence, which continue the trace (`traceparent`). The execution report, sent in
  the background, is a span of its own linked to the run. Without an endpoint nothing is
  recorded, and an incoming `traceparent` is still passed on.
- **Runtime and process metrics on `/metrics`**: goroutines, heap, GC, CPU, start time, and on
  Linux resident memory and file descriptors, under Prometheus's usual names.
- **Exemplars on `/metrics`**: served as OpenMetrics when the scraper asks for it, the gate, run
  and step histograms tie each bucket to the trace of a recent, sampled measurement.
- **Domain metrics** on `/metrics`: run, step and gate duration histograms (buckets chosen for
  the suite's service-level indicators), evidence shipped to Ernest by transport and outcome, and
  the journal backlog not yet shipped over Kafka. Labels are fixed sets, never ids or names.
- **JSON logs with the suite's fields** (`LOG_FORMAT`, JSON by default outside development): one
  object per line with `timestamp`, `severity`, `service.*`, `deployment.environment.name`,
  `message`, and a task's `correlation_id`, `trace_id` and `span_id`. The access log is an
  `http.request` event with the route, never the raw path. `LOG_LEVEL` is now applied: debug lines,
  which may hold what users type, were written at every level.
- **The audit journal is no longer echoed to the logs.** Each record (user, roles, parameters)
  was also printed whole to stdout; the log now says only that a record was written
  (`audit.recorded`, with its sequence, phase and task).
- **The task id is the correlation id.** Calls to Prudence carry it as `X-Correlation-Id` and the
  decision request's `correlationId`; the evidence sent to Ernest carries it as
  `metadata.correlationId` and a `correlationId` header (Kafka) or `X-Correlation-Id` (HTTP). Each
  journal entry keeps the `traceparent` that wrote it, and its evidence carries it, so Ernest's
  processing links to the run. A trace id is no longer derived from the task id.
- **HTTP metrics label routes, not ids.** `aos_http_requests_total` and
  `aos_http_request_seconds` labelled the full path, so a callback's token (a secret) was stored in
  the metrics and every id made a new series; the path is now the route, with `{token}` and `{id}`.
- **Logs carry no user content.** At `info` AOS logged every user message in full, and every
  pipeline's parameters with their values; unknown bus messages were dumped whole at `warn`. Logs
  reach whoever operates the platform (Loki, in the suite's deployment) and outlive the request.
  They now log a message's length, parameter names (`logx.Keys`) and a message's type; the values
  stay in the audit journal. `debug` still logs content, for local development only.
- **Audit evidence carries a keyed digest, not the bare hash** (audit events contract v2,
  `schemaVersion` 3). The hash of a journal record covers the user and the parameters, and the
  rest of what goes into it travels to Ernest, so a reader of Ernest could confirm a guess of who
  ran what. AOS now sends `eventDigest` = HMAC-SHA256 under the installation's evidence key
  (`AOS_EVIDENCE_KEY`, or `AOS_EVIDENCE_KEY_FILE` for a key vault or container secret), over Kafka
  and HTTP alike. Outside development AOS refuses to start without it when evidence goes to Ernest;
  in development it keeps a generated one next to the journal. The journal's hash chain is
  unchanged.
- **The governance gate pairs the status with the decision.** It runs a pipeline only on status
  200 with `ALLOW` and a `decisionId`; before, an `ALLOW` body under an error status, or without
  a `decisionId`, ran it. Checked by the vectors of Prudence's Decision API contract
  (`internal/governance/decision-contract/`).
- **The console is part of AOS again**, in `ui/` (React, on the shared `@chestertown/design`),
  imported with its history from the former aos-ui repository. It follows AOS's version, has its
  own CI job and starts from `run.sh`. It signs in with OIDC (discovery, configurable scope) and
  signs out suite-wide.
- **Authorization by governance function** (Chestertown contract `chestertown.governance/v1`).
  A token from the identity provider resolves to functions through the organization's governance
  model (`AOS_GOVERNANCE_MODEL`, or the packaged default); a token that resolves to none is
  refused with 403. `ai.use:<domain>` runs the pipelines of that RBAC domain; `oversight` decides
  anyone's human step and sees every task; `audit` reads every task; only the requester answers
  a clarification. With `[ai.use, oversight]` in the model's `rules.incompatible`, whoever started
  a task cannot decide its human step. API keys and development tokens keep their roles, mapped
  to functions.
- Identity: OIDC/JWKS validation against the suite's identity provider (`AOS_JWT_ISSUER`,
  `AOS_JWT_AUDIENCE`, `AOS_JWK_URL`); `operator` replaces the old read-write role.
- RBAC is default-deny for unknown intents, with per-domain grants in
  `definitions/rbac/permissions.yaml`.
- The server no longer embeds a UI; consoles consume the event bus over SSE.
- Mock servers moved out of `internal/`, with a configurable base URL.
- Go 1.26.10, grpc 1.84, x/net 0.61, x/text 0.43 (govulncheck clean). The image listens on
  `PORT` (9090 by default).
