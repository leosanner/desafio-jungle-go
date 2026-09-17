# Runbooks

Step-by-step procedures, reproducible from a clean checkout, for the scenarios the challenge asks to be
documented separately (`init.md` §13 and §15).

Each runbook must include: prerequisites, exact commands, expected outcome and how to verify it
(SQL queries, HTTP calls, metrics).

## Index

| Runbook | Scenario | Status |
| --- | --- | --- |
| [test-dependencies.md](test-dependencies.md) | Start PostgreSQL, Keycloak and LocalStack for tests | ✅ |
| [integration.md](integration.md) | Build tags, TST-04 (`POSTGRES_DSN`), TST-07 (`OIDC_ISSUER`), Phases 5–10 | ✅ |
| [multiple-instances.md](multiple-instances.md) | ≥3 contending processes (host binaries or Compose overlay) | ✅ |
| [failure-simulation.md](failure-simulation.md) | Crash after commit, publish-before-ack, `kill -9`, PG/SQS unavailability | ✅ |
| [pending-references.md](pending-references.md) | Reversal before its reference, resolution and expiry | ✅ |
| [load-test.md](load-test.md) | Optional burst: throughput, p50/p95/p99, conflicts, outbox lag | ✅ |

Load tests are an optional differential in `init.md` §14. Command: `go run ./cmd/loadtest`.
There is no RPS goal. [ADR 0022](../adr/0022-load-test-harness.md).
