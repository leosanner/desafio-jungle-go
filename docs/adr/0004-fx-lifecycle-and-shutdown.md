# 0004 — Fx lifecycle and shutdown

- **Status:** Accepted
- **Date:** 2026-09-15
- **Related requirements:** `init.md` §4 (Fx modules, lifecycle, validation, workers, shutdown, close order); §10 SIGTERM on the SQS consumer; matrix IDs FX-01, FX-02, FX-03, FX-04, FX-05, FX-06, TST-06

## Context

Uber Fx must compose configuration, connections, repositories, use cases, handlers and workers. Start must validate config and dependencies. Shutdown must stop new input, finish or release in-flight work, then close dependencies after the components that use them. Phase 1 has an HTTP server and no background workers yet; the HTTP pattern and the worker pattern need to be decided together so later consumers (SQS, outbox, pending references) do not invent a second lifecycle.

## Options considered

### Option A — `fx.Module` per area, hooks on the resource constructor, env start/stop timeouts

- Pros: Matches §4; reverse `OnStop` order follows construction; HTTP Listen-sync / Serve-async / Shutdown-on-stop is the usual safe pattern; workers get a documented cancel → finish/release → `done` channel contract; readiness can fail as soon as shutdown starts.
- Cons: More modules than a single `fx.New` blob; authors must register hooks on the constructor that owns the resource, not in `main`.

### Option B — Manual `main` with `errgroup` and a stop channel, Fx unused beyond `Provide`

- Pros: Explicit goroutine control.
- Cons: Violates the mandatory Fx composition requirement; easy to close the pool before the server.

### Option C — Register all `OnStop` hooks in one composition `Invoke`

- Pros: Shutdown order visible in one file.
- Cons: Reverse-order guarantees are easy to break when a new dependency is added; resource lifetime is detached from its constructor.

## Decision

- **Modules:** one `fx.Module` per area (at least `config`, `postgres`, `sqs`, `http`; later `auth`, `wagering`, `outbox`, workers). Modules are aggregated in `internal/composition`.
- **Constructors:** plain functions (`func NewX(...) (*X, error)`). Avoid `fx.In`/`fx.Out` unless groups/names are required. Never in domain or use cases (ADR 0001).
- **Config:** loaded and **validated** in its provider; a validation error fails `app.Start`.
- **Timeouts:** `fx.StartTimeout` and `fx.StopTimeout` come from env `FX_START_TIMEOUT` and `FX_STOP_TIMEOUT` (Go duration strings).
- **HTTP:** `OnStart` does `net.Listen` **synchronously** (bind failure fails start) and `Serve` **asynchronously**. `OnStop` calls `Shutdown` on the `http.Server` (stop accepting, wait for in-flight requests within the stop context).
- **Readiness:** `GET /health/ready` starts failing as soon as shutdown begins, even if PostgreSQL and SQS are still up. Liveness is process aliveness and is not used as a drain signal.
- **Workers (when added):** cancel fetch of new work; finish or release in-flight work within the `OnStop` context; expose an observable `done` channel (or equivalent) so termination is waitable. Phase 1 does not start workers.
- **Hook placement:** resource `OnStart`/`OnStop` hooks are registered on the constructor that creates the resource (pool, server, client). Fx runs `OnStop` in reverse order, so the pool closes after HTTP/workers that depend on it.
- **Invoke:** only to start components that must run (HTTP server; later workers), not for business logic.

## Consequences

- Positive: FX-01/02/04/05 have a concrete pattern; HTTP drain and ready-fail-on-shutdown are specified before Kubernetes-style probes exist; workers can copy one skeleton.
- Negative / accepted risks: stop timeout too low will interrupt in-flight requests; operators must size `FX_STOP_TIMEOUT` for the slowest in-flight unit of work.
- Known limitations: FX-03 is specified but not implemented until SQS/outbox/pending-reference workers exist. Start-time dependency checks (Postgres ping, SQS queue exists) are expected of those modules when wired; Phase 1 readiness is the operator-facing check.

## Verification

- `fx.ValidateApp` (or `fxtest`) on the composition graph.
- Config validation fails start on missing/invalid env.
- HTTP: listen failure fails start; `Shutdown` is invoked on stop.
- Ready returns non-success once shutdown has started ([`docs/api/health.md`](../api/health.md)).
- Later: worker `done` closed on stop; no leaked goroutines (optional `goleak`).
