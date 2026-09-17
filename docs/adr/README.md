# Architecture Decision Records

Each relevant technical decision is recorded in an `NNNN-title.md` file based on
[`0000-template.md`](0000-template.md). Use the `adr` skill to create new records.

Accepted ADRs are not rewritten: a change of direction creates a new ADR that supersedes the old one.

## Index

| # | Title | Status |
| --- | --- | --- |
| [0001](0001-package-layout-and-layer-boundaries.md) | Package layout and layer boundaries | Accepted |
| [0002](0002-go-version-and-http-router.md) | Go version, HTTP router and logging | Accepted |
| [0003](0003-database-access-and-migrations.md) | Database access and migrations | Accepted |
| [0004](0004-fx-lifecycle-and-shutdown.md) | Fx lifecycle and shutdown | Accepted |
| [0005](0005-test-strategy-initial.md) | Test strategy (initial) | Accepted |
| [0006](0006-money-representation.md) | Money representation | Accepted |
| [0007](0007-wager-transaction-state-machine.md) | WagerTransaction state machine | Accepted |
| [0008](0008-refund-rollback-combinations.md) | REFUND and ROLLBACK combinations | Accepted |
| [0009](0009-sql-unit-of-work.md) | SQL unit of work / transaction boundary | Accepted |
| [0010](0010-per-wallet-concurrency.md) | Per-wallet concurrency | Accepted |
| [0011](0011-oidc-keycloak-auth.md) | OIDC authentication with Keycloak | Accepted |
| [0012](0012-idempotency-canonical-hash.md) | Canonical idempotency hash | Accepted |
| [0013](0013-http-status-mapping.md) | HTTP status and error-body mapping | Accepted |
| [0014](0014-outbox-persistence.md) | Outbox persistence without a publisher | Accepted |
| [0015](0015-outbox-destination-and-routing.md) | Outbox destination and routing (SQS FIFO) | Accepted |
| [0016](0016-outbox-claim-and-backoff.md) | Outbox claim, lease, backoff and recovery | Accepted |
| [0017](0017-inbox-persistence.md) | Inbox persistence in the financial unit of work | Accepted |
| [0018](0018-sqs-inbound-consume.md) | SQS inbound consume, retry, DLQ and shutdown | Accepted |
| [0019](0019-pending-reference-resume.md) | Pending-reference worker, backoff, TTL and PENDING resume | Accepted |
| [0020](0020-multi-instance-and-failure-injection.md) | Multi-instance verification and failure injection | Accepted |
| [0021](0021-observability-logs-and-metrics.md) | Observability: correlation logs and Prometheus metrics | Accepted |

## Recorded decisions

All challenge-required technical choices have an accepted ADR:

- [x] Package layout and boundaries between domain, application and adapters
- [x] Go version and HTTP router
- [x] Database access library (`pgx`) and migration tool
- [x] `Money` representation (`int64` minor units) — [ADR 0006](0006-money-representation.md)
- [x] SQL transaction boundary across repositories (unit of work) — [ADR 0009](0009-sql-unit-of-work.md)
- [x] Per-wallet concurrency strategy (pessimistic row lock + versioned update) — [ADR 0010](0010-per-wallet-concurrency.md)
- [x] Idempotency: key scope, hash algorithm, canonical JSON and normalizations — [ADR 0012](0012-idempotency-canonical-hash.md)
- [x] `WagerTransaction` state machine and transient × permanent failure classification — [ADR 0007](0007-wager-transaction-state-machine.md)
- [x] Pending references: backoff, max attempts/TTL — [ADR 0019](0019-pending-reference-resume.md); wait vs unsuccessful reference is in ADR 0007
- [x] `REFUND` and `ROLLBACK` combinations on the same bet — [ADR 0008](0008-refund-rollback-combinations.md)
- [x] `failureCode` HTTP mapping — [ADR 0013](0013-http-status-mapping.md); [`docs/api/status.md`](../api/status.md)
- [x] SQS inbox: visibility timeout, attempts, DLQ, invalid messages, `MessageGroupId`/`MessageDeduplicationId` — [ADR 0017](0017-inbox-persistence.md), [ADR 0018](0018-sqs-inbound-consume.md)
- [x] Outbox: concurrent claim, lease, backoff, event destination and routing — [ADR 0014](0014-outbox-persistence.md), [ADR 0015](0015-outbox-destination-and-routing.md), [ADR 0016](0016-outbox-claim-and-backoff.md)
- [x] IdP, token validation and permission model (providers × internal service) — [ADR 0011](0011-oidc-keycloak-auth.md)
- [x] Logs, metrics and (optional) tracing — JSON `log/slog` + correlation + Prometheus catalog in [ADR 0021](0021-observability-logs-and-metrics.md); tracing skipped
- [x] Fx lifecycle and shutdown strategy
- [x] Test strategy: build tags, containers, multiple instances and failure injection
