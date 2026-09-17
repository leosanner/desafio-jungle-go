# CLAUDE.md

Guide for agents working in this repository.

## Context

Technical challenge: a **Go + Uber Fx** service that processes financial operations from game providers
(`BET`, `WIN`, `LOSS`, `REFUND`, `ROLLBACK`) via HTTP and SQS, backed by PostgreSQL, Keycloak (OIDC) and
LocalStack/MiniStack. The full challenge statement, and **source of truth**, is [`init.md`](init.md)
(written in Portuguese).

Before implementing any feature, read the corresponding section of `init.md` and its status in
[`docs/requirements/traceability.md`](docs/requirements/traceability.md).

## Language

**English is the default** for everything: code, identifiers, comments, documentation, ADRs, commit
messages and PR descriptions. `init.md` stays in its original language.

## Non-negotiable rules (disqualifying in the challenge)

1. Never use `float32`/`float64` for money — not in parsing, arithmetic, JSON or persistence.
2. Idempotency is always persisted in PostgreSQL; never in memory only.
3. Financial invariants (non-negativity, uniqueness, immutable ledger) are enforced **by the database**
   (constraints, triggers, grants), not only by the application.
4. Events are published only **after** commit (transactional outbox).
5. Append-only ledger: corrections are new entries.
6. Coordination is per wallet; **global locks are forbidden**. The solution must work with N instances.
7. Every business endpoint requires authentication; a provider can only access its own transactions.
8. The domain does not import Fx, `net/http`, the AWS SDK or persistence libraries.
9. Integration tests use real PostgreSQL, IdP and SQS (containers) — never mock all of them away.

When reviewing changes that touch money, balances, the ledger, idempotency, concurrency or messaging,
use the `invariants-review` skill.

## Where things go

| What | Where |
| --- | --- |
| Technical decision (library, lock strategy, hash format...) | ADR in `docs/adr/` (`adr` skill) plus a summary in `ARCHITECTURE.md` |
| HTTP contracts, status codes, error bodies | `docs/api/` |
| Message/event contracts and routing | `docs/events/` |
| Failure simulations, multi-instance runs, load tests | `docs/runbooks/` |
| Status of each challenge requirement | `docs/requirements/traceability.md` |
| Implementation order | `docs/roadmap.md` |

Do not make architectural decisions silently: if a choice is not covered by an ADR, propose it to the
user and record it.

## Project skills (`.claude/skills/`)

| Skill | When to use |
| --- | --- |
| `go-conventions` | Writing or reviewing any Go code in the project |
| `fx-module` | Creating/changing Fx modules, lifecycle, workers and shutdown |
| `db-migration` | Creating or changing the schema/migrations |
| `go-testing` | Writing unit, integration, concurrency or recovery tests |
| `invariants-review` | Reviewing diffs touching money, ledger, idempotency, concurrency, inbox/outbox or auth |
| `adr` | Recording an architectural decision |
| `spec-audit` | Auditing the project against the challenge and updating the traceability matrix |

## Commands

```sh
docker compose up --build
go run ./cmd/wagering
go test ./...
go test -race ./...
go test -tags=integration ./...
go run ./cmd/loadtest
go vet ./...
gofmt -l .
migrate -path migrations -database "$POSTGRES_DSN" down 1
```

- The app applies migrations **Up** on start. Rollback is CLI-only (not on shutdown).
- Health: `curl -sS http://localhost:8080/health/live` and `/health/ready` (port from `HTTP_ADDR`).
  Metrics: `curl -sS http://localhost:8080/metrics` (public). Business routes need `Authorization: Bearer`
  ([docs/api/auth.md](docs/api/auth.md)).
- Default `go test ./...` is unit tests only (no containers). `-tags=integration` is for real
  PostgreSQL / Keycloak / LocalStack when those tests exist ([ADR 0005](docs/adr/0005-test-strategy-initial.md)).
  TST-04 postgres tests need `POSTGRES_DSN` only (Keycloak/SQS not required). HTTP Phase 5
  integration tests (`TestHTTPPhase5UseCases` and concurrency tests) also need `POSTGRES_DSN` only.
  Phase 6 outbox publish tests (`TestOutboxRelay*`, `TestOutboxTwoPublishersContend`,
  `TestOutboxRecoverPublishBeforeAck`) need `POSTGRES_DSN` and LocalStack (`AWS_ENDPOINT_URL`).
  Claim/SKIP LOCKED tests need `POSTGRES_DSN` only. Phase 7 inbound consume tests (`TestInbound*`)
  need `POSTGRES_DSN` and LocalStack (`AWS_ENDPOINT_URL`). Phase 8 pending-reference tests
  (`TestPending*`) need `POSTGRES_DSN` only. Phase 9 multi-instance tests (`TestInstances*`) need
  `POSTGRES_DSN`, `OIDC_ISSUER` and LocalStack (`AWS_ENDPOINT_URL`). TST-07 needs `OIDC_ISSUER` and a
  running Keycloak with the imported `wagering` realm. Optional load burst: `go run ./cmd/loadtest`
  against a ready process ([docs/runbooks/load-test.md](docs/runbooks/load-test.md); [ADR 0022](docs/adr/0022-load-test-harness.md)).

## Current state

Phase 11 delivery is complete: README, ARCHITECTURE, `.env.example`, runbooks (including pending
references and an optional load burst) and a final spec audit. JSON `slog` with correlation IDs
and payload redaction plus a Prometheus catalog on public `GET /metrics` remain from Phase 10
([ADR 0021](docs/adr/0021-observability-logs-and-metrics.md)). Multi-instance proof (`TestInstances*`),
transactional inbox + SQS consume, pending-reference `SKIP LOCKED`, HTTP use cases, domain,
persistence, OIDC and the outbox publisher remain. Tracing and double-entry bookkeeping are
optional extras and are not implemented. Load tests ship as `go run ./cmd/loadtest`
([ADR 0022](docs/adr/0022-load-test-harness.md)). Decisions:
[ADR 0001](docs/adr/0001-package-layout-and-layer-boundaries.md)–[0022](docs/adr/0022-load-test-harness.md)
(accepted).
