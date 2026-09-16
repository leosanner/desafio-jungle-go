# 0013 — HTTP status and error-body mapping

- **Status:** Proposed
- **Date:** 2026-09-16
- **Related requirements:** `init.md` §9; matrix IDs HTTP-10, OPS-13, AUTH-04, ELIM-01, ELIM-02

## Context

Adapters map classifiable domain errors to HTTP. Auth already uses `error` + `message` for 401/403/400/503
([`docs/api/auth.md`](../api/auth.md)). Financial outcomes need distinguishable statuses for invalid
input, conflict, persisted business rejection, pending processing and transient unavailability.

Domain classes ([ADR 0007](0007-wager-transaction-state-machine.md)): `Validation` (usually not
persisted), `Rejection` (row `REJECTED` + `failureCode`), `Wait` (`PENDING_REFERENCE`),
`Transient` (retry / 503).

## Options considered

### Option A — Status from outcome class; `failureCode` on the body when present

- Pros: Clients can branch on HTTP status without parsing `status` first; still echo `failureCode`.
- Cons: Replay of `REJECTED` returns 422, not 200 — clients must treat 422 as a recorded outcome.

### Option B — Always 200 for any persisted transaction; distinguish only in JSON `status`

- Pros: Matches some wallet-provider APIs.
- Cons: Conflicts with the requirement that invalid / conflict / rejection / pending / unavailable
  be distinguishable by the contract at HTTP level.

## Decision

**Option A.** Catalog:

| Outcome | HTTP | `error` | Notes |
| --- | --- | --- | --- |
| Wallet created | `201` | — | Body is the wallet |
| Operation `PROCESSED` (first or replay) | `200` | — | `idempotentReplay` true on replay; original `ResultBalance` |
| Operation `REJECTED` (first or replay) | `422` | — | JSON `status` + `failureCode`; recorded outcome |
| Operation `PENDING_REFERENCE` | `202` | — | Worker/TTL: [ADR 0019](0019-pending-reference-resume.md) |
| Validation / malformed JSON / missing `Idempotency-Key` | `400` | `invalid` | No financial row |
| Duplicate wallet `(playerId, currency)` | `409` | `conflict` | |
| Idempotency payload conflict / external id under another key | `409` | `conflict` | `failureCode` set |
| Wallet or transaction not found | `404` | `not_found` | Other-provider `GET /wagering/transactions/{id}` is also 404 (no existence leak). Wrong `{providerId}` on the provider path stays **403** (auth gate). |
| Unauthenticated | `401` | `unauthenticated` | Unchanged |
| Forbidden | `403` | `forbidden` | Unchanged; generic message |
| Transient (DB retryable, IdP JWKS down) | `503` | `unavailable` | |

Error body:

```json
{"error":"invalid","message":"...","failureCode":"INVALID_AMOUNT"}
```

`failureCode` is omitted when there is no domain code (pure auth/transport). Success bodies stay as
in `init.md` §9. Replay of a recorded rejection uses 422 and `idempotentReplay: true`.

## Consequences

- Positive: HTTP-10 is auditable from one table; adapters map `domain.Classify` without string match.
- Negative / accepted risks: a client that treats any non-2xx as “retry with same key” would retry
  422 forever — 422 is definitive; retries must replay, not re-apply.
- Known limitations: metrics per status are Phase 10. SQS mapping is a later ADR.

## Verification

- [`docs/api/status.md`](../api/status.md) and [`docs/api/failure-codes.md`](../api/failure-codes.md).
- HTTP tests: 400 / 409 / 422 / 202 / 404 / 503 as in the table; 401/403 unchanged.
