# Integration tests (TST-04, TST-07, HTTP use cases, Phase 6 outbox, Phase 7 inbound)

Postgres constraint, atomicity and HTTP use-case tests use build tag `integration` and a real PostgreSQL.
Keycloak and SQS are **not** required for TST-04 or the Phase 5 HTTP tests (`TestHTTPPhase5UseCases`,
`TestHTTPConcurrentSameBet`, `TestHTTPTwoBetsOnHundred`, `TestHTTPDistinctWalletsParallel`).

Phase 6 claim tests (`TestOutboxClaimSkipLocked`, `TestOutboxMarkPublishedAndRetry`) need
`POSTGRES_DSN` only. Publish/recovery tests (`TestOutboxRelayPublishesOpeningEvents`,
`TestOutboxRelayPublishesEnvelope`, `TestOutboxTwoPublishersContend`,
`TestOutboxRecoverPublishBeforeAck`) also need LocalStack (`AWS_ENDPOINT_URL`, dummy AWS keys).

Phase 7 inbound tests (`TestHandleInboundInboxAtomicWithDomain`, `TestInboundConsumerProcessesBet`,
`TestInboundRecoverCommitBeforeDelete`, `TestInboundInvalidMessageGoesToDLQ`,
`TestInboundHTTPxSQSSameKeyOneDebit`, `TestInboundDuplicateSQSCopiesOneDebit`) need `POSTGRES_DSN`
and LocalStack. The commit-before-delete test installs `Consumer.SetAfterCommit` so `DeleteMessage`
is skipped ([ADR 0018](../adr/0018-sqs-inbound-consume.md)). That hook is tests-only.

The publish-before-ack recovery test installs `OutboxRelay.SetAfterPublish` so `MarkPublished` is
skipped after a successful `SendMessage` ([ADR 0016](../adr/0016-outbox-claim-and-backoff.md)). That
hook is tests-only.

OIDC tests (TST-07) use the same tag and a real Keycloak with realm `wagering` imported.

## Prerequisites

- Docker and Docker Compose
- `POSTGRES_DSN` for TST-04 (host value in [`.env.example`](../../.env.example))
- `AWS_ENDPOINT_URL` (and a healthy LocalStack) for Phase 6 publish tests and Phase 7 inbound tests
- `OIDC_ISSUER` (and a healthy Keycloak) for TST-07

## Run

Postgres only (TST-04 and HTTP Phase 5):

```sh
docker compose up -d postgres
set -a && source .env.example && set +a
go test -tags=integration ./internal/adapter/postgres/... ./internal/adapter/http/... ./internal/app/...
```

Postgres + LocalStack (Phase 6 outbox publish, TST-13, Phase 7 inbound, TST-12):

```sh
docker compose up -d postgres localstack
set -a && source .env.example && set +a
go test -tags=integration ./internal/adapter/postgres/... -run 'TestOutbox|TestInbound|TestHandleInbound'
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
- `internal/adapter/postgres/outbox_integration_test.go` — claim `SKIP LOCKED`, relay publish, two publishers (TST-13), publish-before-ack recovery
- `internal/adapter/postgres/inbound_integration_test.go` — inbox atomicity, consume, TST-12 skip-delete, DLQ, HTTP×SQS, application dedup
- `internal/composition/start_stop_integration_test.go` — Fx start/stop including the outbox and inbound workers
- `internal/adapter/auth/oidc_integration_test.go` — `TestOIDCVerifierRealIdP`
- `internal/adapter/http/auth_integration_test.go` — `TestAuthRealIdP` (missing/invalid token, isolation, internal restriction, no wallet writes on 403)

Starting all challenge dependencies:
[`test-dependencies.md`](test-dependencies.md). Strategy: [ADR 0005](../adr/0005-test-strategy-initial.md).
