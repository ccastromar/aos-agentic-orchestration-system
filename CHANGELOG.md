# Changelog

All notable changes to AOS are documented here, starting with 0.4.0. Earlier versions are
described in their GitHub releases.

The format follows the spirit of Keep a Changelog, and AOS uses semantic versioning.

## [Unreleased]

AOS 0.4.0 ships in Chestertown 0.1.0 as the reference implementation of Run, with Prudence
(Decide and Oversee) and Ernest (Prove). The version is higher than Chestertown's because v0.3.0
is already public.

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

- Authorization by governance function, shared with Prudence and Ernest.
- The web console is part of AOS again.
- Traces, metrics and logs follow Chestertown's conventions; logs carry no user content.
- Security hardening.

### Removed

- ReAct pipelines and `workspace.agent_code`. A pipeline with an unknown mode now fails at load.
