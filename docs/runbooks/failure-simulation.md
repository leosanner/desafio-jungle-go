# Failure simulation

Crash windows, abrupt termination and dependency loss (`init.md` §13, ENT-07). Decision:
[ADR 0020](../adr/0020-multi-instance-and-failure-injection.md). Precise commit/publish gaps use
**tests-only hooks**; live processes are killed or have their containers stopped. Production
binaries do not ship crash flags.

## Prerequisites

[`test-dependencies.md`](test-dependencies.md). For process kills, run three instances as in
[`multiple-instances.md`](multiple-instances.md).

## Automated (hooks and SIGKILL)

These `go test -tags=integration` cases are the reproducible proof. They skip if required env is
unset; that skip is not a substitute for CI.

| Window | Test | Infra | Mechanism |
| --- | --- | --- | --- |
| Financial commit, SQS `DeleteMessage` not yet | `TestInboundRecoverCommitBeforeDelete` | Postgres + LocalStack | `Consumer.SetAfterCommit` skips delete; redelivery must not debit twice ([ADR 0018](../adr/0018-sqs-inbound-consume.md)) |
| SQS `SendMessage`, outbox `published_at` not yet | `TestOutboxRecoverPublishBeforeAck` | Postgres + LocalStack | `OutboxRelay.SetAfterPublish` skips ack; republish keeps `eventId` ([ADR 0016](../adr/0016-outbox-claim-and-backoff.md)) |
| Two publishers, one outbox | `TestOutboxTwoPublishersContend` | Postgres + LocalStack | Two relays, `SKIP LOCKED` |
| Committed `PENDING` resumed by a new service | `TestPendingCommittedPendingResumed` | Postgres | Insert `PENDING`, new `Service` + resumer |
| Live process `SIGKILL` after `PENDING_REFERENCE` | `TestInstancesKillPendingResumed` | Postgres + Keycloak + LocalStack | Three OS processes; kill one; remaining workers resume |

```sh
docker compose up -d postgres keycloak localstack
set -a && source .env.example && set +a
go test -tags=integration -count=1 ./internal/adapter/postgres/ -run 'TestInboundRecoverCommitBeforeDelete|TestOutboxRecoverPublishBeforeAck|TestOutboxTwoPublishersContend|TestPendingCommittedPendingResumed'
go test -tags=integration -count=1 -timeout 15m ./internal/composition/ -run TestInstancesKillPendingResumed
```

Hooks are nil in production wiring. Do not add `FAIL_AFTER_COMMIT` (or similar) environment
switches to the binary.

## Abrupt process death (`kill -9`)

Host processes from the multi-instance runbook (PIDs from the terminals or `pgrep -f cmd/wagering`):

```sh
kill -9 "$PID_OF_INSTANCE_A"
```

### After `PENDING_REFERENCE` (HTTP 202)

Full operator procedure: [`pending-references.md`](pending-references.md).

1. Open a wallet (100.00) on instance A.
2. `POST` a `REFUND` that references a BET that does not exist yet → `202` / `PENDING_REFERENCE`.
3. `kill -9` instance A immediately.
4. `POST` the BET on instance B.
5. Poll `GET /wagering/transactions/{id}` on instance C until `PROCESSED` (pending worker poll +
   lease). Balance returns to 100.00 (one debit, one credit). Same-key refund replay is
   `idempotentReplay: true`.

If A held the pending lease, wait up to `PENDING_LEASE` (30s in Compose / `.env.example`) before C
can claim the row.

### After a processed BET

1. `POST` a BET on A until `200`.
2. `kill -9` A.
3. Replay the same `Idempotency-Key` on B → same snapshot, one ledger debit, balance unchanged.

SIGTERM (graceful) is `kill "$PID"` / Compose `stop`. Readiness must go `503` as shutdown starts;
in-flight HTTP finishes or the stop deadline hits; inbound visibility is released
([ADR 0018](../adr/0018-sqs-inbound-consume.md)).

## PostgreSQL unavailable

With the app running:

```sh
docker compose stop postgres
curl -sS -o /tmp/ready.json -w "%{http_code}\n" http://localhost:8080/health/ready
cat /tmp/ready.json
```

Expected: `503` and a non-ok postgres check. A `POST /wagering/transactions` during the outage
must not persist a negative balance (the write never commits). Start Postgres again:

```sh
docker compose start postgres
```

Wait until `GET /health/ready` is `200`. Replay or new operations follow the usual idempotency
and `CHECK (balance_minor >= 0)` rules. `docker compose logs app` should show failed pings, not a
process crash loop that skips migrations incorrectly (the process stays up; ready fails).

## LocalStack / SQS unavailable

```sh
docker compose stop localstack
curl -sS -o /tmp/ready.json -w "%{http_code}\n" http://localhost:8080/health/ready
```

Expected: `503` (SQS check). HTTP financial commits can still succeed: outbox rows stay
unpublished (`published_at` null) and inbound receive fails. Start LocalStack:

```sh
docker compose start localstack
```

Wait for queue init (`awslocal sqs get-queue-url --queue-name wager-transactions.fifo`) and
`GET /health/ready` = `200`. Outbox workers publish due rows (backoff then success). Inbox
redelivery must not duplicate a debit (`TestInboundRecoverCommitBeforeDelete` is the automated
stand-in for “killed after commit, before delete”).

## How to verify afterwards

Always check **three** things: wallet `balance_minor`, ledger (Σ credits − Σ debits), transaction
statuses. HTTP:

```sh
curl -sS -X POST "http://localhost:8080/wallets/${WALLET_ID}/reconciliation" \
  -H "Authorization: Bearer $INTERNAL"
```

SQL:

```sh
psql "$POSTGRES_DSN" <<'SQL'
SELECT w.id, w.balance_minor,
       (SELECT COALESCE(SUM(CASE WHEN direction = 'CREDIT' THEN amount_minor ELSE -amount_minor END), 0)
        FROM wagering.wallet_ledger_entries e WHERE e.wallet_id = w.id) AS ledger_net
FROM wagering.wallets w;
SQL
```

`balance_minor` must equal `ledger_net`. No row in `wagering.wallets` may have `balance_minor < 0`.
