# Changelog

All notable changes to AOS are documented here, starting with 0.4.0. Earlier versions are
described in their GitHub releases.

The format follows the spirit of Keep a Changelog, and AOS uses semantic versioning.

## [Unreleased]

AOS 0.4.0 ships in Chestertown 0.1.0, with Prudence (govern) and Ernest (prove). The version is
higher than Chestertown's because v0.3.0 is already public.

### Added

- Governance gate: a pipeline that belongs to an AI system asks Prudence before it runs, and
  does what the answer says. If Prudence does not answer, the pipeline does not run.
- Execution reports to Prudence, and execution evidence to Ernest.
- Protocol steps for MCP, A2A and gRPC.

### Changed

- Authorization by governance function, shared with Prudence and Ernest.
- The web console is part of AOS again.
- Traces, metrics and logs follow Chestertown's conventions; logs carry no user content.
- Security hardening.
