# 0006 — Money representation

- **Status:** Accepted
- **Date:** 2026-09-15
- **Related requirements:** `init.md` §6.1; matrix IDs MON-01..07, GAR-01, ELIM-03, STK-05 (representation; DB mapping intended)

## Context

`Money` must be immutable, carry an ISO 4217 currency, use a fixed scale of two decimal places, and
never pass through `float32`/`float64` in parsing, arithmetic, JSON or persistence. The challenge
allows either `int64` minor units or an exact decimal library, and requires documented limits and
overflow handling when `int64` is chosen. External JSON is always
`{"amount":"25.00","currency":"BRL"}`.

## Options considered

### Option A — `int64` minor units (cents) plus a currency code

- Pros: No extra dependency; arithmetic is integer; overflow is explicit; maps 1:1 to PostgreSQL
  `BIGINT`; JSON amount stays a string; sufficient for two-decimal ISO 4217 in this challenge.
- Cons: Hard upper bound (`92233720368547758.07`); adding a third decimal scale later would be a
  breaking change; not suitable if the product later needed arbitrary-precision FX.

### Option B — Exact decimal library (`shopspring/decimal` or `cockroachdb/apd`)

- Pros: Larger range; easier to change scale later.
- Cons: Extra dependency in the domain; overflow/limits still need a policy; easy to accidentally
  round or use exponent forms; PostgreSQL mapping is less obvious than `BIGINT` cents.

### Option C — Domain `string` amount, parse at the edges

- Pros: Looks like the JSON contract.
- Cons: Arithmetic on strings is error-prone; invariants leak into every caller.

## Decision

`Money` is an immutable value object in `internal/domain`:

- Storage: `int64` **minor units** (cents) and a 3-letter uppercase ISO 4217 code. Scale is always 2.
- Zero value (`Money{}`) is uninitialized and rejected by every operation.
- External parse (`ParseMoney`) accepts only a non-negative decimal string with **exactly two**
  fractional digits, no leading `+`, no scientific notation, no `NaN`/`Infinity`, no extra scale, and
  no leading zeros in the integer part except `0`. Equivalent forms such as `"25"` or `"25.0"` are
  **rejected** (no silent rounding or normalization).
- Internal construction (`MoneyFromMinor`) may be negative, for differences and calculations.
  Wallet **balance** and external financial input stay non-negative.
- Add, subtract, negate and compare require the same currency and fail on `int64` overflow.
- JSON uses string `amount` only; a numeric JSON amount is rejected without decoding it as a float.
- **Persistence (Phase 3):** amount as `BIGINT` minor units and currency as `CHAR(3)` (or equivalent
  `TEXT` with a check). Not implemented in this phase.

Main scenarios may use only `BRL`; the type always carries currency, and tests cover mismatches
(`USD` vs `BRL`).

Canonical form for later idempotency hashing is the same as the external contract: two-decimal
string plus uppercase currency. Because parse already rejects non-canonical input, hash input needs
no extra amount normalization.

## Consequences

- Positive: GAR-01/ELIM-03 are structural; overflow is testable; DB mapping is predetermined.
- Negative / accepted risks: amounts above `int64` cents are rejected; that bound is far beyond
  wagering balances in this challenge.
- Known limitations: unit of work and the actual migration are Phase 3. Idempotency hash algorithm
  is a later ADR; only the canonical amount form is fixed here.

## Verification

- Unit tests in `internal/domain/money_test.go` (parse, scale, limits, invalid input, currencies,
  overflow, JSON).
- `grep` of `internal/domain` has no `float32`/`float64`/`ParseFloat`.
- Schema mapping verified when the financial migration lands (Phase 3).
