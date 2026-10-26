# AOS audit events · contract v1

How AOS's audit journal reaches Ernest: the record AOS publishes for each journal entry, and
what Ernest's event-ingestor must make of it. AOS is the producer and owns this contract; Ernest
consumes it.

| File | What it is |
| --- | --- |
| `README.md` | This contract (normative) |
| `vectors.json` | Journal records → the exact Kafka record and HTTP request → what Ernest derives |
| `sync.sh` | Copies this directory into Ernest's event-ingestor with a `SOURCE` of SHA-256 sums |

| Side | Test |
| --- | --- |
| Producer: AOS (`internal/audit`, `JournalRecord`) | `contract_test.go`: every case gives exactly its `record`, and no `forbidden` string leaves AOS |
| Consumer: Ernest's event-ingestor (`kafkasource`, `ingest`, `events.Normalize`) | `kafkasource/contract_test.go`: every `record` is read as its `ernest` says; `ingest/aos_contract_test.go`: every `http` request is accepted (signature) and read as its `ernestHttp` says |

## 1. The record

When `AOS_EVIDENCE_KAFKA_BROKERS` is set, AOS ships the journal as one Kafka record per entry,
after the entry is durable and hash-chained on disk:

- **Key**: the trace id, so one trace's events stay in order on one partition; the entry's hash
  when it has no trace.
- **Headers**: `source` = `aos`, `eventType` = `aos.audit.event.<phase>`, `sourceEventId` =
  `<trace>:<phase>:<step>:<hash>` (the hash alone without a trace).
- **Value**: JSON with `source`, `eventType`, `sourceEventId`, `occurredAt` (UTC, milliseconds),
  `modelId` and `metadata` (identifiers, status and digests). A governed pipeline's events carry
  `modelId` = its Prudence AI system and `aiSystem` = `{id, systemOfRecord: "prudence"}`; others
  go under `modelId` = `aos-agentic-orchestration-system`.

Over HTTP (`ERNEST_INGESTOR_URL`, `POST /events` on the ingestor) the same body shape travels, with
the headers as `X-Ernest-Event-Source`, `X-Ernest-Event-Type` and `X-Ernest-Source-Event-Id`.
There is no journal record there, so the event is identified by the SHA-256 of its own JSON (in
UTC) instead of the record hash, and `journalSequence` and `transport` are absent. With
`ERNEST_INGESTOR_HMAC_SECRET` set, AOS signs the body: `X-Ernest-Provider-Timestamp` and
`X-Ernest-Provider-Signature` = `sha256=` HMAC-SHA256 of `<timestamp>.<body>`. The vectors pin
each request (`http`, signed with a test secret at a fixed time) and what Ernest makes of it
(`ernestHttp`, verified as `provider_hmac`).

## 2. What never leaves AOS

User ids, roles, parameters, tool names, error messages and details stay in the local journal.
The vectors fill them with `SECRET…` values, and AOS's test fails if one appears in a record.

## 3. What Ernest makes of it

- The event is deduplicated on (`source`, `eventType`, `sourceEventId`): a replay after a crash
  records nothing new. Its evidence event id is `<eventType>:<sourceEventId>`.
- It is recorded under `modelId`, with the name Prudence gave the system (AOS sends none).
- `metadata.stage` = `execution` with `metadata.aiSystemId` marks the one execution of a governed
  pipeline (`pipeline.started`): what Ernest counts per system (`GET /api/stats/executions`) and
  Prudence compares with the runtime's own reports to detect drift. Every other event is
  `lifecycle`.
- `occurredAt` is the time AOS recorded, not the time Ernest received it.

## 4. Changing it

Change `vectors.json` with the code (`go test ./internal/audit -run TestAuditEventsContract
-update-contract` rewrites the records), commit, run `sync.sh`, and make Ernest's test pass. A
change to a field Ernest reads, or to how the ids are formed, is a new version
(`aos.audit-events/v2`, a new `schemaVersion`), published alongside v1 for a suite release.
