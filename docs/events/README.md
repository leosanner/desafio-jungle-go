# Message contracts

Incoming messages via SQS (`init.md` §10) and outgoing events published by the outbox (§11).

## Provisioned queues

Docker Compose / LocalStack provision these FIFO queues. Inbound redrive is from
`wager-transactions.fifo` to the DLQ. Names are `SQS_WAGER_QUEUE_NAME`, `SQS_WAGER_DLQ_NAME` and
`SQS_EVENTS_QUEUE_NAME`.

| Queue | Role |
| --- | --- |
| `wager-transactions.fifo` | Inbound `WagerTransactionRequested` ([inbound.md](inbound.md), [ADR 0018](../adr/0018-sqs-inbound-consume.md)) |
| `wager-transactions-dlq.fifo` | Dead-letter queue for exhausted / permanent inbound failures |
| `wager-events.fifo` | Outbound domain events from the outbox publisher ([ADR 0015](../adr/0015-outbox-destination-and-routing.md)) |

Readiness probes all three queues. Inbound visibility timeout, max receive count,
`MessageGroupId` / `MessageDeduplicationId`: [inbound.md](inbound.md). Outbound routing:
[`outbox-events.md`](outbox-events.md).

## Expected contents

### Input

See [inbound.md](inbound.md).

### Output

- Common envelope: `eventId`, `eventType`, `aggregateId`, `correlationId`, `causationId`, `occurredAt`,
  `version`, `data`.
- Typed payload for each event: `WagerTransactionProcessed`, `WagerTransactionRejected`,
  `WalletBalanceChanged`, `WagerTransactionPendingReference` — types and FIFO routing in
  [`outbox-events.md`](outbox-events.md).
- At-least-once; consumers dedup by `eventId`. Event version is set by the domain constructor (`1`).

## Index

| Document | Messages |
| --- | --- |
| This README | Queue names including `wager-events.fifo` |
| [inbound.md](inbound.md) | `WagerTransactionRequested` envelope, FIFO ids, delete/retry/DLQ |
| [outbox-events.md](outbox-events.md) | Domain event types, envelope example, SQS group/dedup ids |
