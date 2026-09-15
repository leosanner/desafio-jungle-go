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
| [0006](0006-money-representation.md) | Money representation | Proposed |
| [0007](0007-wager-transaction-state-machine.md) | WagerTransaction state machine | Proposed |
| [0008](0008-refund-rollback-combinations.md) | REFUND and ROLLBACK combinations | Proposed |

## Pending decisions

Decisions the challenge requires documenting that don't have an ADR yet:

- [x] Package layout and boundaries between domain, application and adapters
- [x] Go version and HTTP router
- [x] Database access library (`pgx`, `sqlc`...) and migration tool
- [x] `Money` representation (`int64` minor units) — [ADR 0006](0006-money-representation.md) (Proposed); schema mapping lands in Phase 3
- [ ] SQL transaction boundary across repositories (unit of work)
- [ ] Per-wallet concurrency strategy (pessimistic × optimistic × conditional update)
- [ ] Idempotency: key scope, hash algorithm, canonical JSON and normalizations (conflict rules exist in domain)
- [x] `WagerTransaction` state machine and transient × permanent failure classification — [ADR 0007](0007-wager-transaction-state-machine.md) (Proposed)
- [ ] Pending references: backoff, max attempts/TTL (wait vs unsuccessful reference is in ADR 0007)
- [x] `REFUND` and `ROLLBACK` combinations on the same bet — [ADR 0008](0008-refund-rollback-combinations.md) (Proposed)
- [ ] `failureCode` HTTP mapping (catalog started in [`docs/api/failure-codes.md`](../api/failure-codes.md))
- [ ] SQS inbox: visibility timeout, attempts, DLQ, invalid messages, `MessageGroupId`/`MessageDeduplicationId`
- [ ] Outbox: concurrent claim, lease, backoff, event destination and routing
- [ ] IdP, token validation and permission model (providers × internal service)
- [ ] Logs, metrics and (optional) tracing — JSON `log/slog` chosen in [ADR 0002](0002-go-version-and-http-router.md); correlation fields, metrics and tracing still open
- [x] Fx lifecycle and shutdown strategy
- [x] Test strategy: build tags, containers, multiple instances and failure injection
