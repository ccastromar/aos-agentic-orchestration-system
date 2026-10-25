# Security Policy

AOS runs AI pipelines under control: governed pipelines ask Prudence's policy decision point
before they start and fail closed when it is unreachable. It is a young project; review the
deployment notes below before running it with real data.

## Reporting a vulnerability

Please do not report vulnerabilities in public issues. Use GitHub's private vulnerability
reporting: the repository's **Security** tab → **Report a vulnerability**. Include the version or
commit, what an attacker can do and how to reproduce it; leave out real secrets and personal data.
Only the latest release receives fixes.

## Scope

Security-sensitive areas include:

- The governance gate: a pipeline with an `ai_system` must never run without an `ALLOW` from
  Prudence (`PRUDENCE_PDP_*`), and its client-credentials secret.
- Authorization by governance function: identity-provider tokens (`AOS_JWT_ISSUER`,
  `AOS_JWT_AUDIENCE`, `AOS_JWK_URL`) resolve to functions through the governance model
  (`AOS_GOVERNANCE_MODEL`); RBAC is default-deny for unknown intents.
- Outbound protocol steps (HTTP, MCP, A2A, gRPC) and their SSRF protection
  (`AOS_TOOL_INTERNAL_HOSTS` only widens it for named hosts).
- The audit journal and the evidence it ships to Ernest.
- LLM provider credentials.

## Deployment expectations

- Set `APP_ENV` to anything but `dev`, `development`, `local` or `test`: those enable the
  development login and the mock identity, which are for local use only.
- Configure the identity provider and the governance model; keep the model file read-only.
- Keep `ALLOW_LOCAL_TOOLS=false` and the SSRF protection on.
- Store secrets (PDP client secret, LLM keys, Kafka credentials) in a secrets manager.

## Verifying release images

Release tags publish images to GHCR, each signed with Sigstore cosign (keyless, by the release
workflow itself) and carrying an SBOM and build provenance. Check an image before you run it:

```bash
cosign verify ghcr.io/ccastromar/aos-agentic-orchestration-system:0.4.0 \
  --certificate-identity-regexp '^https://github.com/ccastromar/aos-agentic-orchestration-system/\.github/workflows/images\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
docker buildx imagetools inspect ghcr.io/ccastromar/aos-agentic-orchestration-system:0.4.0 --format '{{ json .SBOM }}'
docker buildx imagetools inspect ghcr.io/ccastromar/aos-agentic-orchestration-system:0.4.0 --format '{{ json .Provenance }}'
```
