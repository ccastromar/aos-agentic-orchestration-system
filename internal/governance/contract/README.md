# Chestertown governance functions · contract v1

Chestertown authorizes people and machines by **governance functions**: a small, fixed
vocabulary that is the same for every organization, whatever its size or sector. An
organization only decides who holds each function (a mapping from its identity provider's
groups or roles) and, within a legal floor, which rules apply. AOS, Prudence and Ernest each
know what every function allows in that app.

This directory is the contract the three apps implement:

| File | What it is |
| --- | --- |
| `README.md` | This specification (normative) |
| `governance-model.schema.json` | JSON Schema of a governance model (syntax only; the rules below add the semantics) |
| `default-model.yaml` | The model that reproduces the suite's behaviour before functions existed |
| `vectors/resolution.json` | Claims + model + app → the functions that app must grant |
| `vectors/validation.json` | Models that must be accepted or refused, with the error code |
| `reference/` | A small Python implementation that checks the vectors (`check_vectors.py`) |

An app conforms when it loads every model the vectors name, refuses exactly the invalid ones
with the same error code, and resolves every case to exactly the expected functions.

## 1. The three layers

1. **Functions (fixed by Chestertown).** The table in section 2. Adding or changing one is a new
   contract version, never an organization's setting.
2. **Rules (set by the organization, with a floor).** How many approvals each risk tier needs,
   which functions one identity may not combine on the same system or task, and which
   accountabilities a system needs before review. An organization may make them stricter; a
   model below the floor is refused.
3. **Mapping (set by the organization).** Which IdP group or role grants which functions. This is
   the only layer that normally differs between organizations.

All three live in one file, a governance model (section 4).

## 2. Functions v1

| Function | Kind | Meaning | Apps |
| --- | --- | --- | --- |
| `system.declare` | person | Register and document an AI system, its controls, cases, impact and evidence | Prudence |
| `system.approve` | person | Approve, reject or renew an AI system; issue attestations | Prudence |
| `policy.author` | person | Draft, edit, validate and simulate policies | Prudence |
| `policy.approve` | person | Approve a validated policy draft | Prudence |
| `oversight` | person | Human oversight: approve human steps, monitor, suspend | AOS, Prudence |
| `incident.handle` | person | Register and manage AI incidents | Prudence |
| `ai.use:<domain>` | person | Use approved AI systems in a business domain (`ai.use:finance`, `ai.use:*`) | AOS |
| `evidence.write` | person | Submit evidence (models, inferences, simulated events) | Ernest |
| `audit` | person | Read everything, change nothing | AOS, Prudence, Ernest |
| `platform.admin` | person | Operate the suite: publish policies, anchors, keys, tokens, legal holds | AOS, Prudence, Ernest |
| `runtime.decide` | machine | Ask for a decision before acting (PDP, AuthZEN, change assessments) | Prudence |
| `runtime.report` | machine | Report an execution | Prudence |

Every person function an identity holds in an app also lets it **read** that app, as every role
does today. Per-system accountabilities (`OWNER`, `SUPERVISOR`, `COMPLIANCE`, `DPO`, `SECURITY`)
are not functions: they say who answers for one system, and Prudence assigns them to people.

### What each function allows today

The routes each function will guard when the apps authorize by function (phase 3). Until then
the apps keep their current roles; the default model (section 6) maps those roles to these
functions.

| App | Function | Routes |
| --- | --- | --- |
| Prudence | `system.declare` | `POST` registry systems, submit, compliance controls, governance cases, impact, compliance snapshots, evidence, demos |
| Prudence | `incident.handle` | `POST /api/incidents/**` |
| Prudence | `oversight` | `POST /api/oversight/**`, `/api/monitoring/**` |
| Prudence | `policy.author` | `/api/policy-admin/**` except approval and publication |
| Prudence | `policy.approve` | `POST /api/policy-admin/drafts/*/approve` |
| Prudence | `platform.admin` | `/api/policy-admin/publications/**` |
| Prudence | `system.approve` | `POST` systems approve, reject, renew; `POST /api/attestations` |
| Prudence | `runtime.decide` | `POST /api/policy/decisions`, `/access/v1/evaluation(s)`, `/api/governance/change-assessments` |
| Prudence | `runtime.report` | `POST /api/executions` |
| Prudence | `audit` | every `GET`, including `/api/policy-admin/**` |
| AOS | `ai.use:<d>` | intents whose `definitions/rbac/permissions.yaml` entry lists role `<d>`; `*` = all |
| AOS | `oversight` | approving or rejecting a human step (phase 3; today only the task owner can) |
| AOS | `platform.admin` | reserved; no AOS route needs more than `ai.use` today |
| Ernest | `evidence.write` | `POST` models, inferences, inferences/batch, demo/seed, ingestor simulations; `PATCH` model status |
| Ernest | `platform.admin` | `POST`/`DELETE` access tokens and emitter keys, `POST /api/anchors`, legal holds |
| Ernest | `audit` | every read |

## 3. Grants

A grant is a string: `function[:domain][@app]`.

- `domain` is required for `ai.use` (a domain name, lowercase letters, digits, `_` and `-`, or `*`)
  and not allowed for any other function.
- `app` is one of `aos`, `prudence`, `ernest`. Without it, the grant applies in every app where the
  function exists (section 2). With it, the function must exist in that app.

## 4. The governance model

