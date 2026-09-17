# Architecture

Deliverable document required by the challenge (`init.md` §15). Each section summarizes an
**accepted** decision and links to the ADR in [`docs/adr/`](docs/adr/).

## Overview

Go + Uber Fx wagering service: hexagonal layout, stdlib HTTP, PostgreSQL via `pgx`, golang-migrate,
LocalStack SQS, Keycloak OIDC, a pure domain model, financial persistence, HTTP use cases, a
concurrent outbox publisher, an SQS inbound consumer with a transactional inbox, a pending-reference
worker, multi-instance verification, and observability (JSON logs with correlation IDs plus a
Prometheus catalog on `GET /metrics`).

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
internal/adapter/metrics
internal/composition/
migrations/
```

Details: [ADR 0001](docs/adr/0001-package-layout-and-layer-boundaries.md).

## Money (`Money`)

[ADR 0006](docs/adr/0006-money-representation.md). Immutable value object: `int64` minor units
(cents) + 3-letter uppercase ISO 4217 code. Scale always 2. Zero value is invalid. External parse
rejects anything that is not a non-negative `digits.dd` string (no silent rounding). JSON `amount`
is a string; numeric JSON is rejected so nothing goes through float. Overflow on
parse/add/subtract/negate is an error. Negative amounts exist only for internal differences.

Persistence: PostgreSQL `BIGINT` minor units + currency column (`migrations/000002_financial_schema`).

## Persistence and transactions

**Access:** `pgx/v5` pool and explicit SQL. No GORM. `database/sql` is not the primary API. **sqlc**
is not used.

**Migrations:** golang-migrate v4, SQL files in `migrations/` with up and down.

| Version | What it creates |
| --- | --- |
| `000001_bootstrap` | Schema `wagering` |
| `000002_financial_schema` | `wallets`, `wager_transactions`, `wallet_ledger_entries` (BIGINT, uniqueness/check, append-only trigger) |
| `000003_outbox` | `outbox_events` |
| `000004_inbox` | `inbox_messages` |

The app runs migrate Up on start from `MIGRATIONS_PATH`. Rollback is a CLI operation, not automatic
on shutdown. [ADR 0003](docs/adr/0003-database-access-and-migrations.md).

**Unit of work:** [ADR 0009](docs/adr/0009-sql-unit-of-work.md). Application ports `UnitOfWork` and
repositories; `Within(ctx, func(ctx, Repositories) error)` opens one `pgx.Tx`, injects tx-scoped
repos, commits on `nil`, rolls back on error or panic. Repositories never begin their own
transactions. `Repositories.Outbox` and `Repositories.Inbox` join the same commit
([ADR 0014](docs/adr/0014-outbox-persistence.md), [ADR 0017](docs/adr/0017-inbox-persistence.md)).
Unique/check/lock errors map by `SQLSTATE` to classifiable errors (`errors.Is`), never by message
string. Financial reads that decide a debit or credit run inside the same `Within`.

## Concurrency and locking

[ADR 0010](docs/adr/0010-per-wallet-concurrency.md). Pessimistic `SELECT ... FOR UPDATE` on the
wallet row inside the unit-of-work transaction, in-memory mutate, then
`UPDATE ... WHERE id = $1 AND version = $2`. Zero rows on that UPDATE is an invariant failure, not
a silent retry. No global mutex, no session advisory lock on a constant, no table lock, no
in-memory lock map. Independent wallets proceed in parallel; N instances share PostgreSQL row
locks. `CHECK (balance_minor >= 0)` is a last line of defense. Optimistic-only version retries are
rejected as the primary strategy. Three independent `cmd/wagering` processes prove CON-02
([ADR 0020](docs/adr/0020-multi-instance-and-failure-injection.md)).

## Idempotency

[ADR 0012](docs/adr/0012-idempotency-canonical-hash.md). Key scope is `(provider_id, idempotency_key)`
for external rows. Hash is SHA-256 hex of key-sorted JSON of the business fields (money as canonical
`"25.00"` + ISO currency). The idempotency key and transport metadata are excluded. Same key + hash
→ replay with the original `ResultBalance`. Same key + different hash → `IDEMPOTENCY_PAYLOAD_CONFLICT`.
`(providerId, externalTransactionId)` under another key → `DUPLICATE_EXTERNAL_TRANSACTION`. The
server never replaces a received `Idempotency-Key`. Internal IDs are UUID v7 generated in
`internal/app` (stdlib `crypto/rand`, no extra dependency).

## `WagerTransaction` state machine

[ADR 0007](docs/adr/0007-wager-transaction-state-machine.md). External operations start `PENDING`.
Allowed moves: `PROCESSED`, `REJECTED`, `FAILED`, `PENDING_REFERENCE`. Terminal states do not
transition. Origin `INTERNAL` (`OPENING`) vs `EXTERNAL`. Failure classes: Validation, Rejection,
Wait, Transient, Permanent.

Durable `PENDING` resumption is the pending-reference worker
([ADR 0019](docs/adr/0019-pending-reference-resume.md)).

## Pending references

Wait vs unsuccessful reference is in [ADR 0007](docs/adr/0007-wager-transaction-state-machine.md):
missing or still-pending reference → `PENDING_REFERENCE`; `REJECTED`/`FAILED` reference →
`REFERENCE_UNSUCCESSFUL`.

[ADR 0019](docs/adr/0019-pending-reference-resume.md). A worker claims due `PENDING` /
`PENDING_REFERENCE` rows with `SELECT … FOR UPDATE SKIP LOCKED`, leases via `next_attempt_at`, and
re-runs Apply with wallet-first locking. Exponential backoff on still-waiting. Max attempts or TTL
→ `REJECTED` / `REFERENCE_NOT_FOUND`. First wait emits `WagerTransactionPendingReference`; retries
do not. Contract: [`docs/events/pending-references.md`](docs/events/pending-references.md).
Operator procedure: [`docs/runbooks/pending-references.md`](docs/runbooks/pending-references.md).

## Reversals

[ADR 0008](docs/adr/0008-refund-rollback-combinations.md). `REFUND` only of a processed `BET`.
`ROLLBACK` of processed `BET`/`WIN`/`REFUND`. At most one successful reversal of each kind per
reference. `REFUND(BET)` and `ROLLBACK(BET)` are mutually exclusive. Shortage on a reversal uses
`INSUFFICIENT_FUNDS_REVERSAL`, not `INSUFFICIENT_FUNDS`.

## Inbox and SQS consumer

[ADR 0017](docs/adr/0017-inbox-persistence.md). `wagering.inbox_messages` is unique on
`(consumer_name, message_id)`. Hash is SHA-256 of the raw SQS body. Completed rows are inserted in
the same `Within` as domain, ledger and outbox. HTTP `Submit` does not write inbox rows.

[ADR 0018](docs/adr/0018-sqs-inbound-consume.md). Worker long-polls `wager-transactions.fifo`.
Envelope `messageId` is inbox identity. Delete after commit. Transient failures change visibility
with exponential backoff; permanent errors go to `wager-transactions-dlq.fifo`. Producers set
`MessageGroupId=walletId` and `MessageDeduplicationId=messageId`. `data.providerId` is queue-gated,
not a JWT. SIGTERM cancels receive and finishes or releases in-flight visibility.

Contract: [`docs/events/inbound.md`](docs/events/inbound.md).

## Outbox and publishing

Domain event types and `WalletBalanceChanged` payload:
[`docs/events/outbox-events.md`](docs/events/outbox-events.md).

[ADR 0014](docs/adr/0014-outbox-persistence.md). `wagering.outbox_events` is written in the same
`Within` as wallet, transaction and ledger.

[ADR 0015](docs/adr/0015-outbox-destination-and-routing.md). Publisher sends the OBX-06 envelope to
FIFO `wager-events.fifo` (`SQS_EVENTS_QUEUE_NAME`). `MessageGroupId` = `aggregateId`;
`MessageDeduplicationId` = `eventId`. At-least-once; consumers dedup by `eventId`.

[ADR 0016](docs/adr/0016-outbox-claim-and-backoff.md). Separate worker claims due rows with
`SELECT … FOR UPDATE SKIP LOCKED`, leases via `next_attempt_at`, publishes outside the lock, then
sets `published_at`. Exponential backoff on send failure. `eventId` is never rewritten. Injectable
`AfterPublish` hook is tests-only (publish-before-ack). N publishers share `SKIP LOCKED`; three OS
processes are covered by [ADR 0020](docs/adr/0020-multi-instance-and-failure-injection.md).

## Authentication and authorization

[ADR 0011](docs/adr/0011-oidc-keycloak-auth.md). Keycloak 26 is the external OIDC IdP. Realm
`wagering` is imported from `docker/keycloak/realm-wagering.json`. Services use `client_credentials`.
The API validates Bearer JWTs with `github.com/coreos/go-oidc/v3` (JWKS, `iss`, `aud`, `exp`,
RS256). `providerId` is the token `azp`, except for `OIDC_INTERNAL_CLIENT` (`wagering-internal`),
which is the wallet service and has no provider id. Path/body `providerId` must match that
identity. Wallet routes are internal-only; provider paths are isolated per `azp`. Health and
`GET /metrics` stay public ([`docs/api/health.md`](docs/api/health.md),
[`docs/api/metrics.md`](docs/api/metrics.md)). Contract: [`docs/api/auth.md`](docs/api/auth.md).

SQS is gated by AWS credentials (LocalStack dummies in Compose). The inbound consumer treats
`data.providerId` as the financial provider, not as an HTTP JWT identity
([ADR 0018](docs/adr/0018-sqs-inbound-consume.md)).

## Uber Fx usage and shutdown

- One `fx.Module` per area; plain constructors; `fx.Invoke` only to run the HTTP server and workers.
- Config validated on start; start/stop timeouts from `FX_START_TIMEOUT` / `FX_STOP_TIMEOUT`.
- Check dependencies at startup (PostgreSQL ping, SQS queue exists, JWKS reachable) and fail fast
  with a clear error.
- HTTP: Listen synchronously, Serve asynchronously, `Shutdown` on stop.
- Readiness fails as soon as shutdown starts.
- `OnStop` runs in reverse order; hooks live on the resource constructor.
- Outbox worker: cancel poll, finish the in-flight batch within a lease-bounded context, wait on `done`.
- Inbound worker: cancel long-poll, finish or release visibility of the in-flight message, wait on `done`.
- Pending worker: cancel poll, finish the in-flight batch within a lease-bounded context, wait on `done`.

[ADR 0004](docs/adr/0004-fx-lifecycle-and-shutdown.md).

## Observability

**Logs:** `log/slog` JSON ([ADR 0002](docs/adr/0002-go-version-and-http-router.md)) with
`X-Correlation-Id` (generated UUID v7 when missing) and identifier fields `correlationId`,
`providerId`, `transactionId`, `walletId`, `messageId` when known. A key denylist redacts
credentials and financial payload fields ([ADR 0021](docs/adr/0021-observability-logs-and-metrics.md)).

**Metrics:** `app.Metrics` port; Prometheus adapter (`prometheus/client_golang`) on a private
registry. Public `GET /metrics`. Catalog: [`docs/api/metrics.md`](docs/api/metrics.md). Outbox lag
is unpublished row count and oldest `occurred_at`. HTTP labels use ServeMux patterns, not raw ids.

**Health:** public `GET /health/live` and `GET /health/ready` (PostgreSQL + SQS; ready fails during
shutdown). [`docs/api/health.md`](docs/api/health.md).

HTTP statuses: [ADR 0013](docs/adr/0013-http-status-mapping.md), [`docs/api/status.md`](docs/api/status.md).

**Tracing:** not implemented (optional OBS-03).

## Limitations, interpretations and unfinished work

### Unfinished / out of scope

- OpenTelemetry tracing and dashboards (OBS-03) — optional in `init.md` §12 / §14; not implemented.
- Load tests — optional differential (`init.md` §14). No reproducible throughput harness is shipped.
  Concurrency and recovery are proven by tagged tests and runbooks, not by p50/p95/p99 targets.
  There is no RPS goal.
- Double-entry bookkeeping (“partidas dobradas”) — optional differential; the ledger is
  append-only credit/debit entries, not a paired double-entry chart of accounts.
- `sqlc` was considered and not chosen ([ADR 0003](docs/adr/0003-database-access-and-migrations.md)).

### Operational limitations

- Health and `GET /metrics` are unauthenticated. HTTP metric cardinality uses route patterns, not
  raw ids. Operators must not expose the process on a public network without a scrape ACL.
- Tagged integration tests skip when required env is unset (`skip-if-no-env`). That skip is not a
  substitute for CI with real PostgreSQL, Keycloak and LocalStack.
- A JWT’s `iss` must match the process `OIDC_ISSUER`. Host tokens (`http://localhost:8081/...`)
  are rejected by Compose app containers (`http://keycloak:8080/...`).
