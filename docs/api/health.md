# Health checks

Public liveness and readiness probes (`init.md` §9). There is **no authentication** on `/health/*`.

Router: stdlib `net/http` ServeMux ([ADR 0002](../adr/0002-go-version-and-http-router.md)).
Shutdown behaviour: [ADR 0004](../adr/0004-fx-lifecycle-and-shutdown.md).

`Content-Type: application/json` on all responses below.

## `GET /health/live`

Process liveness: can this instance answer HTTP?

| Status | When |
| --- | --- |
| `200` | The HTTP server is running |

The probe does **not** check PostgreSQL or SQS. It stays `200` during graceful shutdown so
orchestrators treat drain as a readiness problem, not a crash.

```http
GET /health/live HTTP/1.1
```

```json
{"status":"ok"}
```

## `GET /health/ready`

Readiness of PostgreSQL and SQS. Also fails as soon as **shutdown starts**, even if both
dependencies still respond.

| Status | When |
| --- | --- |
| `200` | Process is not shutting down, PostgreSQL ping succeeds, and the configured SQS queues are reachable |
| `503` | Shutdown in progress, or any check failed |

| Status | When |
| --- | --- |
| `200` | Process is not shutting down, PostgreSQL ping succeeds, and the configured SQS queues are reachable |
| `503` | Shutdown in progress, or any check failed |

Success (dependency details are omitted; HTTP 200 is the signal):

```json
{"status":"ok"}
```

Unavailable (example: PostgreSQL down). `checks` lists each probe (`ok` or the error text):

```json
{
  "status": "unavailable",
  "checks": {
    "postgres": "postgres: ping: connection refused",
    "sqs": "ok"
  }
}
```

Unavailable because shutdown started:

```json
{
  "status": "unavailable",
  "checks": {
    "shutdown": "in progress"
  }
}
```

Clients must use the HTTP status (`200` vs `503`) as the primary signal; the body is diagnostic.

SQS readiness means the process can reach the broker and the queues named by `SQS_WAGER_QUEUE_NAME`,
`SQS_WAGER_DLQ_NAME` and `SQS_EVENTS_QUEUE_NAME` exist. It does **not** mean the inbound consumer
has drained the queue. The inbound worker consumes `wager-transactions.fifo`; the outbox publisher
publishes to the events queue.

## Authentication

None. Both endpoints are public. Business routes require OIDC ([auth.md](auth.md)); these probes stay public.
Prometheus scrape is also public: [`metrics.md`](metrics.md).
