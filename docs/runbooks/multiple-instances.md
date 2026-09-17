# Multiple instances

Prove per-wallet coordination with at least three independent processes (`init.md` §8, CON-02,
TST-11, ELIM-07). Decision: [ADR 0020](../adr/0020-multi-instance-and-failure-injection.md).
Locks live in PostgreSQL; instances do not share memory.

## Prerequisites

- Docker and Docker Compose
- Host Go 1.25+ if you run binaries outside Compose
- [`.env.example`](../../.env.example) (host values; dummy secrets only)

Start dependencies (and optionally the first app replica):

```sh
docker compose up -d postgres keycloak localstack
docker compose ps
```

Wait until Keycloak discovery and LocalStack queues respond. See
[`test-dependencies.md`](test-dependencies.md).

## Automated proof

Tagged tests spawn **three** `cmd/wagering` OS processes (own HTTP port, own pgx pool, own
memory) against an isolated database and unique FIFO queues:

```sh
docker compose up -d postgres keycloak localstack
set -a && source .env.example && set +a
go test -tags=integration -count=1 -timeout 15m ./internal/composition/ -run 'TestInstances'
```

| Test | What it shows |
| --- | --- |
| `TestInstancesThreeProcessesTwoBetsOnHundred` | Two 80.00 bets on 100.00 across instances; one processed, one `INSUFFICIENT_FUNDS`; balance 20.00; one debit; replays on a third instance do not change the outcome |
| `TestInstancesConcurrentSameBet` | Same bet 50× round-robin across three processes → one debit |
| `TestInstancesDistinctWalletsParallel` | Two wallets, two instances, both complete (no global lock) |
| `TestInstancesHTTPxSQSSameKeyOneDebit` | HTTP submit on one instance and SQS consume on the cluster → one debit |
| `TestInstancesKillPendingResumed` | `PENDING_REFERENCE` committed, `SIGKILL` that process, BET on another, remaining workers resume the refund |

Needs `POSTGRES_DSN`, `OIDC_ISSUER` and `AWS_ENDPOINT_URL` (skip-if-no-env is not a substitute for
CI). Default `go test ./...` does not run these tests.

## Three host processes

Use this when the binary runs on the host (issuer `http://localhost:8081/...`, not the Compose DNS
name). Terminals 2 and 3 keep the same env except `HTTP_ADDR`.

```sh
docker compose up -d postgres keycloak localstack
set -a && source .env.example && set +a

HTTP_ADDR=:8080 go run ./cmd/wagering
HTTP_ADDR=:8082 go run ./cmd/wagering
HTTP_ADDR=:8083 go run ./cmd/wagering
```

Ready:

```sh
curl -sS -o /dev/null -w "%{http_code}\n" http://localhost:8080/health/ready
curl -sS -o /dev/null -w "%{http_code}\n" http://localhost:8082/health/ready
curl -sS -o /dev/null -w "%{http_code}\n" http://localhost:8083/health/ready
```

All three must print `200`. Tokens: [`docs/api/auth.md`](../api/auth.md).

## Three Compose replicas

Shares the Compose `wagering` database and the provisioned FIFO queues (operator-shaped; not
isolated per test):

```sh
docker compose -f docker-compose.yml -f docker-compose.instances.yml up --build
```

| Replica | Host port |
| --- | --- |
| `app` | 8080 |
| `app-b` | 8082 |
| `app-c` | 8083 |

`OIDC_ISSUER` inside those containers is `http://keycloak:8080/realms/wagering`. Obtain tokens
from `http://keycloak:8080` (for example `docker compose exec app …`) or do not mix host-issued
tokens with Compose processes.

## Mandatory two-bet scenario (manual)

Wallet **100.00 BRL**, two distinct **80.00 BRL** bets sent at the same time to **different**
instances.

```sh
INTERNAL=$(curl -sS -X POST http://localhost:8081/realms/wagering/protocol/openid-connect/token \
  -d grant_type=client_credentials -d client_id=wagering-internal -d client_secret=internal-secret \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')
PROVIDER=$(curl -sS -X POST http://localhost:8081/realms/wagering/protocol/openid-connect/token \
  -d grant_type=client_credentials -d client_id=provider-a -d client_secret=provider-a-secret \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')

WALLET=$(curl -sS -X POST http://localhost:8080/wallets \
  -H "Authorization: Bearer $INTERNAL" -H "Content-Type: application/json" \
  -d '{"playerId":"player-multi-1","initialBalance":{"amount":"100.00","currency":"BRL"}}')
WALLET_ID=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"$WALLET")
PLAYER=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["playerId"])' <<<"$WALLET")

bet() {
  local port=$1 ext=$2
  curl -sS -w "\n%{http_code}\n" -X POST "http://localhost:${port}/wagering/transactions" \
    -H "Authorization: Bearer $PROVIDER" -H "Content-Type: application/json" \
    -H "Idempotency-Key: provider-a:${ext}" \
    -d "{\"providerId\":\"provider-a\",\"externalTransactionId\":\"${ext}\",\"playerId\":\"${PLAYER}\",\"walletId\":\"${WALLET_ID}\",\"roundId\":\"round-1\",\"gameId\":\"game-1\",\"kind\":\"BET\",\"money\":{\"amount\":\"80.00\",\"currency\":\"BRL\"}}"
}

bet 8082 bet-a &
bet 8083 bet-b &
wait
```

Expected: one `200` `PROCESSED`, one `422` `INSUFFICIENT_FUNDS`; `GET` wallet on **8080** shows
`"20.00"`; ledger has a single debit (opening credit + one debit). Replay both bets on any
instance: `idempotentReplay: true`, same statuses, same balance.

Reconciliation:

```sh
curl -sS -X POST "http://localhost:8080/wallets/${WALLET_ID}/reconciliation" \
  -H "Authorization: Bearer $INTERNAL"
```

`consistent` must be true.

## How to verify in SQL

```sh
psql "$POSTGRES_DSN" -c "SELECT id, balance_minor, version FROM wagering.wallets WHERE id = '${WALLET_ID}'"
psql "$POSTGRES_DSN" -c "SELECT direction, amount_minor, balance_after_minor FROM wagering.wallet_ledger_entries WHERE wallet_id = '${WALLET_ID}' ORDER BY created_at, id"
psql "$POSTGRES_DSN" -c "SELECT external_transaction_id, status, failure_code FROM wagering.wager_transactions WHERE wallet_id = '${WALLET_ID}' AND origin = 'EXTERNAL'"
```

`balance_minor` = 2000; one `DEBIT` of 8000; one `PROCESSED` BET and one `REJECTED` /
`INSUFFICIENT_FUNDS`.

Failure injection (`kill -9`, stop Postgres/SQS): [`failure-simulation.md`](failure-simulation.md).
Pending references: [`pending-references.md`](pending-references.md).
