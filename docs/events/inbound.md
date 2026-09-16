# Inbound `WagerTransactionRequested`

HTTP and SQS share `app.Submit` / `HandleInbound` and `domain.HashCanonicalPayload`
([ADR 0012](../adr/0012-idempotency-canonical-hash.md)). SQS adds inbox identity
([ADR 0017](../adr/0017-inbox-persistence.md)) and the consume policy in
[ADR 0018](../adr/0018-sqs-inbound-consume.md).

## Queue

| Queue | Role |
| --- | --- |
| `wager-transactions.fifo` | Inbound operations (`SQS_WAGER_QUEUE_NAME`) |
| `wager-transactions-dlq.fifo` | Permanent / exhausted failures (`SQS_WAGER_DLQ_NAME`) |

Redrive: `maxReceiveCount=5`. Queue `VisibilityTimeout=30` seconds. Process env:
`SQS_VISIBILITY_TIMEOUT`, `SQS_WAIT_TIME` (long poll, max 20s), `SQS_BACKOFF_MAX`.

## Envelope

```json
{
  "messageId": "msg-123",
  "type": "WagerTransactionRequested",
  "occurredAt": "2026-09-08T12:00:00.000Z",
  "data": {
    "providerId": "provider-a",
    "externalTransactionId": "transaction-123",
    "idempotencyKey": "provider-a:transaction-123",
    "playerId": "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1",
    "walletId": "0192f291-27dd-7d3f-8071-5f8685deef37",
    "roundId": "round-987",
    "gameId": "fortune-chimp",
    "kind": "BET",
    "money": { "amount": "25.00", "currency": "BRL" }
  }
}
```

`data.idempotencyKey` is the HTTP `Idempotency-Key`. Optional
`referenceExternalTransactionId` on `data` for WIN/REFUND/ROLLBACK. `OPENING` is rejected.

Inbox identity is envelope `messageId` (not the SQS receipt). Inbox hash is SHA-256 of the **raw
body**. Financial idempotency hash is canonical JSON of `data` excluding `idempotencyKey`.

## FIFO attributes (producers)

| Attribute | Value |
| --- | --- |
| `MessageGroupId` | `walletId` |
| `MessageDeduplicationId` | Envelope `messageId` |

Content-based deduplication is an extra 5-minute window on the provisioned inbound queues.
Application dedup is the inbox plus `(provider_id, idempotency_key)`.

## Outcome → SQS

| Outcome | Action |
| --- | --- |
| PROCESSED, REJECTED, PENDING_REFERENCE, replay, inbox already completed | `DeleteMessage` after commit |
| Malformed JSON, wrong type, hash mismatch, validation, payload/external-id conflict | Send to DLQ, then delete |
| Wallet not found, deadlock / unavailable | `ChangeMessageVisibility` backoff; redrive after 5 receives |

`PENDING_REFERENCE` completes the inbound message; the pending-reference worker resumes the
transaction ([pending-references.md](pending-references.md), [ADR 0019](../adr/0019-pending-reference-resume.md)).

## Auth

Broker access is AWS credentials. `data.providerId` is the financial provider, not a JWT `azp`.

## Shutdown

Stop long-poll. Finish the in-flight message within `SQS_VISIBILITY_TIMEOUT`, or set visibility to 0
if `FX_STOP_TIMEOUT` hits first.
