# Wagering operations

Provider routes (and internal reads). [auth.md](auth.md). Statuses: [status.md](status.md).
Idempotency hash: [ADR 0012](../adr/0012-idempotency-canonical-hash.md).

## `POST /wagering/transactions`

```http
POST /wagering/transactions
Authorization: Bearer <provider>
Content-Type: application/json
Idempotency-Key: provider-a:transaction-123
```

`Idempotency-Key` is **required**. The server never replaces it with `{providerId}:{externalTransactionId}`.

```json
{
  "providerId": "provider-a",
  "externalTransactionId": "transaction-123",
  "playerId": "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1",
  "walletId": "0192f291-27dd-7d3f-8071-5f8685deef37",
  "roundId": "round-987",
  "gameId": "fortune-chimp",
  "kind": "BET",
  "money": { "amount": "25.00", "currency": "BRL" }
}
```

`providerId` must match the token `azp` when present; if omitted, `azp` is used. Internal clients
are forbidden (`403`). `OPENING` is rejected (`400`, `OPENING_NOT_ALLOWED`).

For `REFUND` / `ROLLBACK` (and optionally `WIN`), add `referenceExternalTransactionId`.

`200` after processing:

```json
{
  "transactionId": "0192f298-345e-7e38-af88-e43f851a819d",
  "status": "PROCESSED",
  "balance": { "amount": "975.00", "currency": "BRL" },
  "idempotentReplay": false
}
```

| Repeat | Result |
| --- | --- |
| Same key, same canonical hash | Persisted outcome, `idempotentReplay: true`, original `balance` even if the wallet moved later |
| Same key, different payload | `409` `IDEMPOTENCY_PAYLOAD_CONFLICT` |
| Same `(providerId, externalTransactionId)`, other key | `409` `DUPLICATE_EXTERNAL_TRANSACTION` |

`REJECTED` is `422` with `failureCode`. `PENDING_REFERENCE` is `202`; the pending-reference worker
resumes it ([pending-references.md](../events/pending-references.md)).

Canonical hash fields (sorted-key JSON, SHA-256 hex): `providerId`, `externalTransactionId`,
`playerId`, `walletId`, `roundId`, `gameId`, `kind`, `money.amount`, `money.currency`,
`referenceExternalTransactionId` (omitted if empty). Not hashed: the idempotency key and transport
headers.

## `GET /wagering/transactions/{transactionId}`

Any authenticated actor. A provider receives `404` for a transaction it does not own (including
unknown ids). Internal clients receive the row or `404` if missing.

## `GET /providers/{providerId}/wagering/transactions/{externalTransactionId}`

Internal, or the provider whose `azp` equals `{providerId}` (else `403` before the handler).
`404` if that provider has no such external id.

Transaction read body (pendencies and rejections):

```json
{
  "transactionId": "...",
  "providerId": "provider-a",
  "externalTransactionId": "transaction-123",
  "playerId": "...",
  "walletId": "...",
  "roundId": "round-987",
  "gameId": "fortune-chimp",
  "kind": "BET",
  "money": { "amount": "25.00", "currency": "BRL" },
  "referenceExternalTransactionId": "",
  "status": "REJECTED",
  "failureCode": "INSUFFICIENT_FUNDS",
  "balance": null,
  "createdAt": "2026-09-16T13:00:00Z",
  "updatedAt": "2026-09-16T13:00:00Z"
}
```

`balance` is the processing snapshot when `status` is `PROCESSED`; otherwise `null`.
