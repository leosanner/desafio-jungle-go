# Architecture

> Deliverable document required by the challenge (`init.md` §15). Each section summarizes the decision
> and links to the corresponding ADR in [`docs/adr/`](docs/adr/). Sections marked _TBD_ have no decision yet.

## Overview

Phase 5 of a Go + Uber Fx wagering service: hexagonal layout, stdlib HTTP, PostgreSQL via `pgx`,
golang-migrate, LocalStack SQS queues, a **pure domain model**, **financial persistence**, **OIDC
authentication**, and **HTTP use cases** (wallet opening, wagering operations with persistent
idempotency, ledger reads, reconciliation). Outbox **rows** are written in the same commit; there is
still no publisher worker, SQS consumer, or pending-reference worker.

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
internal/adapter/auth
internal/composition/
migrations/
```

Details: [ADR 0001](docs/adr/0001-package-layout-and-layer-boundaries.md).

## Money (`Money`)

**Proposed:** [ADR 0006](docs/adr/0006-money-representation.md). Immutable value object: `int64`
minor units (cents) + 3-letter uppercase ISO 4217 code. Scale always 2. Zero value is invalid.
External parse rejects anything that is not a non-negative `digits.dd` string (no silent rounding).
JSON `amount` is a string; numeric JSON is rejected so nothing goes through float. Overflow on
parse/add/subtract/negate is an error. Negative amounts exist only for internal differences.

**Persistence (Phase 3):** `BIGINT` minor units + currency column
([ADR 0006](docs/adr/0006-money-representation.md)).

## Persistence and transactions

**Access (accepted):** `pgx/v5` pool and explicit SQL. No GORM. `database/sql` is not the primary
API. **sqlc** is not used in Phase 1 (optional later, not chosen).

**Migrations (accepted):** golang-migrate v4, SQL files in `migrations/` with up and down.
Baseline `000001_bootstrap` creates schema `wagering`. `000002_financial_schema` adds `wallets`,
`wager_transactions`, `wallet_ledger_entries` (BIGINT minor units, uniqueness/check constraints,
append-only ledger trigger). `000003_outbox` adds `outbox_events`. The app runs migrate Up on start
from `MIGRATIONS_PATH`. Rollback is a CLI operation, not automatic on shutdown.

[ADR 0003](docs/adr/0003-database-access-and-migrations.md).

**Money mapping (proposed):** PostgreSQL `BIGINT` cents plus a currency column, matching domain
`int64` minor units ([ADR 0006](docs/adr/0006-money-representation.md)).

**Unit of work (proposed):** [ADR 0009](docs/adr/0009-sql-unit-of-work.md). Application ports
`UnitOfWork` and repositories; `Within(ctx, func(ctx, Repositories) error)` opens one `pgx.Tx`,
injects tx-scoped repos, commits on `nil`, rolls back on error or panic. Repositories never begin
their own transactions. `Repositories.Outbox` joins the same commit ([ADR 0014](docs/adr/0014-outbox-persistence.md)).
Inbox remains later. Unique/check/lock errors map by `SQLSTATE` to classifiable errors
(`errors.Is`), never by message string. Financial reads that decide a debit or credit run inside
the same `Within`.

## Concurrency and locking

**Proposed:** [ADR 0010](docs/adr/0010-per-wallet-concurrency.md). Pessimistic
`SELECT ... FOR UPDATE` on the wallet row inside the unit-of-work transaction, in-memory mutate,
then `UPDATE ... WHERE id = $1 AND version = $2`. Zero rows on that UPDATE is an invariant
failure, not a silent retry. No global mutex, no session advisory lock on a constant, no table
lock, no in-memory lock map. Independent wallets proceed in parallel; N instances share PostgreSQL
row locks. `CHECK (balance_minor >= 0)` is a last line of defense. Optimistic-only version retries
are rejected as the primary strategy.

## Idempotency

**Proposed:** [ADR 0012](docs/adr/0012-idempotency-canonical-hash.md). Key scope is
`(provider_id, idempotency_key)` for external rows. Hash is SHA-256 hex of key-sorted JSON of the
business fields (money as canonical `"25.00"` + ISO currency). The idempotency key and transport
metadata are excluded. Same key + hash → replay with the original `ResultBalance`. Same key +
different hash → `IDEMPOTENCY_PAYLOAD_CONFLICT`. `(providerId, externalTransactionId)` under another
key → `DUPLICATE_EXTERNAL_TRANSACTION`. The server never replaces a received `Idempotency-Key`.
Internal IDs are UUID v7 generated in `internal/app` (stdlib `crypto/rand`, no extra dependency).

## `WagerTransaction` state machine

**Proposed:** [ADR 0007](docs/adr/0007-wager-transaction-state-machine.md). External operations start
`PENDING`. Allowed moves: `PROCESSED`, `REJECTED`, `FAILED`, `PENDING_REFERENCE`. Terminal states
do not transition. Origin `INTERNAL` (`OPENING`) vs `EXTERNAL`. Failure classes: Validation,
Rejection, Wait, Transient, Permanent.

Durable `PENDING` resumption is a persistence concern (later).

## Pending references

Wait vs unsuccessful reference is in [ADR 0007](docs/adr/0007-wager-transaction-state-machine.md):
missing or still-pending reference → `PENDING_REFERENCE`; `REJECTED`/`FAILED` reference →
`REFERENCE_UNSUCCESSFUL`. Backoff, max attempts/TTL and `REFERENCE_NOT_FOUND` — _TBD_ (Phase 8).

## Reversals

**Proposed:** [ADR 0008](docs/adr/0008-refund-rollback-combinations.md). `REFUND` only of a processed
`BET`. `ROLLBACK` of processed `BET`/`WIN`/`REFUND`. At most one successful reversal of each kind
per reference. `REFUND(BET)` and `ROLLBACK(BET)` are mutually exclusive. Shortage on a reversal
uses `INSUFFICIENT_FUNDS_REVERSAL`, not `INSUFFICIENT_FUNDS`.

## Inbox and SQS consumer

**Provisioned queues (Phase 1):** `wager-transactions.fifo` and `wager-transactions-dlq.fifo` with
redrive. Message contracts, visibility timeout, attempts, invalid messages, and
`MessageGroupId`/`MessageDeduplicationId` — _TBD_

See [`docs/events/README.md`](docs/events/README.md).

## Outbox and publishing

Domain event types and `WalletBalanceChanged` payload: [`docs/events/outbox-events.md`](docs/events/outbox-events.md).

**Proposed:** [ADR 0014](docs/adr/0014-outbox-persistence.md). `wagering.outbox_events` is written in
the same `Within` as wallet, transaction and ledger. Rows stay unpublished (`published_at` NULL).
Concurrent claim, backoff, recovery, destination and routing. — _TBD_ (Phase 6)

## Authentication and authorization

**Proposed:** [ADR 0011](docs/adr/0011-oidc-keycloak-auth.md). Keycloak 26 is the external OIDC
IdP. Realm `wagering` is imported from `docker/keycloak/realm-wagering.json`. Services use
`client_credentials`. The API validates Bearer JWTs with `github.com/coreos/go-oidc/v3`
(JWKS, `iss`, `aud`, `exp`, RS256). `providerId` is the token `azp`, except for
`OIDC_INTERNAL_CLIENT` (`wagering-internal`), which is the wallet service and has no provider
id. Path/body `providerId` must match that identity. Wallet routes are internal-only; provider
paths are isolated per `azp`. Health endpoints stay public ([`docs/api/health.md`](docs/api/health.md)).
Contract: [`docs/api/auth.md`](docs/api/auth.md).

SQS is gated by AWS credentials (LocalStack dummies in Compose). There is no consumer yet.

## Uber Fx usage and shutdown

- One `fx.Module` per area; plain constructors; `fx.Invoke` only to run the HTTP server (and later workers).
- Config validated on start; start/stop timeouts from `FX_START_TIMEOUT` / `FX_STOP_TIMEOUT`.
- Check dependencies at startup (PostgreSQL ping, SQS queue exists, JWKS reachable) and fail fast
  with a clear error.
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

HTTP statuses: [ADR 0013](docs/adr/0013-http-status-mapping.md), [`docs/api/status.md`](docs/api/status.md).

Reconciliation divergences are logged (`walletId`, entry count, no full payload) and counted via
`app.Metrics`. Broader metrics and tracing — _TBD_

## Limitations, interpretations and unfinished work

Phase 5 adds HTTP use cases (`internal/app`, protected handlers). Still unfinished:

- No SQS consumer, outbox publisher, or pending-reference worker. `PENDING_REFERENCE` is persisted
  and returned as `202` until Phase 8 resumes it.
- ADRs 0006–0014 are **Proposed** until confirmed.
- STK-05 is documented and implemented: `pgx/v5` ([ADR 0003](docs/adr/0003-database-access-and-migrations.md)),
  `BIGINT` minor units ([ADR 0006](docs/adr/0006-money-representation.md), migration `000002`),
  unit of work ([ADR 0009](docs/adr/0009-sql-unit-of-work.md)), outbox insert ([ADR 0014](docs/adr/0014-outbox-persistence.md),
  migration `000003`).
- Inbox persistence remains later (same `Repositories` / `Within` pattern).
- TST-04 is covered by `-tags=integration` tests in `internal/adapter/postgres`.
  HTTP use cases: `TestHTTPPhase5UseCases`, `TestHTTPConcurrentSameBet`, `TestHTTPTwoBetsOnHundred`,
  `TestHTTPDistinctWalletsParallel` (`POSTGRES_DSN`). TST-07 needs Keycloak and `OIDC_ISSUER`.
  Multi-instance runs and failure injection remain later
  ([ADR 0005](docs/adr/0005-test-strategy-initial.md)).
- Health endpoints stay public.

Interpretations: migrate Up on process start; rollback is operator-driven via CLI; default
`go test ./...` never requires Docker; `"25"` / `"25.0"` are rejected as money input (no
normalization); a BET is refunded at most once in its lifetime even if that refund is later
rolled back; HTTP `providerId` is always the token `azp`, never the untrusted body/path alone;
`GET /wagering/transactions/{id}` for another provider is `404` (no existence leak); provider-path
mismatch stays `403`.
