# Changelog

All notable changes to AOS are documented here, starting with 0.4.0. Earlier versions are
described in their GitHub releases.

The format follows the spirit of Keep a Changelog, and AOS uses semantic versioning.

## [Unreleased]

AOS 0.4.0 ships in Chestertown 0.1.0, with Prudence (govern) and Ernest (prove). The version is
higher than the suite's because v0.3.0 is already public.

### Added

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
