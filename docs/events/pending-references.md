# Pending references

Worker that resumes `PENDING` and `PENDING_REFERENCE` rows ([ADR 0019](../adr/0019-pending-reference-resume.md)).
It is not an SQS consumer: inbound already deletes the message after the wait is committed
([inbound.md](inbound.md)).

## When a row waits

`REFUND` / `ROLLBACK` (and optional `WIN` references) persist `PENDING_REFERENCE` when the referenced
external id is missing or the referenced row is still `PENDING` / `PENDING_REFERENCE`. HTTP returns
`202`. SQS deletes the inbound message. Continuity is this worker.

Operations without a missing reference still complete in the request's SQL commit (no intermediate
`PENDING` row). A committed `PENDING` row (accept then crash) is also claimed here.

## Claim and Apply

Due predicate: external rows in `PENDING` or `PENDING_REFERENCE` with `next_attempt_at` null or in
the past. Claim uses `SELECT … FOR UPDATE SKIP LOCKED`, increments `attempt_count`, and leases
`next_attempt_at`. Apply then locks the **wallet** first, then the transaction (same order as HTTP/SQS
submit).

Still waiting → exponential backoff `min(PENDING_BACKOFF_MAX, 1s << (attempts-1))`. The first wait
emits `WagerTransactionPendingReference`; retries do not.

## Exhaustion

After Apply, if the operation still waits and `attempts >= PENDING_MAX_ATTEMPTS` or
`now >= created_at + PENDING_TTL`, the row is `REJECTED` with `REFERENCE_NOT_FOUND` and
`WagerTransactionRejected` is written to the outbox.

If the reference is already `PROCESSED`, the waiter processes even on the last attempt. If the
reference is `REJECTED` / `FAILED`, the waiter is rejected with `REFERENCE_UNSUCCESSFUL` (not
`REFERENCE_NOT_FOUND`).

## Config

`PENDING_POLL_INTERVAL`, `PENDING_BATCH_SIZE`, `PENDING_LEASE`, `PENDING_BACKOFF_MAX`,
`PENDING_MAX_ATTEMPTS`, `PENDING_TTL`.

## Shutdown

Cancel polling. Finish the in-flight batch within `PENDING_LEASE`, or fail the stop deadline
(`FX_STOP_TIMEOUT`).