```yaml
version: chestertown.governance/v1
rules:
  approvals:                 # per risk tier
    HIGH: {count: 2, distinct: true}
    LIMITED: {count: 1}
    MINIMAL: {count: 1}
  incompatible:              # pairs one identity may not hold on the same system, draft or task
    - [system.declare, system.approve]
  accountable:               # accountabilities a system needs before it is submitted
    HIGH: [OWNER, SUPERVISOR, COMPLIANCE]
identity:
  people: {claim: groups}    # claim holding a person's groups or roles
  machines: {claim: roles}   # claim holding a workload's permissions (scope, scp, roles…)
map:                         # claim value → grants
  ai-business-owners: [system.declare, "ai.use:operations"]
  ai-risk-compliance: [system.approve, audit]
  PDP.Decide: [runtime.decide]
```

### Validation (an app refuses to start with an invalid model)

| Rule | Error code |
| --- | --- |
| `version` is exactly `chestertown.governance/v1` | `model.version` |
| `identity.people.claim` and `identity.machines.claim` are non-empty strings | `model.identity` |
| every `map` key is a non-empty string and every value a non-empty list | `model.map` |
| every grant parses as `function[:domain][@app]` | `grant.syntax` |
| the function is one of section 2 | `grant.unknown_function` |
| a domain only on `ai.use`… | `grant.domain_not_allowed` |
| …and always on `ai.use` | `grant.domain_required` |
| the app is `aos`, `prudence` or `ernest` | `grant.unknown_app` |
| the function exists in that app | `grant.app_not_applicable` |
| one map key grants machine functions or person functions, never both | `grant.mixed_kinds` |
| `approvals` tiers are `HIGH`, `LIMITED` or `MINIMAL`, each with `count` ≥ 1 | `rules.approvals` |
| **floor:** `HIGH` needs `count` ≥ 2 and `distinct: true` | `floor.approvals.HIGH` |
| `incompatible` pairs name two different person functions (no domain, no app) | `rules.incompatible` |
| `accountable` tiers are risk tiers and roles are `OWNER`, `SUPERVISOR`, `COMPLIANCE`, `DPO`, `SECURITY` | `rules.accountable` |
| **floor:** `HIGH` accountabilities include `SUPERVISOR` (AI Act art. 14) | `floor.accountable.HIGH` |

A prohibited system can never be approved, so `PROHIBITED` is not a valid approvals tier.
Missing `approvals.HIGH` or `accountable.HIGH` is a floor error, not a default.

## 5. Resolution

Given a validated model, a token's claims and an app, the functions that app grants are:

1. Read the people values: the claim named by `identity.people.claim` (a dotted name such as
   `realm_access.roles` walks nested objects). A string is split on whitespace; an array
   contributes its string elements; anything else contributes nothing.
2. Read the machine values the same way from `identity.machines.claim`. Both claims may be the
   same one.
3. For each people value with a `map` entry, take its **person** grants; for each machine value,
   its **machine** grants. A person never gets a machine function through the people claim, and
   a machine never gets a person function through the machine claim.
4. Keep the grants for this app: those whose `@app` is this app, and those without `@app` whose
   function exists in this app.
5. The result is the set of `function` (or `ai.use:<domain>`) strings, without `@app`, sorted.

Matching is exact and case-sensitive. Values without a `map` entry are ignored. Resolution never
fails: a token that maps to nothing gets no functions, and the app answers 403.

## 6. The default model

`default-model.yaml` reproduces the suite as it is deployed today (the dev realm's roles in the
`roles` claim and Keycloak's `scope` claim for workloads), so phase 2 changes no behaviour. Its
rules are the ones the software already enforces: two distinct approvers for a high-risk system,
a `SUPERVISOR` before a high-risk system is submitted, and a policy draft's author never
approving it. It deliberately does **not** add the rules the software lacks today (declaring
and approving the same system; approving one's own human step); organization templates add them.

One difference from today: `audit` in Prudence also reads `/api/policy-admin/**`, which only
authors, reviewers and admins can read today. An auditor should see the policies.

## 7. Test vectors

- `vectors/resolution.json`: named `models` (a `file` relative to `vectors/`, or `inline`) and
  `cases`, each with `model`, `app`, `claims` and the sorted `expect`ed functions.
- `vectors/validation.json`: a valid `base` model and `cases`. A case either names a `model`
  or applies a `patch` to `base` as a JSON Merge Patch (RFC 7386: objects merge, `null` removes
  a key, anything else replaces). It expects `valid: true` or an `error` code. Each invalid case
  breaks exactly one rule, so the error code does not depend on the order of the checks.

```bash
python -m pip install pyyaml
python contract/governance/reference/check_vectors.py
```

Prudence's CI runs it on every push. Each app adds the same vectors to its own tests in phase 2.

## 8. Decisions

Approved with the vectors on 2026-10-25; changing one is a change to this contract.

1. **Publishing policies is `platform.admin`**, not `policy.approve`: today publication is an
   admin action, separate from approving a draft.
2. **Prudence demos are `system.declare`**: today authors run them.
3. **Changing an Ernest model's status is `evidence.write`** for now. It bypasses Prudence's
   four-eyes review, so it may belong to `platform.admin` or go away.
4. **`ai.use` domains are AOS's RBAC role names** (`finance`, `support`, `operator`…), so phase 2
   needs no change to `permissions.yaml`.
5. **`audit` reads policies in Prudence** (section 6).
