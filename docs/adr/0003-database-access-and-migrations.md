# 0003 — Database access and migrations

- **Status:** Accepted
- **Date:** 2026-09-15
- **Related requirements:** `init.md` §4 (PostgreSQL; versioned migrations with documented apply and rollback; `pgx` preferred, `sqlc` optional, `database/sql` and GORM accepted); matrix IDs STK-04, STK-05 (library only)

## Context

The service persists financial state in PostgreSQL. The challenge prefers `pgx` with explicit SQL, allows `sqlc`, and accepts `database/sql` or GORM, but requires transactions, locks and constraints to remain explicit and verifiable. Apply and rollback of versioned migrations must be documented. Phase 1 only needs a connection, a migration runner, and a baseline schema — not wallets, money mapping or a unit of work.

## Options considered

### Option A — `pgx/v5` pool + golang-migrate v4 SQL files

- Pros: Matches the challenge preference; explicit SQL; `pgxpool` is the usual production pool; golang-migrate is widely used, supports up/down files, and can run from the app or the CLI.
- Cons: Hand-written SQL until/unless `sqlc` is adopted; migrate’s CLI DSN scheme (`postgres://` vs `pgx5://`) can differ from the driver’s preferred string.

### Option B — GORM or `database/sql` as the primary access layer

- Pros: Familiar; GORM migrations-in-code.
- Cons: Implicit SQL and hooks hide locks and constraints; GORM auto-migrate is the opposite of “invariants in the schema”; `database/sql` would still wrap `pgx` for PostgreSQL without buying much in Phase 1.

### Option C — Adopt `sqlc` in Phase 1

- Pros: Typed queries, still explicit SQL.
- Cons: Extra codegen workflow before any financial tables exist; the challenge marks `sqlc` as optional. Choosing it now would freeze a toolchain we do not yet need.

## Decision

- **Access:** `github.com/jackc/pgx/v5` with a `pgxpool.Pool`. Queries are explicit SQL. GORM is not used. `database/sql` is not the primary access layer (no `sql.DB` as the app’s pool).
- **sqlc:** not used in Phase 1. It remains optional later and is **not** chosen now.
- **Migrations:** `github.com/golang-migrate/migrate/v4`, SQL files in `migrations/`. Every version has **up and down**. Naming follows golang-migrate: `{version}_{title}.up.sql` / `{version}_{title}.down.sql`.
- **Baseline:** `000001_bootstrap` creates PostgreSQL schema `wagering` only. Financial tables, constraints and grants come in later migrations.
- **Apply:** the application runs migrate **Up** on start, reading files from `MIGRATIONS_PATH`.
- **Rollback:** not automatic on shutdown. Operators roll back with the golang-migrate CLI, for example:

  ```sh
  migrate -path migrations -database "$POSTGRES_DSN" down 1
  ```

  If the process DSN is not accepted as-is, use the golang-migrate postgres/pgx URL form (scheme `postgres://` or `pgx5://`, plus `sslmode` as required). See the README.

**Out of scope for this ADR** (later ADRs): `Money` database mapping; SQL transaction boundary / unit of work across repositories; ledger immutability triggers and application-role grants.

## Consequences

- Positive: STK-04 is documentable in Phase 1; financial SQL stays explicit; rollback is a conscious operator action, not a side effect of stopping the process.
- Negative / accepted risks: migrate-on-start means a new instance can apply schema changes; that is required for local Compose and must stay compatible with N instances (no destructive down on boot).
- Known limitations: STK-05 stays incomplete until Money mapping and the unit of work are decided. Baseline migration does not enforce financial invariants.

## Verification

- `go.mod` requires `github.com/jackc/pgx/v5` and `github.com/golang-migrate/migrate/v4`.
- `migrations/000001_bootstrap.up.sql` / `.down.sql` exist; up creates schema `wagering`, down drops it (or otherwise reverts that change).
- README documents apply (app on start) and CLI rollback.
- Process DSN is `POSTGRES_DSN`; migration files are loaded from `MIGRATIONS_PATH`.