- Production binaries do not ship crash flags. Precise commit/publish gaps use tests-only hooks
  (`SetAfterCommit`, `SetAfterPublish`).
- `int64` minor units overflow at `92233720368547758.07`. Scale is fixed at 2 decimal places.
- SQS inbound `data.providerId` is trusted as queue-gated identity, not as an HTTP JWT.

### Interpretations

- Migrate Up on process start; rollback is operator-driven via CLI.
- Default `go test ./...` never requires Docker.
- `"25"` / `"25.0"` are rejected as money input (no normalization before hash).
- A BET is refunded at most once in its lifetime even if that refund is later rolled back.
- HTTP `providerId` is always the token `azp`, never the untrusted body/path alone.
- `GET /wagering/transactions/{id}` for another provider is `404` (no existence leak); provider-path
  mismatch stays `403`.
- `PENDING_REFERENCE` is HTTP `202`. The SQS inbound message is deleted after that commit; the
  pending worker resumes from PostgreSQL.
- `OPENING` is an internal wallet credit only; HTTP and SQS reject it as an external kind.
- FIFO `MessageGroupId` is `walletId` inbound and `aggregateId` outbound; application idempotency
  is independent of the broker’s 5-minute content-based window.
- `REFUND`/`ROLLBACK` of a missing or still-pending reference waits (`PENDING_REFERENCE`); a
  reference already `REJECTED`/`FAILED` is `REFERENCE_UNSUCCESSFUL`, not `REFERENCE_NOT_FOUND`.
