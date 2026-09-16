# 0016 — Outbox claim, lease, backoff and publish/ack recovery

- **Status:** Proposed
- **Date:** 2026-09-16
- **Related requirements:** `init.md` §11; matrix IDs OBX-02, OBX-03, TST-13, FX-03, ELIM-08, GAR-04

## Context

Multiple process instances must publish `wagering.outbox_events` without a global lock, recover
abandoned claims, retry failed `SendMessage` with backoff, and preserve `eventId` on republish.
Publishing must happen only after the financial commit ([ADR 0014](0014-outbox-persistence.md)).
The destination is [ADR 0015](0015-outbox-destination-and-routing.md).

Two crash windows must be demonstrated: after commit and before `SendMessage`; after `SendMessage`
and before `published_at` is set.

## Options considered

### Option A — `SELECT … FOR UPDATE SKIP LOCKED`, lease via `next_attempt_at`, publish outside the lock

Short transaction: pick due unpublished rows (`published_at IS NULL AND next_attempt_at <= now()`),
`SKIP LOCKED`, increment `attempts`, set `next_attempt_at = now() + lease`, commit. Then
`SendMessage`. On success set `published_at`. On send failure set `next_attempt_at = now() + backoff`.
Exponential backoff `min(OUTBOX_BACKOFF_MAX, 1s << (attempts-1))`. Unbounded retries.

Injectable `AfterPublish` hook (tests only) can skip `MarkPublished`.

- Pros: Row locks are not held across SQS latency; another instance takes over after the lease;
  existing columns suffice (no migration); hook proves publish-before-ack without `kill -9`.
- Cons: A crash after claim burns a lease even if send never happened; `attempts` counts claims, not
  only broker failures.

### Option B — Hold `FOR UPDATE` for the duration of `SendMessage`

- Pros: One transaction, simpler state.
- Cons: Database row locks last as long as the broker call; pool connections stuck on I/O; worse
  multi-publisher throughput.

### Option C — Extra `claimed_until` / `claimed_by` columns

- Pros: Lease is distinct from backoff.
- Cons: Migration for a distinction `next_attempt_at` already encodes if the claimer always pushes
  it forward.

## Decision

**Option A.**

- Claimer is pool-scoped and opens its own short transaction. It does **not** use the financial
  `UnitOfWork` (those transactions already committed).
- `event_id` is never updated.
- `MarkPublished` is `UPDATE … SET published_at = $at WHERE event_id = $1 AND published_at IS NULL`.
- Abandoned work: when the lease expires the row is due again; another publisher may `SKIP LOCKED`
  it and republish the same `eventId`.
- Production processes do not install the hook. Tests pass `AfterPublish` that returns an error to
  skip the ack (crash between publish and confirmation). Crash between commit and publish is the
  default unpublished row.

Worker: Fx `OnStart` starts a goroutine; poll ticker uses a cancellable context; each `PublishDue`
uses a context bounded by the lease (not cancelled by shutdown mid-send); `OnStop` cancels polling
and waits on an observable `done` channel within `FX_STOP_TIMEOUT`. Config:
`OUTBOX_POLL_INTERVAL`, `OUTBOX_BATCH_SIZE`, `OUTBOX_LEASE`, `OUTBOX_BACKOFF_MAX`.

## Consequences

- Positive: Contention is per row via `SKIP LOCKED`; N instances share PostgreSQL; ELIM-08 holds
  because `SendMessage` only sees committed rows; recovery does not mint a new `eventId`.
- Negative / accepted risks: At-least-once means a consumer may see the same `eventId` twice if ack
  fails outside the FIFO window. Stop timeout must cover an in-flight batch.
- Known limitations: No outbox DLQ; lag metrics are Phase 10; ≥3 OS processes remain Phase 9
  (TST-13 is two publishers, which may share a process as two relays).

## Verification

- Unit: backoff table; relay success / send-fail retry / skip-ack hook with fake ports.
- Integration (PostgreSQL): two claimers, no overlapping `event_id`s (`SKIP LOCKED`).
- Integration (PostgreSQL + LocalStack): two relays on the same unpublished batch; recovery
  commit→publish; recovery publish→ack with the hook; `eventId` preserved on the wire.
- Worker `done` closes on Fx stop.
