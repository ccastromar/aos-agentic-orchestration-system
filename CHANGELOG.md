# Changelog

All notable changes to AOS are documented here, starting with 0.5.0. Earlier versions are
described in their GitHub releases.

The format follows the spirit of Keep a Changelog, and AOS uses semantic versioning.

## [0.5.0] - Unreleased

AOS 0.5.0 ships in Chestertown 0.1.0 as the reference implementation of Run, with Prudence
(Decide and Oversee) and Ernest (Prove). The version is higher than Chestertown's because v0.3.0
is already public. There is no 0.4.0: it was never published, and its changes are part of this
release.

### Added

- Governance gate: a pipeline that belongs to an AI system asks Prudence before it runs, and
  does what the answer says. If Prudence does not answer, the pipeline does not run.
- Execution reports to Prudence, and execution evidence to Ernest.
- Protocol steps for MCP, A2A and gRPC.
- `AOS_EXECUTION_MODE=deterministic`: runs declared pipelines with no LLM, for callers that
  already reason.
- `AOS_GOVERNANCE` and `AOS_EVIDENCE` state each deployment's setup, and `/health/ready` reports
  it. Both are required outside development, and AOS will not start without them.
- Approval before dangerous tools: a pipeline that runs a `mode: dangerous` tool with no human
  approval before it does not load, unless the step states `no_approval_reason`.
  `-validate-definitions` runs the same checks before a deploy.
- Signed callbacks: an `await_callback` step with `callback_secret_env` accepts only callbacks
  the provider signed with that secret.
- A remote agent that rejects an A2A task ends the run `rejected`, with its reason, and is not
  retried.

### Changed

- **`governed` is required on every pipeline** (breaking: a definition without it no longer
  loads). `true`: an AI system; under `AOS_GOVERNANCE=prudence` Prudence decides at its entry, and
  one with no `ai_system` fails closed instead of running ungoverned. `false`, with
  `ungoverned_reason`: not an AI system (no agent, analyst, model-backed or A2A tool, or
  model-written summary, which AOS checks at load); it runs without asking Prudence and the reason
  goes into its audit record. Under `AOS_GOVERNANCE=none` a governed pipeline now runs (that
  deployment has no Prudence) instead of failing closed.
- The free-chat fallback is governed with no AI system, so under Prudence governance a request no
  intent matches does not run.
- A DAG pipeline honours `skip_summary` as a linear one does: the results are returned as they
  are, with no model.
- A finished run always ends `completed` in `/task` (breaking for callers that waited for `ok`).
  It used to end `ok` when no model was configured or the summary failed, and `completed`
  otherwise, so the same run reported two statuses depending on the path.
- The deterministic pipelines in `definitions/` (banking, CRM, devops, helpdesk, logistics, HR,
  custody, workspace, the DAG demos) drop their model-written summary
  and analyst step and declare `governed: false`; the result is returned as is. The ones that use
  a model or an agent (memory, RAG, code generation, the Claude Code agent, A2A agents, the
  fallback) and the governance demo declare `governed: true`.
- `pipeline_transfer_reviewed` names its AI system (`AOS_PRUDENCE_TRANSFER_SYSTEM_ID`): its remote
  compliance agent makes it an AI system. Unset, it does not run under Prudence governance.
- The README says what Prudence governs in AOS: one decision per pipeline run, at its entry.
- Authorization by governance function, shared with Prudence and Ernest.
- The web console is part of AOS again. Its chat is a thread of requests, each task a card with
  its status, and a run reads as a list of steps that starts with Prudence's decision; a
  pipeline that branches opens on its graph.
- Traces, metrics and logs follow Chestertown's conventions; logs carry no user content.
- Security hardening.
- Each step's output is also kept under its step id, as `{{ index . "steps.<id>.<key>" }}`, so
  two steps that call the same tool no longer overwrite each other's output. The tool-named
  keys still work.

### Removed

- ReAct pipelines and `workspace.agent_code`. A pipeline with an unknown mode now fails at load.
