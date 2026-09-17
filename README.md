# Wagering

Solution to a take-home backend challenge: a **Go + Uber Fx** service that processes
`BET`, `WIN`, `LOSS`, `REFUND` and `ROLLBACK` from game providers over HTTP and SQS, with
several instances sharing PostgreSQL.

The original statement (Portuguese) is [`init.md`](init.md). Decisions, limitations and
interpretations: [`ARCHITECTURE.md`](ARCHITECTURE.md). Requirement coverage:
[`docs/requirements/traceability.md`](docs/requirements/traceability.md).

This is a challenge deliverable, not production software. Credentials below and in
[`.env.example`](.env.example) are **local dummies**.

```mermaid
flowchart LR
  Providers -->|HTTP + JWT| App
  Providers -->|SQS FIFO| App
  Internal[Internal wallet service] -->|HTTP + JWT| App
  App -->|OIDC| Keycloak
  App -->|pgx| PostgreSQL
  App -->|consume / publish| SQS
```

`App` is `cmd/wagering`. Several OS processes (or Compose replicas) can run against the same
database: coordination is a per-wallet row lock, not a process-local mutex.

## Design in brief

| Guarantee | How |
| --- | --- |
| No floating-point money | `int64` minor units + ISO 4217; JSON `amount` is a string; PostgreSQL `BIGINT` |
| Persistent idempotency | Unique `(provider_id, idempotency_key)` in PostgreSQL; replay returns the stored result |
| Invariants in the database | `CHECK (balance_minor >= 0)`, uniqueness, append-only ledger trigger — independent of app locks and FIFO dedup |
| Publish after commit | Transactional outbox in the same SQL transaction; publisher claims committed rows |
| Auditable ledger | Insert-only entries; corrections are new rows |
| Parallel wallets, no global lock | `SELECT … FOR UPDATE` on the wallet row; independent wallets proceed concurrently |
| Authn / authz | Keycloak OIDC `client_credentials`; `providerId` from JWT `azp`; a provider sees only its own transactions |
| At-least-once messaging | Transactional inbox; SQS delete after commit; DLQ after five receives; pending references resume from PostgreSQL |

Optional extras **not** implemented: OpenTelemetry tracing, double-entry bookkeeping. An optional
load burst ships as `go run ./cmd/loadtest` ([runbook](docs/runbooks/load-test.md)).

## Quick start

```sh
docker compose up --build
```

Wait until the `app` container is healthy (`GET /health/ready` → `200`). Keycloak realm import
can take a minute on first start.

