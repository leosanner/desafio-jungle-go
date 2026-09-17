# 0015 — Outbox destination and routing

- **Status:** Accepted
- **Date:** 2026-09-16
- **Related requirements:** `init.md` §11; matrix IDs OBX-02, OBX-04, OBX-06, HTTP-13

## Context

Phase 5 persists unpublished `wagering.outbox_events` rows in the same commit as domain writes
([ADR 0014](0014-outbox-persistence.md)). Phase 6 must provision a destination, document routing and
consumption, and publish the OBX-06 envelope. Inbound `wager-transactions.fifo` is reserved for the
SQS consumer (Phase 7). LocalStack already hosts SQS.

## Options considered

### Option A — Dedicated SQS FIFO queue `wager-events.fifo`

`MessageGroupId` = `aggregateId`, `MessageDeduplicationId` = `eventId`. Explicit dedup (no
content-based deduplication). One queue for all four event types; `eventType` is in the envelope.

- Pros: Fits the existing LocalStack setup; per-aggregate order; republish after send-without-ack
  collapses inside the FIFO 5-minute window; readiness can `GetQueueUrl` the same way as inbound.
- Cons: FIFO 5-minute dedup is not the application guarantee; consumers still key on `eventId`.

### Option B — SNS topic with SQS subscriptions

- Pros: Fan-out to many consumers without changing the publisher.
- Cons: Extra AWS surface; no second consumer in this challenge; more Compose/LocalStack wiring.

### Option C — One FIFO queue per `eventType`

- Pros: Consumers subscribe only to the types they need.
- Cons: Four queues to provision and probe; routing logic in the publisher; no consumer split yet.

## Decision

**Option A.**

- Queue name from required env `SQS_EVENTS_QUEUE_NAME` (Compose/example: `wager-events.fifo`).
- FIFO, `ContentBasedDeduplication` off. Every `SendMessage` sets `MessageGroupId` to the row's
  `aggregate_id` and `MessageDeduplicationId` to `eventId`.
- Body is the JSON envelope: `eventId`, `eventType`, `aggregateId`, `correlationId`, optional
  `causationId`, `occurredAt` (UTC RFC 3339), `version`, `data` (immutable snapshot from `payload`).
- Delivery is **at-least-once**. Consumers must ignore duplicate `eventId`s. FIFO dedup is extra.
- No events DLQ in this phase: a failed `SendMessage` stays in PostgreSQL with backoff
  ([ADR 0016](0016-outbox-claim-and-backoff.md)).
- `/health/ready` requires the events queue to exist, in addition to the inbound wager queue and DLQ.

## Consequences

- Positive: Destination is provisioned independently of inbound consume; `eventId` is stable across
  republish; health fails fast if LocalStack did not create the queue.
- Negative / accepted risks: A consumer that does not dedup by `eventId` can double-handle after the
  FIFO window, or if the publisher uses a non-FIFO destination later.
- Known limitations: No fan-out, no per-type queues, no events DLQ, no inbound consumer.

## Verification

- LocalStack init creates `wager-events.fifo`; config rejects a missing `SQS_EVENTS_QUEUE_NAME`.
- Integration: relay `SendMessage` then `ReceiveMessage` yields the envelope with the stored `eventId`.
- Republish of the same `eventId` within the FIFO window does not create a second visible message.
