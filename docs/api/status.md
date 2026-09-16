# HTTP status catalog

Financial and transport statuses (`init.md` §9, [ADR 0013](../adr/0013-http-status-mapping.md)).
Auth 401/403 remain in [auth.md](auth.md). Health is in [health.md](health.md).

`Content-Type: application/json` on every body below.

## Success

| Status | When | Body |
| --- | --- | --- |
| `200` | `PROCESSED` operation (first time or idempotent replay) | [wagering.md](wagering.md) result; `idempotentReplay` true on replay |
| `200` | `GET` wallet, ledger page, transaction | Resource JSON |
| `201` | `POST /wallets` created | Wallet |
| `202` | `PENDING_REFERENCE` (reference missing or still pending) | Result with `status: "PENDING_REFERENCE"`; worker in [pending-references.md](../events/pending-references.md) |

## Error body

```json
{
  "error": "invalid",
  "message": "idempotency key is required",
  "failureCode": "MISSING_IDENTITY"
}
```

`failureCode` is omitted when the failure is not a domain code (auth, malformed JSON without a code).

| Status | `error` | When |
| --- | --- | --- |
| `400` | `invalid` | Malformed JSON, missing `Idempotency-Key`, domain `Validation` (not persisted) |
| `401` | `unauthenticated` | Missing/invalid Bearer ([auth.md](auth.md)) |
| `403` | `forbidden` | Authenticated but not allowed ([auth.md](auth.md)) |
| `404` | `not_found` | Unknown wallet/transaction, or provider `GET` of another provider's internal id |
| `409` | `conflict` | Duplicate `(playerId, currency)`; `IDEMPOTENCY_PAYLOAD_CONFLICT`; `DUPLICATE_EXTERNAL_TRANSACTION` |
| `422` | — (success-shaped body) | Persisted `REJECTED` with `failureCode`; replay of that row also `422` |
| `503` | `unavailable` | Retryable database error or IdP JWKS failure |

`422` body is the operation result, not the error envelope:

```json
{
  "transactionId": "0192f298-345e-7e38-af88-e43f851a819d",
  "status": "REJECTED",
  "failureCode": "INSUFFICIENT_FUNDS",
  "idempotentReplay": false
}
```

`balance` is present on `PROCESSED` (and on replay of `PROCESSED`) from the snapshot stored at
processing time. It is omitted when the transaction has no `ResultBalance` (`REJECTED`,
`PENDING_REFERENCE`).
