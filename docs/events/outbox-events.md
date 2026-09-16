# Outbox event types (domain)

Concrete event types from `internal/domain` (`init.md` §11). Envelope fields `eventId`,
`correlationId` and `causationId` are assigned when the outbox row is written
([ADR 0014](../adr/0014-outbox-persistence.md)). Constructors set `eventType` and `version`
(currently `1`). Timestamps are UTC RFC 3339. Money in JSON is the string contract from
[ADR 0006](../adr/0006-money-representation.md). `data` uses camelCase field names.

| `eventType` | Trigger | Aggregate (`aggregateId`) |
| --- | --- | --- |
| `WagerTransactionProcessed` | Successful completion, including `LOSS` and internal `OPENING` | transaction id |
| `WagerTransactionRejected` | Definitive business rejection | transaction id |
| `WalletBalanceChanged` | Effective balance change | wallet id |
| `WagerTransactionPendingReference` | Wait for a missing or still-pending reference | transaction id |

`WalletBalanceChanged.data` includes `walletId`, `transactionId`, `direction`, `money`,
`balanceBefore`, `balanceAfter`, `walletVersion`.

Zero-balance wallet opening and `LOSS` do **not** emit `WalletBalanceChanged`. Rows are inserted
unpublished (`published_at` NULL) in the same SQL commit as the domain write.

## Destination and consumption ([ADR 0015](../adr/0015-outbox-destination-and-routing.md))

Published to FIFO `wager-events.fifo` (`SQS_EVENTS_QUEUE_NAME`).

| SQS attribute | Value |
| --- | --- |
| `MessageGroupId` | `aggregateId` |
| `MessageDeduplicationId` | `eventId` |

Delivery is **at-least-once**. Consumers must treat `eventId` as the idempotency key. FIFO's
5-minute deduplication window is extra, not the application guarantee. There is no events DLQ;
failed `SendMessage` stays in PostgreSQL with backoff
([ADR 0016](../adr/0016-outbox-claim-and-backoff.md)).

Wire body example:

```json
{
  "eventId": "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1",
  "eventType": "WalletBalanceChanged",
  "aggregateId": "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a2",
  "correlationId": "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a3",
  "causationId": "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a4",
  "occurredAt": "2026-09-16T12:00:00Z",
  "version": 1,
  "data": {
    "walletId": "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a2",
    "transactionId": "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a4",
    "direction": "CREDIT",
    "money": {"amount": "100.00", "currency": "BRL"},
    "balanceBefore": {"amount": "0.00", "currency": "BRL"},
    "balanceAfter": {"amount": "100.00", "currency": "BRL"},
    "walletVersion": 1,
    "occurredAt": "2026-09-16T12:00:00Z"
  }
}
```
