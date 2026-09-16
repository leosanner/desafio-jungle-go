# Runbooks

Step-by-step procedures, reproducible from a clean checkout, for the scenarios the challenge asks to be
documented separately (`init.md` §13 and §15).

Each runbook must include: prerequisites, exact commands, expected outcome and how to verify it
(SQL queries, HTTP calls, metrics).

## Index

| Runbook | Scenario | Status |
| --- | --- | --- |
| [test-dependencies.md](test-dependencies.md) | Start PostgreSQL, Keycloak and LocalStack/MiniStack for tests | 🚧 Phase 4 (TST-04 Postgres; TST-07 Keycloak) |
| [integration.md](integration.md) | Build tags, TST-04 (`POSTGRES_DSN`) and TST-07 (`OIDC_ISSUER`) | 🚧 |
| _to be created_ — multiple instances | ≥3 contending processes | — |
| _to be created_ — failure simulation | Crash after commit, between publish and ack, PG/SQS unavailability | — |
| _to be created_ — pending references | Reversal before its reference, resolution and expiry | — |
| _to be created_ — load test (optional) | Methodology, throughput, p50/p95/p99, conflicts, outbox lag | — |
