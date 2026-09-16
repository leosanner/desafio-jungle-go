# Outbox event types (domain)

Concrete event types from `internal/domain` (`init.md` §11). Envelope fields `eventId`,
`correlationId` and `causationId` are assigned when the outbox row is written
([ADR 0014](../adr/0014-outbox-persistence.md)). Constructors set `eventType` and `version`
(currently `1`). Timestamps are UTC. Money in JSON is the string contract from
[ADR 0006](../adr/0006-money-representation.md).

| `eventType` | Trigger | Aggregate |
| --- | --- | --- |
| `WagerTransactionProcessed` | Successful completion, including `LOSS` and internal `OPENING` | transaction |
| `WagerTransactionRejected` | Definitive business rejection | transaction |
| `WalletBalanceChanged` | Effective balance change | wallet |
| `WagerTransactionPendingReference` | Wait for a missing or still-pending reference | transaction |

`WalletBalanceChanged.data` includes `walletId`, `transactionId`, `direction`, `money`,
`balanceBefore`, `balanceAfter`, `walletVersion`.

Zero-balance wallet opening and `LOSS` do **not** emit `WalletBalanceChanged`. Rows are inserted
unpublished (`published_at` NULL) in the same SQL commit as the domain write. Destination, routing,
concurrent claim and at-least-once consumption remain TBD (publisher in Phase 6).
