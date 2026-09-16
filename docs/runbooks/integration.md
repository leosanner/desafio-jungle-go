# Integration tests (TST-04, TST-07, HTTP use cases)

Postgres constraint, atomicity and HTTP use-case tests use build tag `integration` and a real PostgreSQL.
Keycloak and SQS are **not** required for TST-04 or the Phase 5 HTTP tests (`TestHTTPPhase5UseCases`,
`TestHTTPConcurrentSameBet`, `TestHTTPTwoBetsOnHundred`, `TestHTTPDistinctWalletsParallel`).

OIDC tests (TST-07) use the same tag and a real Keycloak with realm `wagering` imported.

## Prerequisites

- Docker and Docker Compose
- `POSTGRES_DSN` for TST-04 (host value in [`.env.example`](../../.env.example))
- `OIDC_ISSUER` (and a healthy Keycloak) for TST-07

## Run

Postgres only (TST-04 and HTTP Phase 5):

```sh
docker compose up -d postgres
set -a && source .env.example && set +a
go test -tags=integration ./internal/adapter/postgres/... ./internal/adapter/http/... ./internal/app/...
```

Keycloak (TST-07):

```sh
docker compose up -d keycloak
set -a && source .env.example && set +a
go test -tags=integration ./internal/adapter/auth/... ./internal/adapter/http/...
```

Or the full tagged suite (needs Postgres, Keycloak and LocalStack for start/stop):

```sh
docker compose up -d postgres keycloak localstack
set -a && source .env.example && set +a
go test -tags=integration ./...
```

Tests skip if the required env is unset. That skip is not a substitute for CI with real containers.

Files:

- `internal/adapter/postgres/migrations_integration_test.go` — `TestMigrationsUpAndDown`
- `internal/adapter/postgres/constraints_integration_test.go` — CHECK/UNIQUE and ledger append-only trigger
- `internal/adapter/postgres/uow_integration_test.go` — atomic commit/rollback, duplicate wallet, money round-trip, concurrent 80.00 bets on 100.00
- `internal/adapter/postgres/isolated_db_integration_test.go` — one `wagering_it_*` database per test (does not migrate the Compose `wagering` database)
- `internal/adapter/auth/oidc_integration_test.go` — `TestOIDCVerifierRealIdP`
- `internal/adapter/http/auth_integration_test.go` — `TestAuthRealIdP` (missing/invalid token, isolation, internal restriction, no wallet writes on 403)

Starting all challenge dependencies:
[`test-dependencies.md`](test-dependencies.md). Strategy: [ADR 0005](../adr/0005-test-strategy-initial.md).
