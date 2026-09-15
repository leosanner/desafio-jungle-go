# 0008 — REFUND and ROLLBACK combinations

- **Status:** Proposed
- **Date:** 2026-09-15
- **Related requirements:** `init.md` §7; matrix IDs OPS-04..09, OPS-12, LED-03

## Context

`REFUND` credits a processed `BET`. `ROLLBACK` reverses a processed `BET`, `WIN` or `REFUND`. Both
require `referenceExternalTransactionId` resolved as `(providerId, referenceExternalTransactionId)`.
The same reference must not receive two successful reversals of the **same** kind. Combinations of
`REFUND` and `ROLLBACK` on one bet must not return the same debit twice. A reversal that would
drive the wallet negative is an auditable rejection with a **different** `failureCode` from a
normal bet without funds.

## Options considered

### Option A — Mutual exclusion on the BET debit; ROLLBACK of REFUND/WIN allowed

A processed BET may be returned by **either** one `REFUND` **or** one `ROLLBACK` of that BET, never
both. `ROLLBACK` of a `WIN` or of a `REFUND` is a separate reference (the WIN/REFUND id). A BET is
refunded at most once in its lifetime, even if that refund is later rolled back.

- Pros: Simple uniqueness (`(kind, referencedTransactionId)` among `PROCESSED`); no double credit
  of the original debit; matches “no two successful reversals of the same type”.
- Cons: After `REFUND` + `ROLLBACK(REFUND)`, a second refund of the original BET is refused.

### Option B — Allow REFUND after rolling back a previous REFUND

Treat a rolled-back refund as if it never consumed the debit.

- Pros: More operator flexibility.
- Cons: Easy to get double-return bugs; uniqueness is no longer “one PROCESSED REFUND per BET”.

## Decision

Resolution is by `(providerId, referenceExternalTransactionId)`. The operation and its reference
must match **provider, player, wallet, currency and round**. Reversal `Money` must equal the
referenced amount (no partial reversals).

Kind rules:

| Incoming | Allowed reference | Movement |
| --- | --- | --- |
| `REFUND` | `PROCESSED` `BET` only | Credit (return the debit) |
| `ROLLBACK` | `PROCESSED` `BET`, `WIN` or `REFUND` | Opposite of the reference (`BET` → credit, `WIN`/`REFUND` → debit) |

Uniqueness (among `PROCESSED` rows only):

1. At most one `REFUND` whose reference is a given BET.
2. At most one `ROLLBACK` whose reference is a given transaction.
3. `REFUND(BET)` and `ROLLBACK(BET)` are **mutually exclusive**. If either is `PROCESSED`, the other
   is rejected with `DUPLICATE_REVERSAL`.
4. `ROLLBACK(REFUND)` is allowed (reference is the refund). That does **not** lift rule 1: the BET
   still has a processed refund, so it cannot be refunded again and cannot be rolled back as a BET.

Missing reference → `PENDING_REFERENCE` (Phase 2). Reference still pending → wait. Reference
terminal unsuccessful → `REFERENCE_UNSUCCESSFUL`. Amount/player/wallet/round mismatch →
`REFERENCE_MISMATCH`. Wrong kind (e.g. refund of a WIN) → `REFERENCE_KIND_INVALID`.

Insufficient funds:

- `BET` debit without balance → `INSUFFICIENT_FUNDS` (no ledger, no version bump).
- `ROLLBACK` of `WIN`/`REFUND` that would make the balance negative → `INSUFFICIENT_FUNDS_REVERSAL`
  (rejected, auditable, still no ledger).

`LOSS` and any `REJECTED` operation produce no ledger entry and do not increment wallet version.

## Consequences

- Positive: Duplicate return of the same BET debit is impossible in the domain; failure codes
  distinguish bet vs reversal shortages.
- Negative / accepted risks: lifetime “one refund per BET” even after rolling that refund back.
- Known limitations: persistence of the uniqueness constraint is Phase 3; pending-reference TTL is
  Phase 8.

## Verification

- `internal/domain/apply_test.go`: refund, rollback of bet/win/refund, mutual exclusion, duplicate
  same kind, reversal without funds, LOSS without ledger, opening metadata/events.
