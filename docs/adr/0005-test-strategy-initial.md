# 0005 — Test strategy (initial)

- **Status:** Accepted
- **Date:** 2026-09-15
- **Related requirements:** `init.md` §13 (unit, integration with real PostgreSQL/IdP/SQS, Fx start/stop, `-race`); §15 commands; matrix IDs TST-06, TST-18, ENT-06, ENT-07, ELIM-10

## Context

The challenge forbids replacing PostgreSQL, SQS and the IdP with mocks in integration tests, requires `go test -race` on applicable tests, and asks for documented build tags, multi-instance runs and failure injection. Phase 1 has no domain and almost no adapters to integrate; the strategy must still make `go test ./...` cheap on a clean checkout while leaving a slot for real-container tests.

## Options considered

### Option A — Default unit tests; `integration` build tag for real containers

- Pros: `go test ./...` stays runnable without Docker; integration tests cannot be accidentally skipped by CI that intends to run them (explicit tag); matches the testing skill.
- Cons: Developers must remember `-tags=integration`; Phase 1 may have few or no tagged tests.

### Option B — Every package test boots Compose / testcontainers

- Pros: Always “real”.
- Cons: Slow default loop; violates the spirit of unit tests for pure domain; Phase 1 would be blocked on containers for config and mux tests.

### Option C — Integration tests without a build tag, skipped via `testing.Short`

- Pros: One command in CI with `-short` locally.
- Cons: Easy to skip the tests the challenge cares about; `Short` is the wrong signal (speed vs infrastructure).

## Decision

| Level | Infra | How to run |
| --- | --- | --- |
| Unit | None | `go test ./...` (default). Fakes/stubs for ports. No containers. |
| Integration | Real PostgreSQL, Keycloak, LocalStack | `go test -tags=integration ./...` (exact packages documented when tests exist) |
| Multi-instance / failure | Compose or multiple processes | Runbooks under `docs/runbooks/` (placeholders until those phases) |

- Integration tests **must not** mock away PostgreSQL, Keycloak and SQS together. Fakes are allowed in unit tests and for dependencies outside the challenge scope.
- Phase 1 does **not** fully implement integration tests. Optional tests may skip when required env/containers are absent (`skip-if-no-env`); that is a convenience, not a substitute for later real-container CI.
- Always keep `go test -race ./...`, `go vet ./...` and `gofmt -l .` as local/CI checks.

**Phase 1 is considered proven when:**

- config validation is tested (invalid env fails without containers);
- health handlers are tested with fakes (live OK; ready OK/fail based on dependency fakes; ready fails when shutdown has started);
- `fx.ValidateApp` covers the composition graph;
- `gofmt -l .` is empty, `go vet ./...` is clean, and `go test -race ./...` passes on the unit suite.

Multi-instance contention, crash-after-commit, and broker/database unavailability remain later phases; runbooks may exist as placeholders.

## Consequences

- Positive: Default `go test ./...` matches §15; ELIM-10 is not “solved” by fakes because integration stays tagged and real.
- Negative / accepted risks: until tagged tests exist, integration requirements stay `—` / `🚧` in the matrix; skip-if-no-env can hide missing Compose if CI never passes `-tags=integration`.
- Known limitations: TST-01..05, TST-07..17, CON-02 and failure-injection ADRs are not opened by this decision.

## Verification

- README and `CLAUDE.md` document `go test ./...`, `go test -race ./...`, `go vet ./...`, `gofmt -l .`, and the `integration` tag.
- Phase 1 unit tests do not require Docker.
- [`docs/runbooks/test-dependencies.md`](../runbooks/test-dependencies.md) describes how to start PostgreSQL, Keycloak and LocalStack for later integration tests.
