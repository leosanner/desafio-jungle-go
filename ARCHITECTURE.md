# Architecture

> Deliverable document required by the challenge (`init.md` §15). Each section summarizes the decision
> and links to the corresponding ADR in [`docs/adr/`](docs/adr/). Sections marked _TBD_ have no decision yet.

## Overview

Phase 1 bootstrap of a Go + Uber Fx wagering service: hexagonal layout, stdlib HTTP health checks,
PostgreSQL via `pgx`, golang-migrate, and LocalStack SQS queues provisioned in Compose. There is no
domain model, wagering API, authentication on business routes, or background workers yet.

- Module: `github.com/leosanner/desafio-jungle-go` ([ADR 0001](docs/adr/0001-package-layout-and-layer-boundaries.md))
- Go 1.25, `net/http` ServeMux, `log/slog` JSON ([ADR 0002](docs/adr/0002-go-version-and-http-router.md))

## Package layout

Hexagonal / ports-and-adapters. Dependencies point inward. Fx is imported only from composition
(`internal/composition`) and `cmd/wagering`. Domain must not import Fx, `net/http`, AWS, `pgx` or
`database/sql`. Use cases must not import Fx.

```
cmd/wagering/
internal/config/
internal/domain/
internal/app/
internal/adapter/http
internal/adapter/postgres
internal/adapter/sqs
internal/composition/
migrations/
```

Details: [ADR 0001](docs/adr/0001-package-layout-and-layer-boundaries.md).

## Money (`Money`)

Representation, limits, database mapping, normalization. — _TBD_

## Persistence and transactions

**Access (accepted):** `pgx/v5` pool and explicit SQL. No GORM. `database/sql` is not the primary
API. **sqlc** is not used in Phase 1 (optional later, not chosen).

**Migrations (accepted):** golang-migrate v4, SQL files in `migrations/` with up and down.
Baseline `000001_bootstrap` creates schema `wagering` only. The app runs migrate Up on start from
`MIGRATIONS_PATH`. Rollback is a CLI operation, not automatic on shutdown.

[ADR 0003](docs/adr/0003-database-access-and-migrations.md).

**Still TBD:** `Money` mapping; SQL transaction boundary across repositories (unit of work).

## Concurrency and locking

Per-wallet strategy, lost update prevention, behavior with multiple instances. — _TBD_

## Idempotency

Key scope, canonical hash (algorithm, fields, normalizations), replay and conflicts. — _TBD_

## `WagerTransaction` state machine

Transitions, transient × permanent failures, `PENDING` resumption. — _TBD_

## Pending references

Backoff, limit/TTL, reference still pending or unsuccessful. — _TBD_

## Reversals

`REFUND`, `ROLLBACK` and their combinations; reversal without funds. — _TBD_

## Inbox and SQS consumer

**Provisioned queues (Phase 1):** `wager-transactions.fifo` and `wager-transactions-dlq.fifo` with
redrive. Message contracts, visibility timeout, attempts, invalid messages, and
`MessageGroupId`/`MessageDeduplicationId` — _TBD_

See [`docs/events/README.md`](docs/events/README.md).

## Outbox and publishing

Concurrent claim, backoff, recovery, destination and routing. — _TBD_

## Authentication and authorization

IdP, token validation, permission model, broker access. — _TBD_

Keycloak is started by Docker Compose in Phase 1 but is **not** used by the application yet. Health
endpoints are public and unauthenticated ([`docs/api/health.md`](docs/api/health.md)).

## Uber Fx usage and shutdown

- One `fx.Module` per area; plain constructors; `fx.Invoke` only to run the HTTP server (and later workers).
- Config validated on start; start/stop timeouts from `FX_START_TIMEOUT` / `FX_STOP_TIMEOUT`.
- HTTP: Listen synchronously, Serve asynchronously, `Shutdown` on stop.
- Readiness fails as soon as shutdown starts.
- `OnStop` runs in reverse order; hooks live on the resource constructor.
- Workers (later): cancel fetch, finish or release in-flight work, observable `done` channel. None in Phase 1.

[ADR 0004](docs/adr/0004-fx-lifecycle-and-shutdown.md).

## Observability

**Logs (accepted):** `log/slog` with a JSON handler; level from `LOG_LEVEL`
([ADR 0002](docs/adr/0002-go-version-and-http-router.md)). Do not log credentials, secrets or full
financial payloads.

**Health:** public `GET /health/live` and `GET /health/ready` (PostgreSQL + SQS; ready fails during
shutdown). [`docs/api/health.md`](docs/api/health.md).

Correlation IDs on every business log line, metrics and tracing — _TBD_

## Limitations, interpretations and unfinished work

Phase 1 is bootstrap only:

- No domain types, wagering HTTP API, idempotency, ledger or financial schema.
- No authentication on any route (only health exists; health is intentionally public).
- No SQS consumer, outbox publisher or pending-reference worker.
- No `Money` type; STK-05 remains open for mapping and unit of work.
- Integration tests with real containers, multi-instance runs and failure injection are specified
  ([ADR 0005](docs/adr/0005-test-strategy-initial.md)) but not delivered yet.
- Keycloak is provisioned for later phases and unused by the app.

Interpretations adopted for bootstrap: migrate Up on process start; rollback is operator-driven via
CLI; default `go test ./...` never requires Docker.
