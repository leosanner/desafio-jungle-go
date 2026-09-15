# Outbox event types (domain)

Concrete event types from `internal/domain` (`init.md` §11). Envelope fields `eventId`,
`correlationId` and `causationId` are assigned when the outbox row is written (later phase).
Constructors set `eventType` and `version` (currently `1`). Timestamps are UTC. Money in JSON is
the string contract from [ADR 0006](../adr/0006-money-representation.md).

| `eventType` | Trigger | Aggregate |
| --- | --- | --- |
| `WagerTransactionProcessed` | Successful completion, including `LOSS` and internal `OPENING` | transaction |
| `WagerTransactionRejected` | Definitive business rejection | transaction |
| `WalletBalanceChanged` | Effective balance change | wallet |
| `WagerTransactionPendingReference` | Wait for a missing or still-pending reference | transaction |

`WalletBalanceChanged.data` includes `walletId`, `transactionId`, `direction`, `money`,
`balanceBefore`, `balanceAfter`, `walletVersion`.

Zero-balance wallet opening and `LOSS` do **not** emit `WalletBalanceChanged`. Destination, routing
and at-least-once consumption remain TBD (outbox phase).
