# 0021 — Observability: correlation logs and Prometheus metrics

- **Status:** Proposed
- **Date:** 2026-09-16
- **Related requirements:** `init.md` §12; matrix IDs OBS-01, OBS-02, OBS-03, HTTP-12

## Context

JSON `log/slog` and `LOG_LEVEL` are already chosen ([ADR 0002](0002-go-version-and-http-router.md)).
Phase 5 counts reconciliation divergences on a narrow `app.Metrics` port. The challenge still
requires correlation identifiers on operational logs, redaction of credentials and full financial
payloads, and a metrics catalog covering status, duplicates, retries, DLQ, concurrency conflicts,
outbox lag, processing latency and reconciliation. OpenTelemetry tracing and dashboards are
optional (OBS-03).

## Options considered

### Option A — `slog` JSON + `prometheus/client_golang` + public `GET /metrics`; no tracing

Keep stdlib JSON logs. Middleware generates or echoes `X-Correlation-Id` and logs
`correlationId`, `providerId`, `transactionId`, `walletId`, `messageId` when known. A
`ReplaceAttr` denylist redacts credential and payload keys. Application code records through
`app.Metrics`; the adapter is Prometheus on a process-local registry scraped at `GET /metrics`
(public, like health — not a business route).

- Pros: Matches the required catalog with one extra library; scrape is the usual operator
  interface; domain/app stay free of Prometheus and `net/http`; histograms give processing
  latency without a tracing backend.
- Cons: No distributed traces; `/metrics` is unauthenticated (cardinality is bounded by route
  pattern, not raw path).

### Option B — OpenTelemetry SDK for logs, metrics and traces

- Pros: One vendor-neutral pipeline; OBS-03 included.
- Cons: Heavier than the required 5-point observability score; needs a collector to be useful;
  still need an exporter for the same counters.

### Option C — Ad-hoc JSON counters on an internal endpoint

- Pros: No new module.
- Cons: No histogram buckets, no standard scrape, more custom code than `client_golang`.

## Decision

**Option A.**

1. **Logs (OBS-01):** `log/slog` JSON remains. HTTP sets `X-Correlation-Id` (echo client value if
   it is 1–128 printable ASCII without `"` or `\`; otherwise UUID v7). SQS uses envelope
   `messageId` as `correlationId`. Business logs include the identifiers that exist for that
   path and **never** Authorization headers, tokens, secrets or full financial payloads (no
   `money` / `amount` / request body). `config.NewLogger` applies a key denylist.
2. **Metrics (OBS-02):** `app.Metrics` is the port. Production implementation:
   `internal/adapter/metrics` (`prometheus/client_golang`) with a **private** registry so tests
   do not share global state. Catalog: [`docs/api/metrics.md`](../api/metrics.md).
   `GET /metrics` is public. HTTP cardinality uses `Request.Pattern` (`POST /wallets/{walletId}/…`),
   never raw IDs. Outbox lag is `COUNT(*)` / `MIN(occurred_at)` of unpublished rows (including
   leased). Durations are `time.Duration` on the port; only the Prometheus adapter converts to
   seconds (not money).
3. **Tracing (OBS-03):** not implemented. A later ADR may add OpenTelemetry without changing the
   log field names or the Prometheus catalog.

## Consequences

- Positive: OBS-01/02 and HTTP-12 have a concrete catalog and scrape endpoint; N instances each
  expose their own `/metrics`.
- Negative / accepted risks: unauthenticated scrape; operators must not put the process on a
  public network without a scrape ACL. No traces across HTTP and SQS beyond `correlationId` /
  `messageId`.
- Known limitations: health and `/metrics` themselves are omitted from access logs and HTTP
  histograms to avoid probe noise.

## Verification

- Unit: correlation header echo/generate; log capture has identifiers and does not contain a
  Bearer token or amount string; `RecordingMetrics` on replay, payload conflict and
  reconciliation divergence; Prometheus registry scrape includes catalog names.
- Integration: unpublished outbox `Lag` query (`POSTGRES_DSN`).
- Manual: `curl -sS http://localhost:8080/metrics` after `docker compose up --build`.
