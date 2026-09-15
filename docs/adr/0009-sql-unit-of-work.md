# 0009 — SQL unit of work / transaction boundary

- **Status:** Proposed
- **Date:** 2026-09-15
- **Related requirements:** `init.md` §4 (document the SQL transaction boundary across repositories); §5 guarantees 3, 5, 7, 8; matrix IDs STK-05, WAL-04, GAR-03, GAR-05, GAR-07, GAR-08, OBX-01

## Context

Financial changes must land in one PostgreSQL commit: wallet balance, `WagerTransaction`, and the
matching ledger entry (WAL-04, GAR-05). Invariants live in the database, not in process memory
(GAR-03, GAR-08). Lost updates are prevented inside that same transaction (GAR-07; locking is
[ADR 0010](0010-per-wallet-concurrency.md)). Later, inbox and outbox rows must join the same commit
as domain writes (OBX-01) so events are never published before the originating transaction
confirms.

[ADR 0003](0003-database-access-and-migrations.md) chose `pgx/v5` and left the unit of work out of
scope. [ADR 0001](0001-package-layout-and-layer-boundaries.md) forbids the domain from importing
`pgx`. Use cases must own the transaction boundary without leaking the driver. Reads that decide a
debit or credit must see the same snapshot they will write.

## Options considered

### Option A — Callback unit of work on application ports

`UnitOfWork.Within(ctx, func(ctx, Repositories) error)` opens one `pgx.Tx`, injects tx-scoped
repositories, commits on `nil`, rolls back on error or panic. Repository interfaces live in
`internal/app`. The adapter implements UoW and repos with `pgx/v5`.

- Pros: One commit across wallet, transaction and ledger; domain and use cases stay free of `pgx`;
  `Repositories` can grow (inbox/outbox) without changing the callback pattern; rollback on panic
  is explicit.
- Cons: Callback style is more ceremony than “just pass a tx”; nested `Within` needs a later rule
  (Phase 3 uses one callback per operation).

### Option B — Pass `pgx.Tx` into use cases

Use cases call `pool.Begin` (or take `pgx.Tx`) and pass the tx into each repository method.

- Pros: Obvious to anyone who knows `pgx`; no extra port.
- Cons: Leaks the driver into `internal/app`, contradicting ADR 0001; swapping or testing the
  boundary requires `pgx` types; STK-05’s “documented boundary” becomes an infrastructure leak.

### Option C — Each repository calls `Begin` / `Commit`

Wallet, transaction and ledger repositories each open their own transaction.

- Pros: Simple repository constructors (they only need the pool).
- Cons: Cannot share one commit (WAL-04, GAR-05 fail); partial writes survive a later failure;
  inbox/outbox cannot join the same commit later (OBX-01).

## Decision

The application layer (`internal/app`) owns the ports: `UnitOfWork` and the repository interfaces.
The domain stays free of I/O and `pgx`.

- `UnitOfWork.Within(ctx, func(ctx context.Context, repos Repositories) error)`:
  1. Opens one `pgx.Tx`.
  2. Injects **tx-scoped** repositories into `Repositories`.
  3. Runs the callback.
  4. Commits if the callback returns `nil`.
  5. Rolls back if the callback returns an error or panics (the panic is re-raised after rollback).
- Repositories **never** open their own transactions. All wallet, transaction and ledger writes for
  one operation share that tx.
- `Repositories` is a struct of interfaces. Inbox and outbox repositories are **not** in this
  phase; the struct must remain extensible so they can join the same commit later **without**
  changing the UoW pattern (`Within` + tx-scoped repos).
- Adapter: `internal/adapter/postgres` implements UoW with `pgx/v5`. No GORM, no sqlc, no
  `database/sql` as the primary API ([ADR 0003](0003-database-access-and-migrations.md)).
- Unique-violation, check-violation and lock/deadlock errors are mapped to classifiable
  app/domain errors usable with `errors.Is` / `errors.As`. Mapping uses PostgreSQL `SQLSTATE`
  (via `pgconn.PgError`), never string comparison of messages.
- Reads that participate in a financial decision (load wallet for debit/credit, load a reference
  for REFUND/ROLLBACK) run **inside** `Within` on the same tx as the writes.

Money on the wire to PostgreSQL is `BIGINT` minor units plus currency, as proposed in
[ADR 0006](0006-money-representation.md). This ADR does not choose the schema objects; it chooses
the commit boundary those objects are written through.

## Consequences

- Positive: WAL-04 is structural (one tx); driver types stay in the adapter; OBX-01 can land later
  by adding fields to `Repositories`; STK-05’s transaction-boundary gap is closed as a decision.
- Negative / accepted risks: every mutating use case must go through `Within`; forgetting that is a
  review defect. Constraint violations become visible only after SQL (domain still rejects first).
- Known limitations: inbox/outbox are not in this phase. Nested `Within` (savepoints or re-entrant
  commit) is not specified; Phase 3 use cases use a single callback. Per-wallet locking lives in
  [ADR 0010](0010-per-wallet-concurrency.md).

## Verification

- `go list -deps ./internal/domain/...` and `./internal/app/...` contain neither `pgx` nor
  `database/sql`.
- Integration tests: a mid-callback failure leaves no wallet, transaction or ledger row; a success
  persists all three together.
- Adapter tests (or a small mapper test): `23505`, `23514` and lock/deadlock `SQLSTATE` values wrap
  sentinels that `errors.Is` can match; `err.Error()` string matching is absent.
- When inbox/outbox are added, they are fields on `Repositories` used inside the same `Within`;
  the UoW interface does not grow a second commit API.