Then run the [example calls](#example-calls) against `http://localhost:8080` with tokens from
`http://localhost:8081`. Unit tests (no Docker):

```sh
go test ./...
go test -race ./...
go vet ./...
```

## Prerequisites

- **Go 1.25+** (version in `go.mod` and the Dockerfile)
- **Docker** and **Docker Compose**
- **curl** and **python3** (JSON extraction in the copy-paste examples)
- Optionally [golang-migrate](https://github.com/golang-migrate/migrate) CLI, only to roll
  migrations back by hand
- Optionally the AWS CLI (or `awslocal` inside the LocalStack container) for manual SQS examples

## Environment variables

Copy [`.env.example`](.env.example) and adjust. Do not commit real secrets. Values there are local
dummies for a **host** process (`localhost` ports). Compose overrides them with service DNS names.

### Process

| Variable | Purpose |
| --- | --- |
| `LOG_LEVEL` | `log/slog` level (`debug`, `info`, `warn`, `error`) |
| `HTTP_ADDR` | HTTP listen address (e.g. `:8080`) |
| `FX_START_TIMEOUT` | Fx start timeout (Go duration, e.g. `15s`) |
| `FX_STOP_TIMEOUT` | Fx stop timeout (Go duration, e.g. `30s`) |

### PostgreSQL

| Variable | Purpose |
| --- | --- |
| `POSTGRES_DSN` | PostgreSQL DSN for `pgx` (URL form recommended, e.g. `postgres://…`) |
| `MIGRATIONS_PATH` | Directory of golang-migrate SQL files (typically `migrations`) |

### AWS / SQS

| Variable | Purpose |
| --- | --- |
| `AWS_REGION` | AWS region for SQS / LocalStack |
| `AWS_ACCESS_KEY_ID` | Static access key (LocalStack accepts any non-empty dummy) |
| `AWS_SECRET_ACCESS_KEY` | Static secret key (dummy for LocalStack) |
| `AWS_ENDPOINT_URL` | SQS endpoint (`http://localhost:4566` on the host; Compose-internal URL in Docker) |
| `AWS_EC2_METADATA_DISABLED` | Set `true` locally so the SDK does not wait on EC2 IMDS |
| `SQS_WAGER_QUEUE_NAME` | FIFO queue name: `wager-transactions.fifo` |
| `SQS_WAGER_DLQ_NAME` | FIFO DLQ name: `wager-transactions-dlq.fifo` |
| `SQS_EVENTS_QUEUE_NAME` | Outbox destination FIFO: `wager-events.fifo` |
| `SQS_VISIBILITY_TIMEOUT` | Inbound message visibility / in-flight deadline (e.g. `30s`) |
| `SQS_WAIT_TIME` | Receive long-poll wait (max `20s`) |
| `SQS_BACKOFF_MAX` | Cap for inbound visibility backoff (e.g. `20s`) |

### Outbox publisher

| Variable | Purpose |
| --- | --- |
| `OUTBOX_POLL_INTERVAL` | Publisher poll interval (e.g. `250ms`) |
| `OUTBOX_BATCH_SIZE` | Max unpublished rows claimed per tick (positive integer) |
| `OUTBOX_LEASE` | Claim lease / in-flight publish deadline (e.g. `30s`) |
| `OUTBOX_BACKOFF_MAX` | Cap for exponential publish retry backoff (e.g. `1m`) |

### Pending-reference worker

| Variable | Purpose |
| --- | --- |
| `PENDING_POLL_INTERVAL` | Worker poll interval (e.g. `250ms`) |
| `PENDING_BATCH_SIZE` | Max `PENDING` / `PENDING_REFERENCE` rows claimed per tick |
| `PENDING_LEASE` | Claim lease / in-flight resume deadline (e.g. `30s`) |
| `PENDING_BACKOFF_MAX` | Cap for exponential pending retry backoff (e.g. `1m`) |
| `PENDING_MAX_ATTEMPTS` | Max claim attempts before `REFERENCE_NOT_FOUND` |
| `PENDING_TTL` | Max age from `created_at` before `REFERENCE_NOT_FOUND` |

### OIDC

| Variable | Purpose |
| --- | --- |
| `OIDC_ISSUER` | Expected JWT `iss` (Keycloak realm URL, no trailing slash) |
| `OIDC_AUDIENCE` | Expected JWT `aud` (`wagering-api`) |
| `OIDC_JWKS_URL` | Optional JWKS URL; defaults to `{OIDC_ISSUER}/protocol/openid-connect/certs` |
| `OIDC_INTERNAL_CLIENT` | OAuth client id of the internal wallet service (`wagering-internal`) |

## Running the environment

From a clean checkout, start PostgreSQL, Keycloak (realm import), LocalStack (queues + redrive)
and the app. The app applies migrations **Up** on start.

```sh
docker compose up --build
```

Wait until the app container is healthy (or until `GET /health/ready` returns 200).

Three Compose replicas (ports `8080` / `8082` / `8083`):

```sh
docker compose -f docker-compose.yml -f docker-compose.instances.yml up --build
```

### Running the binary on the host

Start dependencies only, then run the module against `.env.example`:

```sh
docker compose up -d postgres keycloak localstack
set -a && source .env.example && set +a
go run ./cmd/wagering
```

Use the host values in `.env.example` (`localhost` ports, not Docker DNS names). Service names and
published ports: [`docs/runbooks/test-dependencies.md`](docs/runbooks/test-dependencies.md).

A token obtained from `http://localhost:8081` has `iss` `http://localhost:8081/realms/wagering`
and is rejected by the Compose app process (`http://keycloak:8080/realms/wagering`). Use host
`.env.example` values when the binary runs on the host.

## SQS queues

LocalStack is provisioned on start from [`docker/localstack/init-sqs.sh`](docker/localstack/init-sqs.sh):

| Queue | Role |
| --- | --- |
| `wager-transactions.fifo` | Inbound wager operations (`SQS_WAGER_QUEUE_NAME`) |
| `wager-transactions-dlq.fifo` | Dead-letter queue (`SQS_WAGER_DLQ_NAME`); redrive `maxReceiveCount=5` |
| `wager-events.fifo` | Outbox publisher destination (`SQS_EVENTS_QUEUE_NAME`) |

Confirm the queues exist:

```sh
docker compose exec -T localstack awslocal sqs get-queue-url --queue-name wager-transactions.fifo
docker compose exec -T localstack awslocal sqs get-queue-url --queue-name wager-transactions-dlq.fifo
docker compose exec -T localstack awslocal sqs get-queue-url --queue-name wager-events.fifo
```

The process consumes `wager-transactions.fifo` and publishes committed outbox rows to
`wager-events.fifo`. Inbound: [`docs/events/inbound.md`](docs/events/inbound.md)
([ADR 0017](docs/adr/0017-inbox-persistence.md), [ADR 0018](docs/adr/0018-sqs-inbound-consume.md)).
Outbound: [ADR 0015](docs/adr/0015-outbox-destination-and-routing.md),
[ADR 0016](docs/adr/0016-outbox-claim-and-backoff.md). Contracts:
[`docs/events/README.md`](docs/events/README.md).

## Migrations

The app applies golang-migrate **Up** on start from `MIGRATIONS_PATH`.

| Version | What it creates |
| --- | --- |
| `000001_bootstrap` | Schema `wagering` |
| `000002_financial_schema` | `wallets`, `wager_transactions`, `wallet_ledger_entries` (BIGINT minor units, uniqueness/check constraints, append-only ledger trigger) |
| `000003_outbox` | `outbox_events` (insert unpublished; publisher claims and sets `published_at`) |
| `000004_inbox` | `inbox_messages` (`(consumer_name, message_id)` unique; completed in the financial commit) |

Rollback is **not** run on shutdown. Using the [golang-migrate CLI](https://github.com/golang-migrate/migrate):

```sh
migrate -path migrations -database "$POSTGRES_DSN" down 1
```

If the CLI rejects the process DSN, switch the scheme to the postgres/pgx URL the driver expects,
for example `postgres://user:pass@localhost:5432/dbname?sslmode=disable` or `pgx5://…`
(golang-migrate v4). See [ADR 0003](docs/adr/0003-database-access-and-migrations.md).

## Authentication and test identities

Keycloak realm `wagering` is imported automatically from
[`docker/keycloak/realm-wagering.json`](docker/keycloak/realm-wagering.json). Grant:
`client_credentials`. Local dummy secrets (not for production):

| Client | Secret | Role |
| --- | --- | --- |
| `wagering-internal` | `internal-secret` | Wallet routes |
| `provider-a` | `provider-a-secret` | `providerId=provider-a` |
| `provider-b` | `provider-b-secret` | `providerId=provider-b` |

Audience: `wagering-api`. Admin console (local): `http://localhost:8081` (`admin` / `admin`).
Contract: [`docs/api/auth.md`](docs/api/auth.md). Health and `/metrics` stay public.

The examples below assume a **host** binary (`OIDC_ISSUER=http://localhost:8081/realms/wagering`).
For the Compose `app` container, obtain tokens from `http://keycloak:8080` (for example
`docker compose exec app …`) so `iss` matches.

```sh
ACCESS_TOKEN=$(curl -sS -X POST http://localhost:8081/realms/wagering/protocol/openid-connect/token \
  -d grant_type=client_credentials \
  -d client_id=provider-a \
  -d client_secret=provider-a-secret | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')
```

## Example calls

With `HTTP_ADDR=:8080`. Full HTTP contracts: [`docs/api/`](docs/api/).

### Health and metrics

```sh
curl -sS http://localhost:8080/health/live
curl -sS -o /tmp/ready.json -w "%{http_code}\n" http://localhost:8080/health/ready
curl -sS http://localhost:8080/metrics | head
```

- Live: `200` and `{"status":"ok"}` while the process can serve HTTP.
- Ready: `200` and `{"status":"ok"}` when PostgreSQL and SQS pass checks; `503` otherwise,
  including once shutdown has started.
- Metrics: Prometheus text (`wagering_*`). Public, like health. Catalog:
  [`docs/api/metrics.md`](docs/api/metrics.md).

Business HTTP echoes `X-Correlation-Id` (UUID v7 if the client omits it). JSON logs include
`correlationId` / `providerId` / `transactionId` / `walletId` / `messageId` when known; they
never include Bearer tokens or money amounts ([ADR 0021](docs/adr/0021-observability-logs-and-metrics.md)).

### Open a wallet, bet, read, reconcile

```sh
INTERNAL_TOKEN=$(curl -sS -X POST http://localhost:8081/realms/wagering/protocol/openid-connect/token \
  -d grant_type=client_credentials -d client_id=wagering-internal -d client_secret=internal-secret \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')
PROVIDER_TOKEN=$(curl -sS -X POST http://localhost:8081/realms/wagering/protocol/openid-connect/token \
  -d grant_type=client_credentials -d client_id=provider-a -d client_secret=provider-a-secret \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')

WALLET=$(curl -sS -X POST http://localhost:8080/wallets \
  -H "Authorization: Bearer $INTERNAL_TOKEN" -H "Content-Type: application/json" \
  -H "X-Correlation-Id: demo-open-wallet" \
  -d '{"playerId":"player-demo-1","initialBalance":{"amount":"1000.00","currency":"BRL"}}')
echo "$WALLET"
WALLET_ID=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"$WALLET")
PLAYER=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["playerId"])' <<<"$WALLET")

curl -sS -X POST http://localhost:8080/wagering/transactions \
  -H "Authorization: Bearer $PROVIDER_TOKEN" -H "Content-Type: application/json" \
  -H "Idempotency-Key: provider-a:bet-demo-1" \
  -d "{\"providerId\":\"provider-a\",\"externalTransactionId\":\"bet-demo-1\",\"playerId\":\"${PLAYER}\",\"walletId\":\"${WALLET_ID}\",\"roundId\":\"round-1\",\"gameId\":\"fortune-chimp\",\"kind\":\"BET\",\"money\":{\"amount\":\"25.00\",\"currency\":\"BRL\"}}"

curl -sS "http://localhost:8080/providers/provider-a/wagering/transactions/bet-demo-1" \
  -H "Authorization: Bearer $PROVIDER_TOKEN"

curl -sS "http://localhost:8080/wallets/${WALLET_ID}/ledger" \
  -H "Authorization: Bearer $INTERNAL_TOKEN"

curl -sS -X POST "http://localhost:8080/wallets/${WALLET_ID}/reconciliation" \
  -H "Authorization: Bearer $INTERNAL_TOKEN"
```

Expected: wallet `201` with `"1000.00"`; BET `200` `PROCESSED` balance `"975.00"`; replay of the
same `Idempotency-Key` returns `idempotentReplay: true` without a second debit; reconciliation
`consistent: true`.

`REFUND` / `ROLLBACK` before the referenced BET returns `202` `PENDING_REFERENCE`. Procedure:
[`docs/runbooks/pending-references.md`](docs/runbooks/pending-references.md).

### Inbound SQS

Same use case and idempotency as HTTP ([`docs/events/inbound.md`](docs/events/inbound.md)).
`MessageGroupId` is the wallet id; `MessageDeduplicationId` is envelope `messageId`.

```sh
BODY=$(WALLET_ID="$WALLET_ID" PLAYER="$PLAYER" python3 - <<'PY'
import json, os
print(json.dumps({
  "messageId": "msg-bet-demo-sqs-1",
  "type": "WagerTransactionRequested",
  "occurredAt": "2026-09-17T12:00:00.000Z",
  "data": {
    "providerId": "provider-a",
    "externalTransactionId": "bet-demo-sqs-1",
    "idempotencyKey": "provider-a:bet-demo-sqs-1",
    "playerId": os.environ["PLAYER"],
    "walletId": os.environ["WALLET_ID"],
    "roundId": "round-1",
    "gameId": "fortune-chimp",
    "kind": "BET",
    "money": {"amount": "10.00", "currency": "BRL"}
  }
}))
PY
)
QUEUE_URL=$(docker compose exec -T localstack awslocal sqs get-queue-url \
  --queue-name wager-transactions.fifo --query QueueUrl --output text)
docker compose exec -T localstack awslocal sqs send-message \
  --queue-url "$QUEUE_URL" \
  --message-group-id "$WALLET_ID" \
  --message-deduplication-id "msg-bet-demo-sqs-1" \
  --message-body "$BODY"
```

Poll `GET /providers/provider-a/wagering/transactions/bet-demo-sqs-1` until `PROCESSED`. Published
events land on `wager-events.fifo`.

## Tests

Unit tests (default; no containers):

```sh
go test ./...
go test -race ./...
go vet ./...
gofmt -l .
```

`gofmt -l .` must print nothing. Dependencies are pinned in `go.mod` / `go.sum`.

Integration tests use build tag `integration`. They skip if required env is unset (`skip-if-no-env`).
That skip is not a substitute for CI with real containers.

| What | Needs | Package hint |
| --- | --- | --- |
| Migrations, constraints, ledger immutability, financial atomicity | `POSTGRES_DSN` | `internal/adapter/postgres` |
| HTTP use cases and concurrency | `POSTGRES_DSN` | `TestHTTPPhase5UseCases` and concurrency tests |
| Outbox claim / `SKIP LOCKED` | `POSTGRES_DSN` | `TestOutbox*` claim tests |
| Outbox publish | `POSTGRES_DSN` + LocalStack (`AWS_ENDPOINT_URL`) | `TestOutboxRelay*`, contend/recover |
| Inbound consume, inbox, DLQ | `POSTGRES_DSN` + LocalStack | `TestInbound*` |
| Pending-reference resume | `POSTGRES_DSN` | `TestPending*` |
| Multi-instance (three OS processes) | `POSTGRES_DSN`, `OIDC_ISSUER`, LocalStack | `TestInstances*` in `internal/composition` |
| Auth against the real IdP | Keycloak + `OIDC_ISSUER` | `TestAuthRealIdP`, `TestOIDCVerifierRealIdP` |

Full tagged suite with Compose dependencies:

```sh
docker compose up -d postgres keycloak localstack
set -a && source .env.example && set +a
go test -tags=integration ./...
```

How to run tagged tests: [`docs/runbooks/integration.md`](docs/runbooks/integration.md). Starting
dependencies: [`docs/runbooks/test-dependencies.md`](docs/runbooks/test-dependencies.md). Strategy:
[ADR 0005](docs/adr/0005-test-strategy-initial.md). Three processes:
[`docs/runbooks/multiple-instances.md`](docs/runbooks/multiple-instances.md)
([ADR 0020](docs/adr/0020-multi-instance-and-failure-injection.md)). Crashes and dependency loss:
[`docs/runbooks/failure-simulation.md`](docs/runbooks/failure-simulation.md). Pending references:
[`docs/runbooks/pending-references.md`](docs/runbooks/pending-references.md). Optional load burst
(throughput, p50/p95/p99, conflicts, outbox lag): [`docs/runbooks/load-test.md`](docs/runbooks/load-test.md)
(`go run ./cmd/loadtest`; [ADR 0022](docs/adr/0022-load-test-harness.md)).

## Documentation

| Document | What it is |
| --- | --- |
| [`init.md`](init.md) | Challenge statement (Portuguese) |
| [`ARCHITECTURE.md`](ARCHITECTURE.md) | Decisions, limitations, interpretations |
| [`docs/requirements/traceability.md`](docs/requirements/traceability.md) | Requirement → code / test / docs |
| [`docs/adr/`](docs/adr/) | One accepted decision per file |
| [`docs/api/`](docs/api/) | HTTP contracts, status codes, error bodies |
| [`docs/events/`](docs/events/) | SQS inbound and outbox event contracts |
| [`docs/runbooks/`](docs/runbooks/) | Integration, N instances, failures, pending references, load |
| [`.env.example`](.env.example) | Local dummy values, no real secrets |
