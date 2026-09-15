# 0002 — Go version, HTTP router and logging

- **Status:** Accepted
- **Date:** 2026-09-15
- **Related requirements:** `init.md` §4 (Go version in `go.mod` and Dockerfile; HTTP `net/http` or a router); §9 health checks; §12 JSON logs; matrix IDs STK-01, HTTP-13, OBS-01

## Context

The challenge requires a declared Go version in both `go.mod` and the Dockerfile, an HTTP stack of either stdlib `net/http` or a third-party router, and JSON logs with correlation identifiers (full observability comes later). Phase 1 only needs a server that can serve public health checks, so the bootstrap toolchain should stay small and stdlib-first.

## Options considered

### Option A — Go 1.25, stdlib `ServeMux`, `log/slog` JSON

- Pros: Method+path patterns (`GET /health/live`) are built into `net/http` since Go 1.22; no extra HTTP dependency; `slog` is stdlib structured logging with a JSON handler; version is current and declared once.
- Cons: Less middleware ecosystem than Chi/Echo; correlation-ID helpers must be written (acceptable; not needed for Phase 1 health).

### Option B — Third-party router (Chi, Echo, or Gin)

- Pros: Batteries for middleware, groups, and param helpers.
- Cons: Extra dependency for a surface that Phase 1 only uses for two health routes; Gin’s default logger is not `slog`; diverges from “stdlib unless justified”.

### Option C — Older Go (1.21 or 1.22) with a third-party mux for method routing

- Pros: Wider distro compatibility.
- Cons: The challenge asks to declare the version in use; 1.25 is available; older toolchains lose recent `ServeMux` and `slog` improvements we would then replace with libraries.

## Decision

- **Go 1.25** is the language version declared in `go.mod` (`go 1.25`) and in the application Dockerfile.
- **HTTP:** stdlib `net/http` with `ServeMux` method+path patterns. No third-party HTTP router.
- **Logging:** `log/slog` with a JSON handler. Log level comes from `LOG_LEVEL`. Credentials, secrets and full financial payloads must never be logged (enforced as the service grows; Phase 1 has no financial payloads).

Metrics, tracing and the exact set of correlation fields on every log line remain later work (see pending ADR on observability beyond JSON `slog`).

## Consequences

- Positive: Minimal HTTP surface; health routes map 1:1 to mux patterns; logging matches §12’s JSON requirement without a logging framework.
- Negative / accepted risks: Custom middleware (auth, correlation IDs) will be written against `http.Handler` rather than a router’s plugin API.
- Known limitations: OBS-01 is not complete until correlation IDs and redaction are implemented on business paths.

## Verification

- `go.mod` contains `go 1.25`; Dockerfile uses the same major.minor.
- HTTP handlers are registered on `net/http.ServeMux` (or `http.NewServeMux`); `go.mod` has no Chi/Echo/Gin (or equivalent) module.
- Application logs are JSON objects produced by `slog`.
- `GET /health/live` and `GET /health/ready` are served as documented in [`docs/api/health.md`](../api/health.md).
