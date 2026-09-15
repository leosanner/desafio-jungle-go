# Roadmap

Proposed phases. Each phase ends with passing tests, an updated traceability matrix and ADRs for the
decisions made. The order prioritizes the highest-weighted criteria (§14) and minimizes rework.

| Phase | Deliverable | Main requirements | Status |
| --- | --- | --- | --- |
| 0 | Repository structure, docs and agent skills | — | ✅ |
| 1 | Bootstrap: `go.mod`, package layout, config, base Fx, health checks, Dockerfile, Compose (PG, Keycloak, LocalStack), empty migrations, local checks (`vet`, `gofmt`, `-race`) | STK, FX, HTTP-13 | ✅ |
| 2 | Pure domain: `Money`, `Wallet`, `WagerTransaction`, `WalletLedgerEntry`, errors and events, with unit tests | MON, WAL, WTX, LED, DOM, TST-01..03 | ✅ |
| 3 | Persistence: schema with constraints, ledger immutability, unit of work, repositories | GAR-03/05/07/08, TST-04 | ✅ |
| 4 | Authentication and authorization with Keycloak | AUTH, TST-07 | — |
| 5 | HTTP use cases: opening, operations, idempotency, reads, reconciliation | HTTP, OPS, CON | — |
| 6 | Outbox and concurrent publisher | OBX, TST-13 | — |
| 7 | SQS consumer with inbox, retry and DLQ | SQS, INB, TST-12 | — |
| 8 | Pending references and `PENDING` resumption | OPS-10..12, WTX-04, TST-14/15 | — |
| 9 | Multiple instances and failure simulation | CON-02, TST-11, runbooks | — |
| 10 | Observability: logs, metrics | OBS | — |
| 11 | Delivery: README, ARCHITECTURE, `.env.example`, final audit | ENT, ELIM | — |
