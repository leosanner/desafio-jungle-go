# Wallets

Internal-client routes (`OIDC_INTERNAL_CLIENT`). [auth.md](auth.md). Statuses: [status.md](status.md).

## `POST /wallets`

Opens a wallet for `(playerId, currency)`. Currency comes from `initialBalance`.

```http
POST /wallets
Authorization: Bearer <internal>
Content-Type: application/json
```

```json
{
  "playerId": "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1",
  "initialBalance": { "amount": "1000.00", "currency": "BRL" }
}
```

`201`:

```json
{
  "id": "0192f291-27dd-7d3f-8071-5f8685deef37",
  "playerId": "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1",
  "balance": { "amount": "1000.00", "currency": "BRL" },
  "version": 1
}
```

Positive `initialBalance` creates an internal `OPENING` in `PROCESSED`, a credit ledger entry and
outbox rows for `WagerTransactionProcessed` and `WalletBalanceChanged` in the same commit. Version
is `1`. Zero `"0.00"` creates the wallet only (no `OPENING`, ledger or financial events). A second
wallet for the same player and currency is `409 conflict`.

## `GET /wallets/{walletId}`

`200` with the same wallet object. `404` if unknown.

## `GET /wallets/{walletId}/ledger`

```http
GET /wallets/{walletId}/ledger?cursor=...&limit=50
```

Stable order: `created_at ASC, id ASC`. `limit` defaults to `50`, maximum `100`. `cursor` is an
opaque `base64url` token of `created_at (RFC3339Nano)|id` from the last item of the previous page
(exclusive). Invalid cursor → `400`. Unknown wallet → `404`.

```json
{
  "items": [
    {
      "id": "...",
      "transactionId": "...",
      "direction": "CREDIT",
      "money": { "amount": "1000.00", "currency": "BRL" },
      "balanceBefore": { "amount": "0.00", "currency": "BRL" },
      "balanceAfter": { "amount": "1000.00", "currency": "BRL" },
      "createdAt": "2026-09-16T13:00:00.000000000Z"
    }
  ],
  "nextCursor": "..."
}
```

`nextCursor` is omitted when there is no further page.

## `POST /wallets/{walletId}/reconciliation`

Rebuilds the balance from the ledger on a consistent snapshot (`SELECT … FOR UPDATE` on the wallet
row plus an aggregate of its entries). Does **not** change the stored balance.

```json
{
  "walletId": "0192f291-27dd-7d3f-8071-5f8685deef37",
  "storedBalance": { "amount": "975.00", "currency": "BRL" },
  "calculatedBalance": { "amount": "975.00", "currency": "BRL" },
  "difference": { "amount": "0.00", "currency": "BRL" },
  "consistent": true,
  "checkedEntries": 2
}
```

`difference` is stored minus calculated. Divergence is returned as `consistent: false`, logged
(without a full financial payload) and counted on a metric. `404` if the wallet does not exist.
