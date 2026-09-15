# Integration tests (TST-04)

Postgres constraint and atomicity tests use build tag `integration` and a real PostgreSQL.
Keycloak and SQS are **not** required for TST-04.

## Prerequisites

- Docker and Docker Compose
- `POSTGRES_DSN` (host value in [`.env.example`](../../.env.example))

## Run

```sh
docker compose up -d postgres
set -a && source .env.example && set +a
go test -tags=integration ./internal/adapter/postgres/...
```

Or the full tagged suite:

```sh
go test -tags=integration ./...
```

Tests skip if `POSTGRES_DSN` is unset. That skip is not a substitute for CI with a real database.

Files:

- `internal/adapter/postgres/migrations_integration_test.go` — `TestMigrationsUpAndDown`
- `internal/adapter/postgres/constraints_integration_test.go` — CHECK/UNIQUE and ledger append-only trigger
- `internal/adapter/postgres/uow_integration_test.go` — atomic commit/rollback, duplicate wallet, money round-trip, concurrent 80.00 bets on 100.00
- `internal/adapter/postgres/isolated_db_integration_test.go` — one `wagering_it_*` database per test (does not migrate the Compose `wagering` database)

Starting all challenge dependencies (including Keycloak and LocalStack for later tags):
[`test-dependencies.md`](test-dependencies.md). Strategy: [ADR 0005](../adr/0005-test-strategy-initial.md).
