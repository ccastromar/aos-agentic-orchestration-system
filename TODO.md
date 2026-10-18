# AOS roadmap

Updated 2026-10-17. AOS is the "operate" vertex of the Chestertown MVP (AOS operates, Prudence
governs, Ernest proves); see the README status.

## Implemented

- YAML-driven linear, DAG and ReAct execution; conditions, retries, per-step timeouts and human gates.
- Missing-parameter clarification and intent confidence extraction.
- JWT with a development login or OIDC/JWKS validation against the shared Chestertown IdP, RBAC and audit events.
- Structured logs, tracing and Prometheus metrics.
- OpenAPI tool loading from local plugin definitions.
- Session context, execution recall and vector memory with Qdrant or an in-memory development store.
- React dashboard, chat, events, settings, execution history and memory management.
- Tamper-evident local audit journal with optional hash-only event export to Ernest.
- Prudence governance gate (PEP) at pipeline entry, with client-credentials auth to a secured PDP.
- Deterministic mock LLM (`cmd/mock-ollama`, shipped as `aos-mock-llm`) for live demos without a model.

## Chestertown MVP — governed live demo (2026-10)

- [x] Pipelines declare `ai_system`; `governed: true` fails closed when the system id or `PRUDENCE_PDP_URL` is missing.
- [x] DENY and APPROVAL_REQUIRED end the task as `governance_blocked` with the verdict, persisted for `/task`; approval only in Prudence.
- [x] Four live-demo pipelines, one per EU AI Act case (limited, high-risk, prohibited, suspended), each bound to its own Prudence system.
- [x] Read-only mock tools for them; the social-scoring tool refuses and logs if it is ever reached.
- [x] Deterministic mock LLM: Ollama and OpenAI-style `/v1` APIs, pinned demo phrases, intent-catalog matching, honest analyst summaries, OpenAI tool calling and canned replies, `#mock-fail:*` markers.
- [x] Unit tests for the governance gate, `governance_blocked`, fail-closed config and the demo pipelines' definitions; CI green again (custody anchor test fixed).
- [x] Deployed with Prudence and Ernest on the MVP server; the four cases verified from the AOS UI.
- [x] Execution evidence to Ernest over Kafka: the audit journal is the outbox, a background shipper publishes it (TLS), governed `pipeline.started` events are tagged as executions of their AI system for Prudence's time-aware drift check.

## Memory and history closure — codex/close-memory-history

- [x] Fix missing-identity panic in `/ask`.
- [x] Scope authenticated conversation and memory context to the user, ignoring caller-supplied session IDs.
- [x] Filter execution history by authenticated owner and protect persisted task details.
- [x] Scope Qdrant deletion to both memory ID and owner; wait for mutations to complete.
- [x] Synchronize the in-memory vector store and test concurrent operations.
- [x] Follow Qdrant list pagination and restore memory timestamps.
- [x] Propagate memory operation errors; delete by ID or exact text instead of broad substring matches.
- [x] Avoid duplicate implicit extraction for explicit memory operations.
- [x] Reject model-generated personal facts absent from the user message.
- [x] Keep memory-operation summaries out of the vector store and render memory replies directly from confirmed tool results.
- [x] Allow unknown intents to reach the declared fallback pipeline instead of requesting `invalid_intent` as a parameter.
- [x] Re-run history entries through `/ask_structured`, with a fresh task and normal RBAC, guards and approval gates.
- [x] Generic `await_callback` step with authenticated callback webhook and persisted resume state.
- [x] Reset visible task/chat state when switching users.

## Remaining follow-up

- Validate against a real Qdrant instance and configured embedding model (collection dimensions are currently fixed at 768).
- Add end-to-end coverage using a real LLM; current automated tests and the live demo use controlled doubles (the deterministic mock).
- Introduce explicit persisted execution status instead of inferring completion from step results (only `governance_blocked` is persisted explicitly today).
- Decide whether to support separate conversations per user; current memory/context scope is one per authenticated user.
- Kafka evidence runs over TLS without client authentication on the MVP server, so Ernest records it as "unverified"; move the broker to SASL_SSL with per-producer ACLs (AOS may only write `aos.executions`).
- Link each execution to the Prudence decision that allowed it (the PDP response carries no decision id yet), so Ernest can prove the pairing, not only the timing.
- Governance is checked once at pipeline entry; consider per-step checks for long or multi-system pipelines, and re-checking on resume after a callback wait.
- On APPROVAL_REQUIRED, point the user to the system's review in Prudence (link or notification) instead of only showing the verdict.
- The mock LLM recognises AOS's prompts by fixed marker strings that mirror `internal/llm`; add a test that fails when those prompts change without updating `mocks/ollama/respond.go`.

## Future capabilities

- Immutable snapshots of external context and provider/runtime versions; pipeline, referenced tool, and agent definition snapshots with digests are now captured per execution.
- Dedicated risk/compliance agents (current banking checks are tools).
- Remote dynamic tool discovery beyond local OpenAPI definitions.
- Domain SDKs for banking, crypto, AI operations and DevOps.
- LLM supervisor for coherence checks, monitoring and diagnostics.
- End-to-end AOS + Ernest evidence-verification demo, including Ernest queue processing and an independently verifiable anchor.
