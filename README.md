# tec-go — Distributed wager processing

Go + Uber Fx service that processes financial operations from game providers via HTTP and SQS, backed by
PostgreSQL, Keycloak and LocalStack. Full challenge statement (in Portuguese) in [`init.md`](init.md).

> Phase 3: process skeleton, pure domain model, and **financial persistence** (schema `000002`,
> repositories, unit of work). There is **no wagering API** and **no authentication** yet. Keycloak is
> started but unused by the app. See [`docs/roadmap.md`](docs/roadmap.md).

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
| `FX_START_TIMEOUT` | Fx start timeout (Go duration, e.g. `15s`) |
| `FX_STOP_TIMEOUT` | Fx stop timeout (Go duration, e.g. `30s`) |

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

LocalStack is provisioned with two FIFO queues and redrive from the main queue to the DLQ:

| Queue | Role |
| --- | --- |
| `wager-transactions.fifo` | Inbound wager operations (`SQS_WAGER_QUEUE_NAME`) |
| `wager-transactions-dlq.fifo` | Dead-letter queue (`SQS_WAGER_DLQ_NAME`) |

The process **does not consume** these queues. It only uses SQS for readiness (queue exists /
reachable). Message contracts: [`docs/events/README.md`](docs/events/README.md).

## Migrations

The app applies golang-migrate **Up** on start from `MIGRATIONS_PATH`.

| Version | What it creates |
| --- | --- |
| `000001_bootstrap` | Schema `wagering` |
| `000002_financial_schema` | `wallets`, `wager_transactions`, `wallet_ledger_entries` (BIGINT minor units, uniqueness/check constraints, append-only ledger trigger) |

Rollback is **not** run on shutdown. Using the [golang-migrate CLI](https://github.com/golang-migrate/migrate):

```sh
migrate -path migrations -database "$POSTGRES_DSN" down 1
```

If the CLI rejects the process DSN, switch the scheme to the postgres/pgx URL the driver expects, for
example `postgres://user:pass@localhost:5432/dbname?sslmode=disable` or `pgx5://…` (golang-migrate v4).
See [ADR 0003](docs/adr/0003-database-access-and-migrations.md).

## Authentication and test identities

Not used in Phase 3. Health endpoints are public. Keycloak is up for later phases; provisioning of
realms, clients and test users will be documented when authentication is implemented.

## Example calls

With `HTTP_ADDR=:8080` (adjust host/port to match Compose or `.env.example`):

```sh
curl -sS http://localhost:8080/health/live
curl -sS -o /tmp/ready.json -w "%{http_code}\n" http://localhost:8080/health/ready
```

- Live: `200` and `{"status":"ok"}` while the process can serve HTTP.
- Ready: `200` and `{"status":"ok"}` when PostgreSQL and SQS pass checks; `503` otherwise,
  including once shutdown has started.

Full contract: [`docs/api/health.md`](docs/api/health.md). There are no `/wallets` or
`/wagering/transactions` routes yet.

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

**TST-04** (postgres migrations, constraints, ledger immutability, financial atomicity) needs
`POSTGRES_DSN` only. Keycloak and SQS are not required:

```sh
docker compose up -d postgres
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
