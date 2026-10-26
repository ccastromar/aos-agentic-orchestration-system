# Prudence Decision API · contract v1

How a runtime asks Prudence whether a governed step may run, and reports what it ran. The
API itself is specified in [`docs/api/decision-api-v1.yaml`](../../docs/api/decision-api-v1.yaml);
this directory adds what a schema cannot say: when a gate lets a step run, what a runtime does
with each answer to a report, and test vectors that both sides run.

| File | What it is |
| --- | --- |
| `README.md` | This contract (normative) |
| `vectors/gate.json` | HTTP answers to `POST /api/policy/decisions` → whether the step runs |
| `vectors/report.json` | Statuses of `POST /api/executions` → recorded, retry or drop |
| `vectors/provider.json` | What Prudence must answer for each outcome and request |
| `reference/` | A Python gate and schema check that agree with every vector (`check_vectors.py`) |

Who runs the vectors:

| Side | Implementation | Test |
| --- | --- | --- |
| Provider | Prudence (`PolicyController`, `ExecutionReportController`) | `DecisionApiContractTest` |
| Gate | AOS (`internal/governance`) | `decision_contract_test.go`, on a copy made by `sync.sh` |
| Gate | `integrations/prudence-core` (Pydantic AI, LangGraph, ADK, OpenAI Agents, Agent Framework) | `tests/test_decision_contract.py` |

An implementation conforms when it agrees with every case. A new gate (another orchestrator)
should run `vectors/gate.json` and `vectors/report.json` before it calls a real Prudence.

## 1. Versions

`prudence.decision-api/v1`. v1 only grows: new optional response fields, new vectors that
spell out behaviour v1 already had. A gate must ignore fields it does not know. Anything that
would change what an existing vector expects is v2, served alongside v1.

A gate may send `correlationId`, its own id for the business flow (AOS sends its task id): Prudence
records it with the decision and it travels with the decision's evidence. It is optional, never a
trace id (trace context travels as W3C `traceparent`), and only a plain token
(`[A-Za-z0-9._:-]`, at most 128); anything else is a `400`.

## 2. When a step runs (fail closed)

A gate lets the step run **only** when all of these hold:

1. the status is `200`;
2. the body is a JSON object;
3. `decision` is exactly the string `ALLOW`;
4. `decisionId` is a non-blank string (the gate keeps it for the execution report).

Everything else stops the step: another status, a body that does not parse, a missing or blank
`decisionId`, an unknown decision, a timeout, a refused connection, a token that cannot be
obtained. The status and the decision come in pairs, and a gate checks both:

| Status | `decision` | The gate |
| --- | --- | --- |
| 200 | `ALLOW` | runs the step |
| 202 | `APPROVAL_REQUIRED` | stops; reports the `approver` role |
| 403 | `DENY` | stops; reports the `reason` |
| 403 | (no body: the token lacks `pdp.decide`) | stops |
| 503 | `ERROR` (Prudence could not decide) | stops |

A body with `ALLOW` under any status but 200 is not an `ALLOW`.

## 3. Execution reports

After the step started, the runtime reports it (`POST /api/executions`) citing the
`decisionId`. Reporting never blocks or fails the step. By status:

| Status | Meaning | The runtime |
| --- | --- | --- |
| 201, 200 | recorded (200: the same report was already recorded) | done |
| 400, 401, 403, 409 | a bad report, a bad token, no `execution.report`, other facts for the same execution | drops it and logs it: retrying cannot help |
| 5xx, no answer | Prudence could not take it now | retries with backoff, then logs it |

Reports are idempotent on (`runtime`, `executionId`), so retrying is safe.

## 4. What Prudence answers

`vectors/provider.json` lists, for each outcome of the PDP, the status and decision Prudence
returns, the requests it refuses with 400 before deciding anything, and a sequence of execution
reports with their statuses. Every body matches its schema in `decision-api-v1.yaml`, and
carries no field the schema does not declare: a new field is added to the schema first.

## 5. Copies

`sync.sh` copies this directory and the OpenAPI file into AOS with a `SOURCE` file (this
repository's commit and a SHA-256 per file). AOS's test checks the sums, so a copy edited by
hand fails CI: change the contract here, commit, and sync again.
