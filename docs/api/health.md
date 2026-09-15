# Health checks

Public liveness and readiness probes (`init.md` §9). These are the only HTTP routes in Phase 1.
There is **no authentication** on `/health/*`.

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

SQS readiness means the process can reach the broker and the queues named by `SQS_WAGER_QUEUE_NAME`
and `SQS_WAGER_DLQ_NAME` exist. It does **not** mean a consumer is running (Phase 1 has none).

## Authentication

None. Both endpoints are public. Business routes (later) require OIDC; these probes stay public.
