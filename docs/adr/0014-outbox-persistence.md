# 0014 — Outbox persistence without a publisher

- **Status:** Accepted
- **Date:** 2026-09-16
- **Related requirements:** `init.md` §9 (opening commit), §11; matrix IDs HTTP-01, OBX-01, OBX-05..08, ELIM-08, GAR-04

## Context

HTTP-01 requires `OPENING` + ledger + outbox rows for `WagerTransactionProcessed` and
`WalletBalanceChanged` in the **same commit** as the wallet. Domain events already exist
([`docs/events/outbox-events.md`](../events/outbox-events.md)). Phase 6 owns the concurrent
publisher (claim, backoff, recovery). Publishing before commit is disqualifying (ELIM-08).

[ADR 0009](0009-sql-unit-of-work.md) left `Repositories.Outbox` as a planned field on the same
`Within` callback.

## Options considered

### Option A — Persist unpublished rows in Phase 5; publisher in Phase 6

Migration adds `wagering.outbox_events`. Use cases insert rows in the same `Within` as wallet,
transaction and ledger. `published_at` stays NULL. No worker.

- Pros: HTTP-01 and OBX-01 hold now; Phase 6 only adds claim/publish; crash between commit and
  publish leaves durable rows.
- Cons: Events accumulate until Phase 6; no delivery yet.

### Option B — Defer the table until Phase 6

- Pros: Smaller Phase 5 diff.
- Cons: HTTP-01 incomplete; opening would have to be retrofitted.

### Option C — Publisher in Phase 5

- Pros: End-to-end events sooner.
- Cons: Scope of Phase 6 (SKIP LOCKED, multi-publisher, recovery tests).

## Decision

**Option A.**

Table `wagering.outbox_events`:

| Column | Role |
| --- | --- |
| `event_id` | Primary key; UUID v7 assigned at insert; preserved on later republish |
| `event_type`, `event_version` | From the domain constructor (`1`) |
| `aggregate_id` | Transaction id or wallet id |
| `correlation_id` | Request `X-Correlation-Id` or a generated UUID v7 |
| `causation_id` | Originating transaction id (nullable) |
| `occurred_at` | UTC |
| `payload` | JSONB snapshot of event **data** (money as strings, [ADR 0006](0006-money-representation.md)) |
| `attempts` | `0` until the publisher |
| `next_attempt_at` | `occurred_at` (immediately eligible) |
| `published_at` | NULL until Phase 6 |

Partial index on unpublished rows `(next_attempt_at) WHERE published_at IS NULL` for the future
claimer. Application role may `INSERT` only from use cases; publisher `UPDATE` of claim columns is
Phase 6. No `SKIP LOCKED` in this phase.

`Repositories.Outbox.Insert` joins the existing unit of work. Inbox remains later (Phase 7).

Zero-balance opening and `LOSS` still do not write `WalletBalanceChanged`.

## Consequences

- Positive: Events cannot be published before commit because nothing publishes them yet, and when
  the publisher arrives it will only see committed rows.
- Negative / accepted risks: Unbounded unpublished growth until Phase 6 in long-running envs.
- Known limitations: Destination, routing, concurrent claim, backoff and recovery stay Phase 6.

## Verification

- Migration `000003` up/down; integration: positive opening inserts two outbox rows in the same
  commit as wallet/ledger; zero opening inserts none; callback error inserts none.
- `go list -deps ./internal/app` still has no `pgx`.
