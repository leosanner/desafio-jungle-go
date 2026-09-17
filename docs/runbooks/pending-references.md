# Pending references

Operator procedure for `PENDING` / `PENDING_REFERENCE` (`init.md` §7 OPS-10..12, TST-14/15).
Decision: [ADR 0019](../adr/0019-pending-reference-resume.md). Message contract:
[`docs/events/pending-references.md`](../events/pending-references.md).

The inbound SQS message is already deleted after the wait is committed. Continuity is a SQL
worker (`SKIP LOCKED`), not a queue consumer.

## Prerequisites

[`test-dependencies.md`](test-dependencies.md). Host `.env.example` values if the binary runs
on the host (issuer `http://localhost:8081/...`). Compose app processes use
`http://keycloak:8080/realms/wagering` — do not mix host-issued tokens with those processes.

Default knobs (`.env.example` / Compose): `PENDING_POLL_INTERVAL=250ms`, `PENDING_BATCH_SIZE=10`,
`PENDING_LEASE=30s`, `PENDING_BACKOFF_MAX=1m`, `PENDING_MAX_ATTEMPTS=8`, `PENDING_TTL=5m`.

## Automated proof

Tagged tests need PostgreSQL only (`POSTGRES_DSN`). They skip if it is unset; that skip is not a
substitute for CI.

```sh
docker compose up -d postgres
set -a && source .env.example && set +a
go test -tags=integration -count=1 ./internal/adapter/postgres/ -run 'TestPending'
```

| Test | What it shows |
| --- | --- |
| `TestPendingClaimSkipLocked` | Two resumers; `SKIP LOCKED` gives each a different due row |
| `TestPendingRefundResolvesWhenBetArrives` | `REFUND` before `BET` → `PENDING_REFERENCE`; BET then resume → `PROCESSED`, balance restored |
| `TestPendingRefundExpiresReferenceNotFound` | Exhausted attempts/TTL → `REJECTED` / `REFERENCE_NOT_FOUND` |
| `TestPendingCommittedPendingResumed` | Inserted `PENDING` resumed by a **new** `Service` (restart / other instance) |
| `TestPendingTwoResumersOneRefund` | Two workers, one waiter: a single financial outcome |
| `TestPendingLeaseHidesRow` | A leased row is invisible to the other resumer until the lease expires |

Process-level `SIGKILL` after `PENDING_REFERENCE`: `TestInstancesKillPendingResumed`
([`failure-simulation.md`](failure-simulation.md), [`multiple-instances.md`](multiple-instances.md)).

## Manual: refund before its bet (resolution)

Start the stack (`docker compose up --build` or host `go run ./cmd/wagering` against Compose
dependencies). Tokens from Keycloak on **8081** only work with a host binary; for the Compose
`app` service, obtain the token inside the network (see [auth.md](../api/auth.md)).

```sh
INTERNAL=$(curl -sS -X POST http://localhost:8081/realms/wagering/protocol/openid-connect/token \
  -d grant_type=client_credentials -d client_id=wagering-internal -d client_secret=internal-secret \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')
PROVIDER=$(curl -sS -X POST http://localhost:8081/realms/wagering/protocol/openid-connect/token \
  -d grant_type=client_credentials -d client_id=provider-a -d client_secret=provider-a-secret \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')

WALLET=$(curl -sS -X POST http://localhost:8080/wallets \
  -H "Authorization: Bearer $INTERNAL" -H "Content-Type: application/json" \
  -d '{"playerId":"player-pending-1","initialBalance":{"amount":"100.00","currency":"BRL"}}')
WALLET_ID=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"$WALLET")
PLAYER=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["playerId"])' <<<"$WALLET")
```

Post a `REFUND` of a BET that does not exist yet. Expect HTTP `202` and `PENDING_REFERENCE`.

