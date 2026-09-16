# 0012 — Canonical idempotency hash

- **Status:** Proposed
- **Date:** 2026-09-16
- **Related requirements:** `init.md` §9 (hash, replay, conflict); matrix IDs HTTP-07, HTTP-08, HTTP-09, GAR-02, ELIM-06, SQS-02

## Context

External operations carry an `Idempotency-Key` (HTTP header; later `data.idempotencyKey` on SQS).
The same key with the same business payload must replay the persisted result. The same key with a
different payload is a conflict. `(providerId, externalTransactionId)` cannot be re-applied under
another key. HTTP and SQS must hash equivalent payloads identically. The challenge requires
canonical JSON with sorted keys, excluding the idempotency key and transport metadata.

Conflict *rules* already live in `domain.CheckIdempotencyReplay` / `CheckExternalIdentity`. This ADR
fixes algorithm, fields and normalizations.

Key scope is already in the schema: unique `(provider_id, idempotency_key)` for `origin = EXTERNAL`
(`wager_transactions_provider_idempotency_key_uidx`). The server never replaces a received key with
one it computed.

## Options considered

### Option A — SHA-256 of `encoding/json` map (sorted object keys)

Build a `map[string]any` of the business fields and `json.Marshal` it. Go sorts map keys
alphabetically, so the bytes are deterministic. Hash with SHA-256, store lowercase hex.

- Pros: Stdlib only; same function for HTTP and SQS; matches “JSON with sorted keys”.
- Cons: Not RFC 8785 JCS (no Unicode escaping rules). Nested objects must also be maps so their
  keys sort. Field set must be listed explicitly.

### Option B — RFC 8785 JCS library

- Pros: Standard canonicalization.
- Cons: Extra dependency; overkill when parse already rejects non-canonical money.

### Option C — Struct field order as canonical JSON

- Pros: Simple marshal of a DTO.
- Cons: Key order depends on struct layout; easy to break when adding a field; not “sorted keys”.

## Decision

**Option A.** `domain.HashCanonicalPayload` SHA-256-hex-encodes this JSON object (keys sorted by
`encoding/json`):

| Field | Rule |
| --- | --- |
| `providerId` | Required string |
| `externalTransactionId` | Required string |
| `playerId` | Required string |
| `walletId` | Required string |
| `roundId` | Required string |
| `gameId` | Required string |
| `kind` | `BET` / `WIN` / `LOSS` / `REFUND` / `ROLLBACK` |
| `money` | Object `{"amount":"<digits>.dd","currency":"AAA"}` — canonical form from [ADR 0006](0006-money-representation.md). Nested keys also sorted (`amount` then `currency`). |
| `referenceExternalTransactionId` | Included only when non-empty (REFUND/ROLLBACK and optional WIN reference) |

**Excluded:** `Idempotency-Key` / `idempotencyKey`, `Authorization`, correlation/request ids,
timestamps, internal `transactionId`, transport envelope fields (`messageId`, SQS attributes).

**Normalizations:** none beyond what parse already enforces. Money that is not the canonical
two-decimal string is rejected before hashing. Empty `referenceExternalTransactionId` is omitted so
absent and empty are equivalent. The server does not rewrite the caller's `Idempotency-Key`.

Replay: same key + same hash → persisted row, `idempotentReplay: true`, `ResultBalance` from
processing time. Same key + different hash → `IDEMPOTENCY_PAYLOAD_CONFLICT`. Same
`(providerId, externalTransactionId)` under another key → `DUPLICATE_EXTERNAL_TRANSACTION`.

## Consequences

- Positive: HTTP and SQS share one function; hash is stable across process restarts; no floats.
- Negative / accepted risks: adding a hashed field is a breaking change for in-flight retries with
  the old hash (acceptable; keys are short-lived).
- Known limitations: SQS adapter is Phase 7; it must call this same function.

## Verification

- Unit tests: identical maps → identical hex; excluded fields do not change the hash; empty vs
  omitted reference match; different amount/kind → different hash; hex is 64 lowercase chars.
- Integration: HTTP replay stores and compares `payload_hash`; payload conflict returns 409.
