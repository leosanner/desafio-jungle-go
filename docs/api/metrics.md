# Metrics

Prometheus scrape endpoint (`init.md` §12, [ADR 0021](../adr/0021-observability-logs-and-metrics.md)).

`GET /metrics` is **public** (same policy as [`health.md`](health.md)): it is not a business
route, so it does not require a Bearer token. Cardinality is bounded by HTTP **patterns**, never
by wallet or transaction ids.

`Content-Type` is Prometheus text (`text/plain; version=0.0.4` or OpenMetrics).

```http
GET /metrics HTTP/1.1
```

## Catalog

Prefix `wagering_`. Counters are monotonic per process. Gauges are the last observed value on
that instance.

| Name | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `wagering_http_requests_total` | counter | `method`, `pattern`, `code` | Finished business HTTP requests |
| `wagering_http_request_duration_seconds` | histogram | `method`, `pattern` | Business HTTP latency |
| `wagering_operations_total` | counter | `kind`, `status`, `channel` | Persisted operation outcomes (`http` or `sqs`) |
| `wagering_operation_duration_seconds` | histogram | `kind`, `channel` | `Submit` / `HandleInbound` wall time |
| `wagering_duplicates_total` | counter | `kind`, `channel` | Idempotent replays |
| `wagering_conflicts_total` | counter | `reason` | Concurrency / identity conflicts |
| `wagering_sqs_retries_total` | counter | — | Inbound visibility backoff |
| `wagering_sqs_dlq_total` | counter | — | Messages sent to the inbound DLQ |
| `wagering_outbox_unpublished` | gauge | — | Rows with `published_at IS NULL` |
| `wagering_outbox_oldest_unpublished_seconds` | gauge | — | Age of the oldest unpublished row (`0` if none) |
| `wagering_outbox_published_total` | counter | — | Rows marked published in a tick |
| `wagering_outbox_publish_errors_total` | counter | — | Publish failures that scheduled retry |
| `wagering_reconciliation_divergences_total` | counter | — | `ReconcileWallet` found stored ≠ ledger sum |

`channel` is `http` or `sqs`. `kind` is the domain kind (`BET`, `WIN`, …). `status` is the
persisted transaction status (`PROCESSED`, `REJECTED`, `PENDING_REFERENCE`, …).

`reason` values:

| `reason` | When |
| --- | --- |
| `idempotency_payload` | Same idempotency key, different canonical hash |
| `duplicate_external` | Same `(providerId, externalTransactionId)` under another key |
| `optimistic_lock` | Wallet `UPDATE … version` affected 0 rows |
| `unavailable` | Retryable persistence (`ErrUnavailable`, deadlock / serialization) |

Health probes and `GET /metrics` are **not** included in the HTTP counter/histogram.

## Logs

JSON `log/slog` ([ADR 0002](../adr/0002-go-version-and-http-router.md)). Correlation:

| Field | HTTP | SQS inbound |
| --- | --- | --- |
| `correlationId` | `X-Correlation-Id` (generated UUID v7 if missing/invalid) | Envelope `messageId` |
| `providerId` | Token `azp` when the caller is a provider | `data.providerId` |
| `transactionId` / `walletId` | After a successful use case | After `HandleInbound` |
| `messageId` | — | Envelope `messageId` |

The response repeats `X-Correlation-Id`. Credentials, Bearer tokens, request bodies and money
amounts are never log fields.
