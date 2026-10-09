# AOS roadmap

Updated 2026-10-03. AOS remains a learning proof of concept.

## Implemented

- YAML-driven linear, DAG and ReAct execution; conditions, retries, per-step timeouts and human gates.
- Missing-parameter clarification and intent confidence extraction.
- JWT with a development login, RBAC and audit events.
- Structured logs, tracing and Prometheus metrics.
- OpenAPI tool loading from local plugin definitions.
- Session context, execution recall and vector memory with Qdrant or an in-memory development store.
- React dashboard, chat, events, settings, execution history and memory management.
- Tamper-evident local audit journal with optional hash-only event export to Ernest.

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
- Add end-to-end coverage using a real LLM; current automated tests use controlled doubles.
- Introduce explicit persisted execution status instead of inferring completion from step results.
- Decide whether to support separate conversations per user; current memory/context scope is one per authenticated user.
- Review and commit the carried-over local work together with these fixes in both repositories.

## Future capabilities

- Immutable snapshots of external context and provider/runtime versions; pipeline, referenced tool, and agent definition snapshots with digests are now captured per execution.
- Dedicated risk/compliance agents (current banking checks are tools).
- Remote dynamic tool discovery beyond local OpenAPI definitions.
- Domain SDKs for banking, crypto, AI operations and DevOps.
- LLM supervisor for coherence checks, monitoring and diagnostics.
- End-to-end AOS + Ernest evidence-verification demo, including Ernest queue processing and an independently verifiable anchor.
