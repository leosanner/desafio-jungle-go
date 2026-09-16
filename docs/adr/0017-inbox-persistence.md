# 0017 — Inbox persistence in the financial unit of work

- **Status:** Proposed
- **Date:** 2026-09-16
- **Related requirements:** `init.md` §6.5, §10, §11; matrix IDs INB-01, INB-02, OBX-01, SQS-03, SQS-04, TST-12

## Context

HTTP already persists operations through `UnitOfWork.Within` with wallet, transaction, ledger and
outbox in one commit ([ADR 0009](0009-sql-unit-of-work.md), [ADR 0014](0014-outbox-persistence.md)).
SQS delivery is at-least-once. The challenge requires an inbox keyed by `(consumerName, messageId)`,
a payload hash checked on redelivery, received and completed timestamps, and that inbox completion
shares the SQL transaction of the domain writes. A committed `PENDING_REFERENCE` may complete the
inbound message; Phase 8 resumes it.

Crash between commit and SQS `DeleteMessage` must redeliver without a second debit (TST-12).

## Options considered

### Option A — Insert a completed inbox row in the same `Within` as `submitInTx`

Table `wagering.inbox_messages`. Identity is envelope `messageId` (not the SQS receipt id) plus a
fixed `consumer_name`. Hash is SHA-256 hex of the **raw SQS body**. Both `received_at` and
`completed_at` are set on insert in the same commit as domain/ledger/outbox. A later receive with
the same id and hash is a no-op (delete the SQS message). A later receive with a different hash is
permanent (`ErrInboxHashMismatch`).

- Pros: INB-01/02 and OBX-01 are structural; no half-received row can commit without the treatment;
  redelivery after delete-skip is proven with a test hook.
- Cons: An invalid message that never reaches `HandleInbound` has no inbox row (it goes to the DLQ).

### Option B — Two-phase inbox (received, then completed)

Insert `received_at` first, complete in a second transaction after domain writes.

- Pros: Shows in-flight work in the table.
- Cons: A crash after received-only leaves a row without domain writes; redelivery must take over
  incomplete rows; contradicts “inbox and durable treatment share one SQL transaction”.

### Option C — Dedup only on `(providerId, idempotency_key)`

- Pros: No new table.
- Cons: Misses envelope identity and hash-on-redelivery (SQS-03); two SQS copies with different
  keys could still double-apply if the producer also changes the idempotency key.

## Decision

**Option A.**

| Column | Role |
| --- | --- |
| `consumer_name` | Fixed name `wager-transactions` for this process |
| `message_id` | Envelope `messageId` |
| `payload_hash` | Lowercase SHA-256 hex of the raw message body (64 chars) |
| `received_at` | Clock at insert |
| `completed_at` | Clock at insert (`>= received_at`) |

Primary key `(consumer_name, message_id)`. `Repositories.Inbox` joins the existing `Within`. HTTP
`Submit` does not write inbox rows. `HandleInbound` calls `submitInTx` then `Inbox.Insert` in one
callback; unique conflicts recover like `Submit` (replay + ensure inbox). Hash mismatch does not
insert or update the row.

The application role only `INSERT`/`SELECT`s; rows are not updated (completion is the insert).

## Consequences

- Positive: Redelivery after commit-without-delete cannot create a second movement; inbox uniqueness
  is enforced by PostgreSQL; HTTP and SQS still share `submitInTx` / canonical payload hash
  ([ADR 0012](0012-idempotency-canonical-hash.md)).
- Negative / accepted risks: Invalid JSON never gets an inbox row; operators inspect the DLQ.
- Known limitations: Consume policy (visibility, backoff, DLQ, FIFO ids, shutdown) is
  [ADR 0018](0018-sqs-inbound-consume.md). Pending-reference resumption is Phase 8.

## Verification

- Migration `000004` up/down; unique `(consumer_name, message_id)` rejects duplicates.
- Unit: `HandleInbound` writes inbox with domain rows; callback error writes none; second call with
  the same id/hash is a replay; different hash → `ErrInboxHashMismatch`.
- Integration: TST-12 hook skips `DeleteMessage` after commit; redelivery does not debit twice.
