# 0022 — Optional load-test harness as a host Go command

- **Status:** Accepted
- **Date:** 2026-09-17
- **Related requirements:** `init.md` §14 (optional load tests: reproducible command, environment,
  methodology, throughput, p50/p95/p99, errors, conflicts, outbox lag; no RPS goal); ENT-07

## Context

Mandatory concurrency and recovery are already proven by tagged tests and runbooks
([ADR 0005](0005-test-strategy-initial.md), [ADR 0020](0020-multi-instance-and-failure-injection.md)).
`init.md` §14 still lists load tests as an optional differential. They must ship a **reproducible
command**, not a one-off script, and report throughput, latency percentiles, errors, conflicts and
outbox lag. There is no minimum RPS. Default `go test ./...` must stay container-free.

## Options considered

### Option A — `go run ./cmd/loadtest` against a running process

A small stdlib HTTP client. Three closed bursts (distinct wallets, same-key replay, same-wallet
funds race). JSON on stdout. Exit 1 if financial invariants fail or the outbox does not drain.
Optional round-robin across `WAGERING_BASES` for the Compose overlay / three host processes.

- Pros: No extra runtime (k6, Python, vegeta); same Go module; default unit tests stay offline;
  numbers match the catalog on `GET /metrics`; operator path matches `go run ./cmd/wagering`.
- Cons: Not a soak/capacity study; RPS is burst rate over a tiny window; needs a live process and
  matching `OIDC_ISSUER`.

### Option B — k6 / vegeta / hey script in `docs/runbooks/`

- Pros: Familiar load-test UX; richer open-loop schedulers.
- Cons: Extra toolchain; money still has to be asserted out of band; duplicates the HTTP contract
  in another language.

### Option C — `go test -tags=load` that boots the app

- Pros: One binary, skip-if-no-env like integration.
- Cons: Easy to confuse with TST-* proofs; load is optional and must not look mandatory; starting
  Fx inside a test hides the operator-shaped “running process + scrape /metrics” path §14 asks for.

## Decision

Ship **option A**.

- Command: `go run ./cmd/loadtest` (documented in [`docs/runbooks/load-test.md`](../runbooks/load-test.md)).
- Not part of `go test ./...` or `-tags=integration`. Unit tests in `cmd/loadtest` cover
  percentiles, Prometheus text parsing and invariant evaluation only (no network).
- Amounts stay JSON strings (`"10.00"`). `float64` is used only for **latency milliseconds**,
  **RPS** and Prometheus scrape values (seconds/counts), never for money.
- No RPS target. A passing run means invariants held and the outbox drained, not a throughput SLO.
- Dummy Keycloak secrets are the same local values as [`docs/api/auth.md`](../api/auth.md).

## Consequences

- Positive: §14 optional evidence is reproducible from a clean checkout; outbox lag under burst
  becomes visible (`OUTBOX_BATCH_SIZE` vs unpublished gauge).
- Negative / accepted risks: host tokens cannot call a Compose `app` whose `iss` is
  `http://keycloak:8080/...` ([ADR 0011](0011-oidc-keycloak-auth.md)). The runbook states both
  modes. Burst RPS must not be quoted as capacity.
- Known limitations: no soak, no k6, no tracing correlation of the burst, no in-process Fx boot.

## Verification

- `go test ./cmd/loadtest/...` (offline).
- Runbook command against a ready process: JSON includes `throughput_rps`, `latency.p50_ms` /
  `p95_ms` / `p99_ms`, HTTP error counts, `wagering_conflicts_total`, outbox unpublished / drain
  wait, and `all_invariants_ok`.
- Exit code 0 only when the three scenario invariants hold and unpublished reaches 0.
