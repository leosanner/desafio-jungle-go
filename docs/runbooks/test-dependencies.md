# Test dependencies

Start the infrastructure the challenge requires for later integration tests: PostgreSQL, Keycloak
(OIDC) and LocalStack (SQS). Phase 1 does **not** ship a full `-tags=integration` suite
([ADR 0005](../adr/0005-test-strategy-initial.md)).

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

Service names must match `docker-compose.yml`. If Compose uses different service names, use those.

## Ports (host)

Published ports are defined in `docker-compose.yml` and `.env.example`. The usual Phase 1 mapping:

| Service | Host port | What “ready” means |
| --- | --- | --- |
| PostgreSQL | `5432` | Accepts connections with `POSTGRES_DSN` (e.g. `pg_isready` or `psql "$POSTGRES_DSN" -c 'select 1'`) |
| Keycloak | `8081` | Admin console at `http://localhost:8081` (app **does not** call Keycloak in Phase 1) |
| LocalStack | `4566` | SQS `GetQueueUrl` succeeds for `wager-transactions.fifo` and `wager-transactions-dlq.fifo` |
| wagering (when started via Compose) | from `HTTP_ADDR` (often `8080`) | `GET /health/ready` returns `200` |

Confirm actual mappings with:

```sh
docker compose ps
```

## How to know they are ready

1. `docker compose ps` shows the three dependency services as running (and healthy, if healthchecks exist).
2. PostgreSQL: `psql "$POSTGRES_DSN" -c 'select 1'` (schema `wagering` appears after the **app** has
   applied migrations, not necessarily when Postgres first accepts connections).
3. LocalStack: AWS CLI against `AWS_ENDPOINT_URL` (dummy keys are fine):

   ```sh
   aws --endpoint-url "${AWS_ENDPOINT_URL:-http://localhost:4566}" sqs get-queue-url \
     --queue-name wager-transactions.fifo
   aws --endpoint-url "${AWS_ENDPOINT_URL:-http://localhost:4566}" sqs get-queue-url \
     --queue-name wager-transactions-dlq.fifo
   ```

4. Keycloak: open the host URL from Compose; unused by the Phase 1 process.
5. App: [`docs/api/health.md`](../api/health.md) — live `200`, ready `200` only when Postgres and SQS pass.

## Integration tests (later)

```sh
go test -tags=integration ./...
```

Until those tests exist, optional tests may skip when env/containers are missing. That skip is not
a substitute for real-container CI. Multi-instance and failure-injection procedures are separate
runbooks (not written yet).

## Stop

```sh
docker compose down
```
