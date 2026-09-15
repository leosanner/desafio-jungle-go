# Message contracts

Incoming messages via SQS (`init.md` §10) and outgoing events published by the outbox (§11).

## Provisioned queues (Phase 1)

Docker Compose / LocalStack provision these FIFO queues, including redrive from the main queue to
the DLQ. Names are also `SQS_WAGER_QUEUE_NAME` and `SQS_WAGER_DLQ_NAME`.

| Queue | Role |
| --- | --- |
| `wager-transactions.fifo` | Inbound `WagerTransactionRequested` (consumer not implemented in Phase 1) |
| `wager-transactions-dlq.fifo` | Dead-letter queue for exhausted / permanent failures |

The application’s readiness probe checks that both queues exist. Visibility timeout, max receive
count, `MessageGroupId`, `MessageDeduplicationId` and payload contracts are **TBD** (later ADR).

## Expected contents

### Input

- `WagerTransactionRequested` envelope and `data` fields.
- Queues, redrive and DLQ; `MessageGroupId` and `MessageDeduplicationId`.
- Visibility timeout, attempt limits, backoff and invalid message handling.
- Delete/retry/DLQ policy per outcome category.

### Output

- Common envelope: `eventId`, `eventType`, `aggregateId`, `correlationId`, `causationId`, `occurredAt`,
  `version`, `data`.
- Typed payload for each event: `WagerTransactionProcessed`, `WagerTransactionRejected`,
  `WalletBalanceChanged`, `WagerTransactionPendingReference` — types in
  [`outbox-events.md`](outbox-events.md); routing still TBD.
- Provisioned destination, routing, ordering and consumer guarantees (at-least-once, dedup by `eventId`).
- Event versioning policy.

## Index

| Document | Messages |
| --- | --- |
| This README | Queue names `wager-transactions.fifo`, `wager-transactions-dlq.fifo` (provisioned) |
| [outbox-events.md](outbox-events.md) | Domain event types (`WagerTransactionProcessed`, `Rejected`, `WalletBalanceChanged`, `PendingReference`) |
| _payload contracts to be created_ | Inbound `WagerTransactionRequested` envelope and outbox routing |
