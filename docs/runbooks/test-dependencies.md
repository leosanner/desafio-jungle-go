# Test dependencies

Start the infrastructure the challenge requires: PostgreSQL, Keycloak (OIDC) and LocalStack (SQS).
Default `go test ./...` does not need these containers ([ADR 0005](../adr/0005-test-strategy-initial.md)).

**TST-04** (postgres migrations, constraints, ledger immutability, financial atomicity) needs
PostgreSQL and `POSTGRES_DSN` only. Keycloak and SQS are not required for that slice. See
[`integration.md`](integration.md).

**TST-07** (OIDC against the real IdP) needs Keycloak with realm `wagering` imported and
`OIDC_ISSUER` (host value in [`.env.example`](../../.env.example)). PostgreSQL is optional for the
auth tests except the side-effect count in `TestAuthRealIdP`. Phase 6 outbox publish and Phase 7
inbound consume also need LocalStack. Phase 8 pending-reference tests need PostgreSQL only
([`pending-references.md`](pending-references.md)).
Phase 9 `TestInstances*` needs PostgreSQL, Keycloak and LocalStack.

## Prerequisites

- Docker and Docker Compose
- Repository checkout (Compose file and `.env.example` at the root)

## Start

All dependencies (and the app, if you use the default compose service):

```sh
docker compose up --build
```

Dependencies only (typical when running `go test` / `go run ./cmd/wagering` on the host):

```sh
docker compose up -d postgres keycloak localstack
```

Postgres only (enough for TST-04):

```sh
docker compose up -d postgres
```

Service names must match `docker-compose.yml`. If Compose uses different service names, use those.

## Ports (host)

Published ports are defined in `docker-compose.yml` and `.env.example`. The usual mapping:

| Service | Host port | What “ready” means |
| --- | --- | --- |
| PostgreSQL | `5432` | Accepts connections with `POSTGRES_DSN` (e.g. `pg_isready` or `psql "$POSTGRES_DSN" -c 'select 1'`) |
| Keycloak | `8081` | Realm `wagering` imported; `GET {OIDC_ISSUER}/.well-known/openid-configuration` returns 200; admin console at `http://localhost:8081` |
| LocalStack | `4566` | SQS `GetQueueUrl` succeeds for `wager-transactions.fifo`, `wager-transactions-dlq.fifo` and `wager-events.fifo` |
| wagering (when started via Compose) | from `HTTP_ADDR` (often `8080`) | `GET /health/ready` returns `200` |

Confirm actual mappings with:

```sh
docker compose ps
```

## How to know they are ready

1. `docker compose ps` shows the services you started as running (and healthy, if healthchecks exist).
2. PostgreSQL: `psql "$POSTGRES_DSN" -c 'select 1'` (schema `wagering` and financial tables appear
   after the **app** has applied migrations `000001` and `000002`, not necessarily when Postgres first
   accepts connections). Tagged postgres tests apply migrations themselves when they run.
3. LocalStack: AWS CLI against `AWS_ENDPOINT_URL` (dummy keys are fine):

   ```sh
   aws --endpoint-url "${AWS_ENDPOINT_URL:-http://localhost:4566}" sqs get-queue-url \
     --queue-name wager-transactions.fifo
   aws --endpoint-url "${AWS_ENDPOINT_URL:-http://localhost:4566}" sqs get-queue-url \
     --queue-name wager-transactions-dlq.fifo
   aws --endpoint-url "${AWS_ENDPOINT_URL:-http://localhost:4566}" sqs get-queue-url \
     --queue-name wager-events.fifo
   ```

4. Keycloak: `curl -sS "$OIDC_ISSUER/.well-known/openid-configuration"` (issuer from `.env.example`).
   Token endpoint must accept `client_credentials` for `provider-a` / `wagering-internal`.
5. App: [`docs/api/health.md`](../api/health.md) — live `200`, ready `200` only when Postgres and SQS pass.

## Integration tests

```sh
go test -tags=integration ./...
```

Tests skip when required env is missing (`skip-if-no-env`). That skip is not a substitute for
real-container CI. TST-04 (`./internal/adapter/postgres/...`) skips without `POSTGRES_DSN` and does
not need Keycloak or LocalStack. TST-07 (`./internal/adapter/auth/...`, `./internal/adapter/http/...`)
skips without `OIDC_ISSUER`. Phase 9 (`./internal/composition/ -run TestInstances`) skips without
`POSTGRES_DSN`, `OIDC_ISSUER` or `AWS_ENDPOINT_URL`. See [integration.md](integration.md).

Multi-instance: [`multiple-instances.md`](multiple-instances.md). Failure injection:
[`failure-simulation.md`](failure-simulation.md).

## Stop

```sh
docker compose down
```
