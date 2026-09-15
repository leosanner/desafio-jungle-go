# 0001 — Package layout and layer boundaries

- **Status:** Accepted
- **Date:** 2026-09-15
- **Related requirements:** `init.md` §4 (composition; domain independent of Fx, HTTP, SQS and persistence); matrix IDs FX-06, STK (bootstrap)

## Context

The challenge requires Uber Fx for composition and forbids the domain from depending on Fx, HTTP, SQS or persistence libraries. Package organization is otherwise left to the candidate. Phase 1 needs a layout that keeps those boundaries enforceable from the first commit, scales to HTTP, PostgreSQL and SQS adapters, and has a single composition root for N-instance deployments.

## Options considered

### Option A — Hexagonal (ports and adapters) with an `internal/composition` root

```
cmd/wagering/
internal/config/
internal/domain/
internal/app/
internal/adapter/http
internal/adapter/postgres
internal/adapter/sqs
internal/composition/
migrations/
```

- Pros: Dependencies point inward; domain and use cases stay free of Fx and infrastructure; composition is the only Fx import site; `internal/` hides the module from accidental importers.
- Cons: More packages than a flat layout; ports must be declared by consumers (application/domain) rather than dumped into a shared `ports` package without thought.

### Option B — Flat `internal/` by technical layer without a dedicated composition package

Packages such as `internal/http`, `internal/db`, `internal/fx` mixed with domain types.

- Pros: Fewer directories at the start.
- Cons: Easy for domain to import adapters; Fx leaks into use cases; harder to prove FX-06.

### Option C — Feature folders (`internal/wallet`, `internal/wagering`) each owning HTTP + SQL + domain

- Pros: Local cohesion per feature.
- Cons: Cross-feature financial transactions (wallet + ledger + inbox + outbox in one commit) fight package cycles; Fx wiring tends to duplicate; domain isolation is harder to audit.

## Decision

The module path is `github.com/leosanner/desafio-jungle-go`.

Layout:

| Path | Responsibility |
| --- | --- |
| `cmd/wagering/` | Process entrypoint: load env, construct the Fx app, run it |
| `internal/config/` | Configuration structs and validation (no Fx types required) |
| `internal/domain/` | Entities, value objects, domain errors, ports that the domain owns. **Must not** import `go.uber.org/fx`, `net/http`, the AWS SDK, `pgx`, or `database/sql` |
| `internal/app/` | Use cases. Orchestrates domain + ports. **Must not** import Fx |
| `internal/adapter/http` | HTTP handlers and routing (`net/http`) |
| `internal/adapter/postgres` | PostgreSQL adapters (`pgx`) |
| `internal/adapter/sqs` | SQS adapters (AWS SDK) |
| `internal/composition/` | Fx modules only (`fx.Module`, `fx.Provide`, `fx.Invoke`, lifecycle). The only package besides `cmd/` that imports Fx |
| `migrations/` | Versioned SQL (golang-migrate); not a Go package |

Dependencies point inward: adapters → application → domain. Composition imports all of them to wire constructors. `cmd/wagering` imports composition (and config loading as needed) only.

Further adapters (e.g. Keycloak) land under `internal/adapter/` when that work starts; they are not introduced in this ADR.

## Consequences

- Positive: FX-06 is structural; `go list -deps` on `internal/domain` is a mechanical check; later ADRs can add ports without moving packages.
- Negative / accepted risks: Phase 1 looks “empty” in `internal/domain` and `internal/app` until domain work starts; that is intended.
- Known limitations: SQL transaction / unit-of-work types are not defined here (later ADR). Exact port interface names are left to implementation.

## Verification

- Directory tree matches the table above.
- `go list -deps ./internal/domain/...` has no `fx`, `net/http`, AWS, `pgx` or `database/sql`.
- `go list -deps ./internal/app/...` has no `go.uber.org/fx`.
- Fx imports are confined to `internal/composition` and `cmd/wagering`.
