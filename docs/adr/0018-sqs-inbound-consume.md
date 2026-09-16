# 0018 — SQS inbound consume, retry, DLQ and shutdown

- **Status:** Proposed
- **Date:** 2026-09-16
- **Related requirements:** `init.md` §10; matrix IDs SQS-01..08, AUTH-06, FX-03/04, TST-05, TST-12, TST-16

## Context

Queues `wager-transactions.fifo` and `wager-transactions-dlq.fifo` are already provisioned with
redrive. Phase 7 must consume `WagerTransactionRequested`, share HTTP use cases and idempotency,
delete only after a durable commit, retry transients with backoff, send permanent/exhausted work to
the DLQ, and stop fetching on SIGTERM. Inbox identity and hash are [ADR 0017](0017-inbox-persistence.md).

SQS is not an HTTP identity: broker access is AWS credentials ([ADR 0011](0011-oidc-keycloak-auth.md)).
`data.providerId` is the financial provider of the operation, not a JWT `azp`.

## Options considered

### Option A — Long-poll worker, explicit DLQ for permanent errors, visibility backoff for transients

Receive one message (`MaxNumberOfMessages=1`), parse, `HandleInbound`, then:

| Outcome | SQS action |
| --- | --- |
| Processed, REJECTED, PENDING_REFERENCE, replay, inbox already completed | `DeleteMessage` after commit |
| Malformed envelope, hash mismatch, validation, idempotency/external-id conflict | `SendMessage` to the DLQ then delete from the main queue |
| `ErrNotFound` (wallet), `ErrUnavailable`, other infrastructure | `ChangeMessageVisibility` to exponential backoff; do not delete. Redrive (`maxReceiveCount=5`) exhausts to the DLQ |

Injectable `AfterCommit` hook (tests only) can skip delete (TST-12). On `OnStop`, cancel receive;
in-flight work uses a context bounded by visibility timeout. If the stop deadline hits first,
`ChangeMessageVisibility` to 0.

- Pros: Permanent poison does not wait for five receives; transients back off without a global lock;
  FIFO group order is preserved by receiving one message and not deleting until done.
- Cons: Explicit DLQ send duplicates what redrive would eventually do; operators must size
  `FX_STOP_TIMEOUT` around `SQS_VISIBILITY_TIMEOUT`.

### Option B — Never send to the DLQ; only rely on redrive

- Pros: Fewer SQS calls.
- Cons: Invalid JSON is retried until `maxReceiveCount`; slower poison handling; harder to test DLQ
  without waiting for five receives.

### Option C — Batch receive and concurrent handlers

- Pros: Higher throughput.
- Cons: Two in-flight messages from different groups is fine, but a batch from one FIFO group must
  stay ordered; extra complexity for Phase 7.

## Decision

**Option A.**

**Envelope:** `messageId`, `type=WagerTransactionRequested`, `occurredAt`, `data` with the HTTP
business fields plus `idempotencyKey`. Canonical idempotency hash is `domain.HashCanonicalPayload`
on `data` (key excluded from the hash, [ADR 0012](0012-idempotency-canonical-hash.md)). Inbox hash
is SHA-256 of the raw body.

**FIFO (producers):**

| Attribute | Value |
| --- | --- |
| `MessageGroupId` | `walletId` (per-wallet order at the broker; independent wallets proceed in parallel) |
| `MessageDeduplicationId` | Envelope `messageId` |

Content-based deduplication stays enabled on the inbound queues as an extra 5-minute window.
Application dedup is the inbox plus `(provider_id, idempotency_key)` — tests that prove it must use
a distinct `MessageDeduplicationId` or the commit-without-delete path so FIFO is not the only
filter.

**Queue attributes (LocalStack init):** `VisibilityTimeout=30` seconds; redrive
`maxReceiveCount=5` to `wager-transactions-dlq.fifo`.

**Process config:** `SQS_VISIBILITY_TIMEOUT`, `SQS_WAIT_TIME` (long poll, ≤20s),
`SQS_BACKOFF_MAX`. Backoff is `min(SQS_BACKOFF_MAX, 1s << (receiveCount-1))` via
`ChangeMessageVisibility`.

**Invalid messages:** missing `messageId`/`type`/`data`, wrong type, unparseable JSON or money →
DLQ. DLQ `MessageGroupId` is the original group or `invalid`; `MessageDeduplicationId` is the
envelope `messageId` or the body hash.

**Auth:** `HandleInbound` builds `Actor{ProviderID: data.providerId}` so `Submit` authorization
passes. This is queue-gated identity, not OIDC.

**Shutdown:** stop long-poll; finish or release the in-flight receipt; worker `done` is observable.

## Consequences

- Positive: SQS-04/05/07 are explicit; HTTP and SQS share `submitInTx`; N instances compete only via
  SQS visibility and PostgreSQL wallet locks (no global mutex).
- Negative / accepted risks: At-least-once plus FIFO 5-minute dedup can still deliver twice after
  the window; inbox is the durable guard. A DLQ send can itself fail — then the message stays
  visible and redrive eventually copies it.
- Known limitations: Metrics (retries, DLQ counts) are Phase 10. ≥3 OS processes remain Phase 9.

## Verification

- Unit: parse table; disposition table (ack / retry / DLQ); worker `done` on stop.
- Integration (PostgreSQL + LocalStack): happy-path debit; TST-12 skip-delete redelivery; invalid
  body on the DLQ; HTTP and SQS for the same key produce one debit; duplicate SQS copies with
  distinct FIFO dedup ids still one debit.
