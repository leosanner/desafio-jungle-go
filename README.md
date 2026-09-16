# tec-go — Distributed wager processing

Go + Uber Fx service that processes financial operations from game providers via HTTP and SQS, backed by
PostgreSQL, Keycloak and LocalStack. Full challenge statement (in Portuguese) in [`init.md`](init.md).

> Phase 7: process skeleton, domain, financial persistence, OIDC, HTTP use cases, concurrent
> outbox publisher, and **SQS inbound consumer** (inbox + DLQ). No pending-reference worker yet.
> See [`docs/roadmap.md`](docs/roadmap.md).

## Prerequisites

- **Go 1.25+** (same version declared in `go.mod` and the Dockerfile)
- **Docker** and **Docker Compose**
- Optionally [golang-migrate](https://github.com/golang-migrate/migrate) CLI, only if you need to roll
  migrations back by hand

## Environment variables

Copy [`.env.example`](.env.example) and adjust. Do not commit real secrets.

| Variable | Purpose |
| --- | --- |
| `LOG_LEVEL` | `log/slog` level (`debug`, `info`, `warn`, `error`) |
| `HTTP_ADDR` | HTTP listen address (e.g. `:8080`) |
| `POSTGRES_DSN` | PostgreSQL DSN for `pgx` (URL form recommended, e.g. `postgres://…`) |
| `MIGRATIONS_PATH` | Directory of golang-migrate SQL files (typically `migrations`) |
| `AWS_REGION` | AWS region for SQS / LocalStack |
| `AWS_ACCESS_KEY_ID` | Static access key (LocalStack accepts any non-empty dummy) |
| `AWS_SECRET_ACCESS_KEY` | Static secret key (dummy for LocalStack) |
| `AWS_ENDPOINT_URL` | SQS endpoint (`http://localhost:4566` on the host; Compose-internal URL in Docker) |
| `SQS_WAGER_QUEUE_NAME` | FIFO queue name: `wager-transactions.fifo` |
| `SQS_WAGER_DLQ_NAME` | FIFO DLQ name: `wager-transactions-dlq.fifo` |
| `SQS_EVENTS_QUEUE_NAME` | Outbox destination FIFO: `wager-events.fifo` |
| `FX_START_TIMEOUT` | Fx start timeout (Go duration, e.g. `15s`) |
| `FX_STOP_TIMEOUT` | Fx stop timeout (Go duration, e.g. `30s`) |
| `OUTBOX_POLL_INTERVAL` | Publisher poll interval (e.g. `250ms`) |
| `OUTBOX_BATCH_SIZE` | Max unpublished rows claimed per tick (positive integer) |
| `OUTBOX_LEASE` | Claim lease / in-flight publish deadline (e.g. `30s`) |
| `OUTBOX_BACKOFF_MAX` | Cap for exponential publish retry backoff (e.g. `1m`) |
| `SQS_VISIBILITY_TIMEOUT` | Inbound message visibility / in-flight deadline (e.g. `30s`) |
| `SQS_WAIT_TIME` | Receive long-poll wait (max `20s`) |
| `SQS_BACKOFF_MAX` | Cap for inbound visibility backoff (e.g. `20s`) |
| `OIDC_ISSUER` | Expected JWT `iss` (Keycloak realm URL, no trailing slash) |
| `OIDC_AUDIENCE` | Expected JWT `aud` (`wagering-api`) |
| `OIDC_JWKS_URL` | Optional JWKS URL; defaults to `{OIDC_ISSUER}/protocol/openid-connect/certs` |
| `OIDC_INTERNAL_CLIENT` | OAuth client id of the internal wallet service (`wagering-internal`) |

## Running the environment

From a clean checkout, start PostgreSQL, Keycloak, LocalStack (queues + redrive) and the app:

```sh
docker compose up --build
```

Wait until the app container is healthy (or until `GET /health/ready` returns 200 — see below).

### Running the binary on the host

Start dependencies only, then run the module against `.env.example`:

```sh
docker compose up -d postgres keycloak localstack
set -a && source .env.example && set +a
go run ./cmd/wagering
```

Use the host values in `.env.example` (`localhost` ports, not Docker DNS names). Service names and
published ports are listed in [`docs/runbooks/test-dependencies.md`](docs/runbooks/test-dependencies.md).

## SQS queues

LocalStack is provisioned with inbound FIFO queues (redrive to the DLQ) and an outbound events queue:

| Queue | Role |
| --- | --- |
| `wager-transactions.fifo` | Inbound wager operations (`SQS_WAGER_QUEUE_NAME`) |
| `wager-transactions-dlq.fifo` | Dead-letter queue (`SQS_WAGER_DLQ_NAME`) |
| `wager-events.fifo` | Outbox publisher destination (`SQS_EVENTS_QUEUE_NAME`) |

The process consumes `wager-transactions.fifo` and publishes committed outbox rows to
`wager-events.fifo`. Inbound contract: [`docs/events/inbound.md`](docs/events/inbound.md)
([ADR 0017](docs/adr/0017-inbox-persistence.md), [ADR 0018](docs/adr/0018-sqs-inbound-consume.md)).
Outbound: [ADR 0015](docs/adr/0015-outbox-destination-and-routing.md),
[ADR 0016](docs/adr/0016-outbox-claim-and-backoff.md). Message contracts:
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

If the CLI rejects the process DSN, switch the scheme to the postgres/pgx URL the driver expects, for
example `postgres://user:pass@localhost:5432/dbname?sslmode=disable` or `pgx5://…` (golang-migrate v4).
See [ADR 0003](docs/adr/0003-database-access-and-migrations.md).

## Authentication and test identities

Keycloak realm `wagering` is imported automatically from
[`docker/keycloak/realm-wagering.json`](docker/keycloak/realm-wagering.json). Grant:
`client_credentials`. Local dummy secrets:

| Client | Secret | Role |
| --- | --- | --- |
| `wagering-internal` | `internal-secret` | Wallet routes |
| `provider-a` | `provider-a-secret` | `providerId=provider-a` |
| `provider-b` | `provider-b-secret` | `providerId=provider-b` |

Audience: `wagering-api`. Contract: [`docs/api/auth.md`](docs/api/auth.md). Health endpoints stay public.

```sh
ACCESS_TOKEN=$(curl -sS -X POST http://localhost:8081/realms/wagering/protocol/openid-connect/token \
  -d grant_type=client_credentials \
  -d client_id=provider-a \
  -d client_secret=provider-a-secret | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')
curl -sS http://localhost:8080/providers/provider-a/wagering/transactions/tx-1 \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

A token obtained from `http://localhost:8081` has `iss` `http://localhost:8081/realms/wagering`
and is rejected by the Compose app process, which is configured with
`http://keycloak:8080/realms/wagering`. Use host `.env.example` values when running the binary on
the host.

## Example calls

With `HTTP_ADDR=:8080` (adjust host/port to match Compose or `.env.example`):

```sh
curl -sS http://localhost:8080/health/live
curl -sS -o /tmp/ready.json -w "%{http_code}\n" http://localhost:8080/health/ready
```

- Live: `200` and `{"status":"ok"}` while the process can serve HTTP.
- Ready: `200` and `{"status":"ok"}` when PostgreSQL and SQS pass checks; `503` otherwise,
  including once shutdown has started.

Full contract: [`docs/api/health.md`](docs/api/health.md). Business routes require a Bearer token
([`docs/api/auth.md`](docs/api/auth.md)). Wallets: [`docs/api/wallets.md`](docs/api/wallets.md).
Operations: [`docs/api/wagering.md`](docs/api/wagering.md).

```sh
INTERNAL_TOKEN=$(curl -sS -X POST http://localhost:8081/realms/wagering/protocol/openid-connect/token \
  -d grant_type=client_credentials -d client_id=wagering-internal -d client_secret=internal-secret \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')
curl -sS -X POST http://localhost:8080/wallets \
  -H "Authorization: Bearer $INTERNAL_TOKEN" -H "Content-Type: application/json" \
  -d '{"playerId":"0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1","initialBalance":{"amount":"1000.00","currency":"BRL"}}'
```

Use host `.env.example` issuer/ports when the binary runs on the host (not the Compose-internal Keycloak URL).

## Tests

Unit tests (default; no containers):

```sh
go test ./...
go test -race ./...
go vet ./...
gofmt -l .
```

`gofmt -l .` must print nothing.

Integration tests use build tag `integration`. They skip if required env is unset (`skip-if-no-env`).
That skip is not a substitute for CI with real containers.

**TST-04** (postgres migrations, constraints, ledger immutability, financial atomicity) and
**Phase 5 HTTP** (`TestHTTPPhase5UseCases`, concurrency) need `POSTGRES_DSN` only. **Phase 6 outbox**
(`TestOutbox*` in `internal/adapter/postgres`) needs `POSTGRES_DSN` plus LocalStack
(`AWS_ENDPOINT_URL` and AWS dummy keys) for publish tests; claim/SKIP LOCKED tests need Postgres
only. **Phase 7 inbound** (`TestInbound*` in `internal/adapter/postgres`) needs `POSTGRES_DSN` and
LocalStack. **TST-07** (auth against the real IdP) needs Keycloak and `OIDC_ISSUER`:

```sh
docker compose up -d postgres keycloak localstack
set -a && source .env.example && set +a
go test -tags=integration ./...
```

How to run TST-04: [`docs/runbooks/integration.md`](docs/runbooks/integration.md). Starting
dependencies: [`docs/runbooks/test-dependencies.md`](docs/runbooks/test-dependencies.md). Strategy:
[ADR 0005](docs/adr/0005-test-strategy-initial.md). Multi-instance and failure simulation runbooks
are placeholders until later phases.

## Documentation

- [`ARCHITECTURE.md`](ARCHITECTURE.md) — technical decisions
- [`docs/`](docs/) — ADRs, HTTP/event contracts, runbooks and traceability