```sh
HTTP=$(curl -sS -o /tmp/refund.json -w "%{http_code}" -X POST http://localhost:8080/wagering/transactions \
  -H "Authorization: Bearer $PROVIDER" -H "Content-Type: application/json" \
  -H "Idempotency-Key: provider-a:refund-before-bet" \
  -d "{\"providerId\":\"provider-a\",\"externalTransactionId\":\"refund-before-bet\",\"playerId\":\"${PLAYER}\",\"walletId\":\"${WALLET_ID}\",\"roundId\":\"round-pending\",\"gameId\":\"game-1\",\"kind\":\"REFUND\",\"money\":{\"amount\":\"25.00\",\"currency\":\"BRL\"},\"referenceExternalTransactionId\":\"bet-later\"}")
echo "$HTTP"
cat /tmp/refund.json
TX_ID=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["transactionId"])' </tmp/refund.json)
```

`$HTTP` must be `202`. Balance is still `100.00` (no ledger yet). Then post the BET:

```sh
curl -sS -X POST http://localhost:8080/wagering/transactions \
  -H "Authorization: Bearer $PROVIDER" -H "Content-Type: application/json" \
  -H "Idempotency-Key: provider-a:bet-later" \
  -d "{\"providerId\":\"provider-a\",\"externalTransactionId\":\"bet-later\",\"playerId\":\"${PLAYER}\",\"walletId\":\"${WALLET_ID}\",\"roundId\":\"round-pending\",\"gameId\":\"game-1\",\"kind\":\"BET\",\"money\":{\"amount\":\"25.00\",\"currency\":\"BRL\"}}"
```

Wait for the pending worker (poll interval + one Apply). Poll until `PROCESSED`:

```sh
curl -sS "http://localhost:8080/wagering/transactions/${TX_ID}" \
  -H "Authorization: Bearer $PROVIDER"
```

Expected: refund `PROCESSED`; wallet `100.00` (BET debit + REFUND credit); ledger has both
movements. Same-key refund replay returns `idempotentReplay: true`.

If the BET is posted on another instance (or after `kill -9` of the instance that accepted the
refund), the remaining workers resume the same row. If that instance still holds the lease, wait
up to `PENDING_LEASE` (30s by default).

## Manual: expiry (`REFERENCE_NOT_FOUND`)

Restart a **host** process with a short TTL so the wait is observable without five minutes of
idling. Dependencies stay up:

```sh
docker compose up -d postgres keycloak localstack
set -a && source .env.example && set +a
PENDING_TTL=15s PENDING_MAX_ATTEMPTS=2 PENDING_POLL_INTERVAL=250ms go run ./cmd/wagering
```

Open a wallet, post a `REFUND` whose reference never arrives, then poll:

```sh
curl -sS "http://localhost:8080/wagering/transactions/${TX_ID}" \
  -H "Authorization: Bearer $PROVIDER"
```

Expected: `REJECTED` with `failureCode` `REFERENCE_NOT_FOUND`. Wallet balance unchanged. Outbox
has `WagerTransactionRejected`. Replay of the same idempotency key returns that rejection
(`idempotentReplay: true`). A `BET` that arrives **after** expiry does not revive the refund;
the BET is a new operation.

A reference that is already `REJECTED` / `FAILED` rejects the waiter with
`REFERENCE_UNSUCCESSFUL`, not `REFERENCE_NOT_FOUND` (see [ADR 0007](../adr/0007-wager-transaction-state-machine.md)).

## How to verify in SQL

```sh
psql "$POSTGRES_DSN" <<SQL
SELECT external_transaction_id, status, failure_code, attempt_count, next_attempt_at
FROM wagering.wager_transactions
WHERE wallet_id = '${WALLET_ID}'
ORDER BY created_at, id;

SELECT direction, amount_minor, balance_after_minor
FROM wagering.wallet_ledger_entries
WHERE wallet_id = '${WALLET_ID}'
ORDER BY created_at, id;

SELECT id, balance_minor FROM wagering.wallets WHERE id = '${WALLET_ID}';
SQL
```

`balance_minor` must equal Σ credits − Σ debits. A waiting refund has no ledger row of its own
until it processes.

Reconciliation:

```sh
curl -sS -X POST "http://localhost:8080/wallets/${WALLET_ID}/reconciliation" \
  -H "Authorization: Bearer $INTERNAL"
```

`consistent` must be true.

## Shutdown

SIGTERM cancels the poll. The in-flight batch finishes within `PENDING_LEASE`, or the stop
deadline (`FX_STOP_TIMEOUT`) fails. Abrupt death: [`failure-simulation.md`](failure-simulation.md).
