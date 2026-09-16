# 0019 — Pending-reference worker, backoff, TTL and PENDING resume

- **Status:** Proposed
- **Date:** 2026-09-16
- **Related requirements:** `init.md` §6.3, §6.5, §7; matrix IDs OPS-10, OPS-11, OPS-12, WTX-04, TST-14, TST-15, FX-03/04

## Context

External operations may wait for a reference that has not arrived yet (`PENDING_REFERENCE`) or,
rarely, sit in committed `PENDING` after an accept that did not finish Apply. Another instance must
resume that work after restart. Missing-reference retries need persisted exponential backoff and a
hard stop (max attempts or TTL) that rejects with `REFERENCE_NOT_FOUND`.

[ADR 0007](0007-wager-transaction-state-machine.md) already defines wait vs `REFERENCE_UNSUCCESSFUL`.
Schema columns `attempt_count` and `next_attempt_at` plus index `wager_transactions_pending_resume_idx`
exist since migration `000002`. Inbound SQS deletes the message once `PENDING_REFERENCE` is committed
([ADR 0018](0018-sqs-inbound-consume.md)); continuity is this worker, not SQS redelivery.

Synchronous completion without an intermediate accept-commit remains allowed when there is no
missing reference ([ADR 0007](0007-wager-transaction-state-machine.md)).

## Options considered

### Option A — `SELECT … FOR UPDATE SKIP LOCKED` lease, then Apply with wallet-first locking

Short claim transaction: due external rows in `PENDING` / `PENDING_REFERENCE`, `SKIP LOCKED`,
increment `attempt_count`, set `next_attempt_at = now() + lease`, commit. A second unit of work
locks the **wallet** first (same order as `Submit`), then the transaction `FOR UPDATE`, runs
`domain.Apply`, and persists. Still waiting → `ScheduleRetry` with exponential backoff. Exhausted
wait → `REJECTED` / `REFERENCE_NOT_FOUND`. Terminal rows are skipped.

- Pros: No row lock across Apply; N instances share PostgreSQL; lock order matches HTTP/SQS submit
  (avoids wallet/tx deadlock); existing columns, no new table.
- Cons: A crash after claim burns a lease; `attempt_count` counts claims, not only Apply attempts.

### Option B — Hold `FOR UPDATE` on the transaction for the duration of Apply

- Pros: One transaction, no lease.
- Cons: Claim lock order (tx then wallet) deadlocks with `Submit` (wallet then insert/update);
  connections held for the whole Apply.

### Option C — Delayed SQS / separate pending queue

- Pros: Broker-side delay.
- Cons: Extra topology; PENDING resume is a SQL row, not a message (inbound already deleted);
  duplicates the outbox/inbox machinery.

## Decision

**Option A.**

- Claimer is pool-scoped and does **not** use the financial `UnitOfWork`.
- Due predicate: `origin = 'EXTERNAL' AND status IN ('PENDING', 'PENDING_REFERENCE') AND
  (next_attempt_at IS NULL OR next_attempt_at <= now())`. `OPENING` is never claimed.
- Apply lock order: wallet `FOR UPDATE`, then transaction `FOR UPDATE`. If the row is already
  terminal, skip.
- First wait (from `PENDING`) emits `WagerTransactionPendingReference`. Retries that remain
  `PENDING_REFERENCE` update `updated_at` only — no extra outbox event (ADR 0007 bookkeeping).
- Exhaustion is checked **after** Apply: if the reference is now usable, the operation processes
  even on the last attempt. If it still waits and `attempts >= PENDING_MAX_ATTEMPTS` or
  `now >= created_at + PENDING_TTL`, reject with `REFERENCE_NOT_FOUND` and emit
  `WagerTransactionRejected`.
- Exponential backoff `min(PENDING_BACKOFF_MAX, 1s << (attempts-1))` (same helper as the outbox).
- `Submit` / `HandleInbound` still complete operations without a missing reference in one commit
  (no intermediate `PENDING` row). A committed `PENDING` (accept-then-crash, or a test insert) is
  resumed by this worker.
- Config: `PENDING_POLL_INTERVAL`, `PENDING_BATCH_SIZE`, `PENDING_LEASE`, `PENDING_BACKOFF_MAX`,
  `PENDING_MAX_ATTEMPTS`, `PENDING_TTL`.

Worker: Fx `OnStart` goroutine; each `ResumeDue` uses a lease-bounded context; `OnStop` cancels
polling and waits on `done` within `FX_STOP_TIMEOUT`.

## Consequences

- Positive: OPS-10/11 and WTX-04 share one worker; HTTP `202` / SQS delete stay valid; N instances
  contend per row via `SKIP LOCKED`; financial invariants stay in Apply + schema.
- Negative / accepted risks: At-least-once claim means a crashed Apply rolls back and another
  instance retries; a wait that outlives TTL is `REFERENCE_NOT_FOUND` even if the reference exists
  but is still `PENDING` / `PENDING_REFERENCE`.
- Known limitations: No pending-work metrics (Phase 10); ≥3 OS processes remain Phase 9
  (TST-14/15 are one process, two claimers, or a second `Service` on the same database).

## Verification

- Unit: wait retry does not re-emit `WagerTransactionPendingReference`; resume resolves when the
  reference arrives; TTL/max attempts → `REFERENCE_NOT_FOUND`; committed `PENDING` BET is applied.
- Integration (PostgreSQL): two claimers, no overlapping ids (`SKIP LOCKED`); refund-before-bet
  later processes (TST-14); expiry rejects; inserted `PENDING` resumed after a new `Service`
  (TST-15); worker `done` closes on Fx stop.
