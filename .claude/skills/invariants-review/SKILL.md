---
name: invariants-review
description: Review focused on the challenge's financial and distributed guarantees — float-free money, append-only ledger, persistent idempotency, per-wallet concurrency, inbox/outbox, failure recovery and per-provider authorization. Use when reviewing diffs or PRs touching balances, transactions, migrations, consumers, publishers or auth.
---

# Invariants review

Goal: find violations of the disqualifying criteria and of the guarantees in `init.md` §5 **before**
commit. Report only concrete problems, with file:line and a failure scenario
("two instances receive X at the same time → Y").

## How to run it

1. Get the diff (`git diff`, `git diff --staged` or the given target).
2. For each hunk, identify which sections below apply.
3. For each applicable item, reason through an explicit adversarial scenario (duplicate, race,
   crash between steps, database/SQS unavailable, token from another provider).
4. Classify: **DISQUALIFYING**, **SEVERE** (breaks a guarantee), **IMPROVEMENT**.

## Checklist

### Money
- [ ] No `float32`/`float64`/`ParseFloat`/JSON numbers as floats in monetary paths
- [ ] Rejects empty, `NaN`, `Infinity`, scientific notation, scale > 2, negative external input
- [ ] No silent rounding; overflow handled (if `int64`)
- [ ] Operations require the same currency; persistence preserves amount and currency exactly

### Ledger and balance
- [ ] Every balance change has a ledger entry in the **same commit**
- [ ] `balanceAfter = balanceBefore ± amount` validated in the constructor and in the database
- [ ] No `UPDATE`/`DELETE` on the ledger; database prevents it (trigger + grants)
- [ ] `LOSS` and rejections create no entry and don't bump the version
- [ ] Version increments only when the balance changes

### Concurrency
- [ ] Per-wallet coordination (row lock, conditional update, or optimistic with bounded retry)
- [ ] No global mutex, global advisory lock or serialization of all wallets
- [ ] No read-modify-write outside a transaction/lock (lost update)
- [ ] Consistent lock acquisition order (avoid deadlocks with reference + wallet)
- [ ] Nothing relies on in-memory state or a single instance

### Idempotency
- [ ] Key and hash persisted in the same transaction as the effect
- [ ] Same key + same hash → persisted result, `idempotentReplay: true`, original balance
- [ ] Same key + different hash → conflict
- [ ] `(providerId, externalTransactionId)` cannot be re-applied under another key
- [ ] Canonical hash identical for HTTP and SQS; excludes the key and transport metadata
- [ ] Server never silently replaces the received `Idempotency-Key`

### State machine and references
- [ ] Transitions validated in the domain; terminal states don't transition
- [ ] Committed `PENDING` has durable resumption by another instance
- [ ] `PENDING_REFERENCE` with persisted exponential backoff, max attempts/TTL → `REJECTED`
- [ ] Reference matches provider, player, wallet, currency, round and amount
- [ ] Two successful reversals of the same kind are impossible; REFUND/ROLLBACK combination never refunds twice
- [ ] Reversal without funds → auditable rejection with a `failureCode` distinct from a bet without funds
- [ ] `OPENING` rejected via HTTP/SQS

### Inbox / SQS
- [ ] Inbox, domain, ledger and outbox in the same SQL transaction
- [ ] Message deleted **only after** commit; committed business rejection → delete
- [ ] Transient failure → don't delete, backoff; permanent/exhausted → DLQ
- [ ] Envelope `messageId` as identity; hash checked on redelivery
- [ ] SIGTERM: stop fetching, finish or release visibility

### Outbox
- [ ] Event row in the same commit as the change; publishing only afterwards
- [ ] Safe concurrent claim (`FOR UPDATE SKIP LOCKED` or lease with expiry)
- [ ] Abandoned work recoverable; retry with backoff; stable `eventId` on republish
- [ ] Payload is an immutable snapshot; complete envelope; UTC RFC 3339 timestamps; money as strings

### Auth
- [ ] Business endpoint without a valid token → 401, no effect, no leak
- [ ] `providerId` comes from the identity, not trusted from body/path without checking
- [ ] A provider can't read or replay another provider's transaction (including via idempotency)
- [ ] Wallet operations restricted to the internal service
- [ ] JWT validation: signature (JWKS), `iss`, `aud`, `exp`, allowed algorithm

### Observability and errors
- [ ] Logs without tokens, secrets or full financial payloads; with correlation IDs
- [ ] Relevant metrics incremented (status, duplicates, retries, DLQ, conflicts, reconciliation)
- [ ] Classifiable errors; no `panic` for business rules; `ctx` propagated

## Output

List ordered by severity. For each item: severity, `file:line`, violation, concrete scenario, suggested
fix. If nothing is found, state explicitly which sections were checked.
