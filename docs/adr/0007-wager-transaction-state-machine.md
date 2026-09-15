# 0007 — WagerTransaction state machine

- **Status:** Proposed
- **Date:** 2026-09-15
- **Related requirements:** `init.md` §6.3, §7; matrix IDs WTX-02, WTX-05, WTX-06, OPS-10..13, DOM-01

## Context

External operations start in `PENDING` and must move through a validated machine to processed,
waiting-for-reference, rejected or permanently failed. Terminal states are immutable. Replay must
not re-apply effects. The domain must distinguish transient infrastructure failure from a
business rejection, and `OPENING` from provider-originated kinds.

Pending-reference backoff, TTL and the worker are Phase 8; this ADR only fixes transitions and
failure classes so Phase 2 can implement the machine.

## Options considered

### Option A — Explicit domain transitions on an encapsulated entity

Methods such as `MarkProcessed`, `MarkRejected`, `MarkPendingReference`, `MarkFailed`. Origin
(`INTERNAL` vs `EXTERNAL`) is a field; `OPENING` is constructible only as internal.

- Pros: Invalid paths are compile- and test-visible; rehydration does not call transition methods.
- Cons: Application still has to persist `PENDING` when it needs durable resumption.

### Option B — Status as a free string mutated by use cases

- Pros: Less domain code.
- Cons: Easy to skip validation; contradicts §6 encapsulation.

## Decision

Statuses and allowed transitions:

```
PENDING ──► PROCESSED          (terminal)
PENDING ──► REJECTED           (terminal)
PENDING ──► FAILED             (terminal)
PENDING ──► PENDING_REFERENCE
PENDING_REFERENCE ──► PROCESSED
PENDING_REFERENCE ──► REJECTED
PENDING_REFERENCE ──► FAILED
PENDING_REFERENCE ──► PENDING_REFERENCE  (retry/backoff bookkeeping only)
```

`PROCESSED`, `REJECTED` and `FAILED` accept **no** further transitions. Replay reads the persisted
row (including `failureCode` and the balance snapshot on `PROCESSED`) and does not call debit/credit.

**Origin**

| | `INTERNAL` (`OPENING`) | `EXTERNAL` (`BET`/`WIN`/`LOSS`/`REFUND`/`ROLLBACK`) |
| --- | --- | --- |
| Provider, external id, idempotency key, payload hash, round, game, reference | Not applicable | Required as specified per kind |
| Construction | `NewOpeningTransaction` | `NewExternalTransaction` (rejects `OPENING`) |

**Failure classes** (classifiable via `errors.As`, not string matching):

| Class | Meaning | Typical persistence |
| --- | --- | --- |
| `Validation` | Malformed or disallowed input (e.g. `OPENING` on the public API) | No financial effect; usually no new row |
| `Rejection` | Business rule (`INSUFFICIENT_FUNDS`, duplicate reversal, …) | Transaction `REJECTED` with `failureCode` |
| `Wait` | Mandatory/optional reference not yet usable | `PENDING_REFERENCE` |
| `Transient` | Infrastructure; retry | Leave in-flight / retry; not a domain terminal |
| `Permanent` | Unrecoverable infrastructure recorded for audit | `FAILED` |

`PENDING_REFERENCE` when the referenced operation **exists but is still `PENDING` or
`PENDING_REFERENCE`**: wait (same `Wait` class), do not reject. When the reference is `REJECTED` or
`FAILED`: reject with `REFERENCE_UNSUCCESSFUL`. Unknown reference after TTL is Phase 8
(`REFERENCE_NOT_FOUND`); in Phase 2 Apply treats a missing reference as `Wait`.

Synchronous completion without an intermediate accept-commit is allowed when there is no missing
reference (use case later). A committed `PENDING` must remain resumable by another instance
(persistence later).

## Consequences

- Positive: WTX-02/05/06 have a single documented machine; adapters map class → HTTP/SQS without
  parsing error strings.
- Negative / accepted risks: `FAILED` is reserved for infra and is unused by Apply in Phase 2.
- Known limitations: backoff, TTL, durable `PENDING` resume and `failureCode` → HTTP mapping are
  later ADRs/phases.

## Verification

- `internal/domain/transaction_test.go` (transitions, terminal immutability, opening vs external).
- `internal/domain/apply_test.go` (wait vs unsuccessful reference).
