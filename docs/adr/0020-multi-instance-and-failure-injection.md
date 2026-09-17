# 0020 — Multi-instance verification and failure injection

- **Status:** Accepted
- **Date:** 2026-09-16
- **Related requirements:** `init.md` §8, §13, §15; matrix IDs CON-02, TST-11, ELIM-04/05/07, GAR-06/07, ENT-07

## Context

Coordination is per wallet in PostgreSQL ([ADR 0010](0010-per-wallet-concurrency.md)). Inbox, outbox
and pending-reference workers already claim with `SKIP LOCKED` ([ADR 0016](0016-outbox-claim-and-backoff.md),
[ADR 0018](0018-sqs-inbound-consume.md), [ADR 0019](0019-pending-reference-resume.md)). The challenge
still requires those guarantees to be shown with **at least three independent processes**, each with
its own connections and memory, plus documented failure simulations (crash after commit, broker or
database unavailability). Goroutines in one test process cannot prove that.

[ADR 0005](0005-test-strategy-initial.md) left multi-instance and failure injection as later
runbooks. Phase 9 fills that slot.

## Options considered

### Option A — Real OS processes of `cmd/wagering` in tagged tests, plus Compose extras and runbooks

Build the binary once, start three `exec.Command` children with distinct `HTTP_ADDR`, one isolated
PostgreSQL database, unique FIFO queues, and the real Keycloak issuer. HTTP and SQS traffic is
spread across the three bases. `SIGKILL` one child to prove another instance resumes committed
`PENDING_REFERENCE`. Precise crash windows (commit→SQS delete, publish→outbox ack) stay on the
existing tests-only hooks. Runbooks cover the same scenarios by hand, including
`docker compose stop postgres|localstack`.

- Pros: Matches §8 (“processos independentes”); no shared heap; uses production wiring; hooks stay
  out of the production binary; isolated queues avoid stealing the shared Compose inbound queue.
- Cons: Tests need Postgres + Keycloak + LocalStack; slower than in-process HTTP tests; port
  reservation via bind-and-close has a small race.

### Option B — Only Docker Compose `--scale` / extra app services

- Pros: Close to how an operator runs N replicas.
- Cons: Shared Compose database and queues make tests interfere; killing one replica is awkward
  inside `go test`; harder to give each case its own schema.

### Option C — Several in-process Fx apps or goroutines

- Pros: Fast, no binary build.
- Cons: One OS process, shared Go runtime; does not satisfy CON-02 / ELIM-07.

### Option D — Production env flags that crash after commit (`FAIL_AFTER_COMMIT=1`)

- Pros: Can `kill` at the exact window without a test hook.
- Cons: A leftover flag in an operator environment is a foot-gun; the hooks already prove those
  windows without shipping crash switches.

## Decision

**Option A.**

1. **Automated proof (TST-11, CON-02):** `go test -tags=integration` starts **three** `cmd/wagering`
   processes (`TestInstances*`). Each child has its own HTTP port, pgx pool and memory. They share
   one isolated `wagering_it_*` database (migrations applied before start) and a unique inbound /
   DLQ / events FIFO triple so they do not consume the default Compose queues. Tokens come from the
   real Keycloak realm. Scenarios:
   - two distinct 80.00 bets on 100.00, one request per instance, replay on the third;
   - the same bet 50× round-robin across instances;
   - distinct wallets in parallel on different instances;
   - HTTP and SQS for the same idempotency key while three consumers run;
   - `SIGKILL` after `PENDING_REFERENCE` is committed; remaining instances resume when the
     reference arrives (short `PENDING_LEASE` in the test env).
2. **Precise crash windows:** keep tests-only `OutboxRelay.SetAfterPublish` (publish→ack) and
   `Consumer.SetAfterCommit` (commit→delete). Production processes do not install them. Do **not**
   add process-wide crash flags.
3. **Manual / operator proof:** [`docs/runbooks/multiple-instances.md`](../runbooks/multiple-instances.md)
   (three host processes or `docker-compose.instances.yml`) and
   [`docs/runbooks/failure-simulation.md`](../runbooks/failure-simulation.md)
   (`kill -9`, SIGTERM, stop PostgreSQL, stop LocalStack).

Locking, uniqueness and non-negativity stay in PostgreSQL. N instances are correct because they
share those constraints and row locks, not because they share memory.

## Consequences

- Positive: ELIM-07 and CON-02 have process-level evidence; TST-08/09/10/15/16 are repeated across
  instances; ENT-07 runbooks exist; failure windows stay injectable without production kill
  switches.
- Negative / accepted risks: tagged tests skip without `POSTGRES_DSN`, `OIDC_ISSUER` and
  `AWS_ENDPOINT_URL`; bind-and-close ports can collide under extreme load (retry is left to the
  test failure). Abrupt `SIGKILL` during a claimed pending lease waits for `PENDING_LEASE` before
  another instance can take the row — tests use a short lease.
- Known limitations: load tests remain optional (Phase 11 / later); observability of instance
  identity is Phase 10.

## Verification

- `TestInstancesThreeProcessesTwoBetsOnHundred` — CON-02/03, TST-09/11, ELIM-04.
- `TestInstancesConcurrentSameBet` — ELIM-05, TST-08/11.
- `TestInstancesDistinctWalletsParallel` — GAR-06, CON-05, TST-10/11.
- `TestInstancesHTTPxSQSSameKeyOneDebit` — TST-16/11, SQS-08.
- `TestInstancesKillPendingResumed` — WTX-04, TST-15/11, ELIM-07.
- Hook tests remain: `TestInboundRecoverCommitBeforeDelete`, `TestOutboxRecoverPublishBeforeAck`.
- Runbooks executed against Compose; `GET /health/ready` fails when Postgres or SQS is down.
