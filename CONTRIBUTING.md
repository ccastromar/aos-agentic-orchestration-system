# Contributing to AOS

Thank you for helping. AOS is the reference runtime in the Chestertown reference architecture
(AOS runs, Ernest proves, Prudence decides and oversees). Its governance gate and its audit evidence are
read by the other two apps, so the contracts below matter as much as the code.

## Before you open a pull request

Run what CI runs, from the repository root:

```bash
go test -race ./...
cd ui && npm ci && npm run lint && npm run build && npm run test:e2e
```

The console's browser tests also check WCAG 2.1 AA with axe, in light and dark themes.

## Contracts with the other apps

| Directory | What it is | Owner |
| --- | --- | --- |
| `contract/audit-events/` | The Kafka record each audit journal entry becomes; Ernest's event-ingestor runs it | AOS |
| `internal/governance/contract/` | Governance functions v1 | Prudence (synced copy) |
| `internal/governance/decision-contract/` | Prudence's Decision API: when the gate lets a pipeline run | Prudence (synced copy) |

Never edit a synced copy: tests check its SHA-256 sums. Change the contract in Prudence and run
its `sync.sh`. For `contract/audit-events/`, change the vectors with the code
(`go test ./internal/audit -run TestAuditEventsContract -update-contract`), run its `sync.sh`
and open the matching change in Ernest. A change to a field Ernest reads is a new version.

## Style

- Small pull requests, one change each, with a message that says why.
- Tests with the change. The governance gate fails closed: anything but a clear ALLOW stops.
- Only identifiers, status and digests leave AOS as evidence: never prompts, parameters, user
  ids or tool output.
- No secrets, real personal data or local paths in code, fixtures or docs.

## Conduct

Everyone taking part follows the [code of conduct](CODE_OF_CONDUCT.md); report problems to
conduct@ccmlab.cc.

## Security

Report vulnerabilities privately, as `SECURITY.md` describes, never in a public issue.

## License

By contributing you agree that your contribution is licensed under the Apache License 2.0, the
license of this repository (section 5 of the license).
