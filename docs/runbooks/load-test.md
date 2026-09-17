# Load test

Optional differential (`init.md` §14). Decision: [ADR 0022](../adr/0022-load-test-harness.md).
There is **no RPS goal**. A passing run means financial invariants held and the
outbox drained, not a capacity SLO.

Correctness under concurrency is still proven by tagged tests
([`integration.md`](integration.md), [`multiple-instances.md`](multiple-instances.md)). This
harness adds **throughput, p50/p95/p99, errors, conflicts and outbox lag** against a process
that is already running.

## Prerequisites

- Docker Compose dependencies up ([`test-dependencies.md`](test-dependencies.md))
- A ready wagering process whose `OIDC_ISSUER` matches the token `iss`
- Host Go 1.25+

**Issuer trap:** host tokens from `http://localhost:8081/realms/wagering` are rejected by the
Compose `app` service (`iss` `http://keycloak:8080/realms/wagering`). Use host binaries with
host tokens, or issue tokens on the Compose network and call `http://app:8080`.

## Environment

Defaults match [`.env.example`](../../.env.example) and [`docs/api/auth.md`](../api/auth.md)
(local dummy client secrets only).

| Variable | Default | Meaning |
| --- | --- | --- |
| `WAGERING_BASE` | `http://127.0.0.1:8080` | Single process |
| `WAGERING_BASES` | — | Comma-separated bases; round-robin (overrides `WAGERING_BASE`) |
| `OIDC_ISSUER` | `http://localhost:8081/realms/wagering` | Must match the process |
| `OIDC_INTERNAL_CLIENT` | `wagering-internal` | Wallet / reconciliation |
| `LOADTEST_INTERNAL_SECRET` | `internal-secret` | Local dummy |
| `LOADTEST_PROVIDER_CLIENT` | `provider-a` | Bets |
| `LOADTEST_PROVIDER_SECRET` | `provider-a-secret` | Local dummy |
| `LOADTEST_DISTINCT_WALLETS` | `80` | Scenario A size |
| `LOADTEST_REPLAY_N` | `120` | Scenario B size |
| `LOADTEST_CONTENTION_N` | `40` | Scenario C size (10.00 bets on 100.00) |
| `LOADTEST_CONCURRENCY` | `64` | Max in-flight HTTP calls |
| `LOADTEST_HTTP_TIMEOUT` | `15s` | Per request |
| `LOADTEST_OUTBOX_WAIT` | `30s` | Deadline for `wagering_outbox_unpublished == 0` |

## Methodology

Closed bursts (all workers start behind a barrier). Client-side latency. Amounts are JSON
strings (`"10.00"`). After each scenario: `GET /wallets/{id}` and
`POST /wallets/{id}/reconciliation`. After all three: poll public `GET /metrics` until
unpublished is 0 (or the wait expires).

| Scenario | Load | Expected invariant |
| --- | --- | --- |
| Distinct wallets | N wallets, one 10.00 BET each in parallel | N `PROCESSED`, every balance `"90.00"`, reconciliation consistent |
| Replay burst | Same BET N times in parallel | 1 `PROCESSED`, N−1 `idempotentReplay`, balance `"90.00"` |
| Same-wallet contention | N distinct 10.00 BETs on `"100.00"` | 10 processed, N−10 `INSUFFICIENT_FUNDS`, balance `"0.00"` |

JSON on stdout includes `throughput_rps`, `latency.p50_ms` / `p95_ms` / `p99_ms`, HTTP 5xx /
4xx counts, `wagering_conflicts_total` by reason, duplicate BET HTTP counter, outbox
unpublished / drain wait / publish errors. Exit **0** only when `all_invariants_ok` is true
(three scenarios plus drained outbox). Exit **1** on invariant or drain failure; **2** on
bad config. Tokens are never printed.

Burst RPS is `requests / wall_s` over a tiny window. Do not quote it as sustained capacity.
Default `OUTBOX_BATCH_SIZE=10` can leave a few seconds of unpublished lag after the HTTP
path has already committed; that lag is the measurement, not a failure by itself, as long
as it reaches 0 inside `LOADTEST_OUTBOX_WAIT`.

## Host process (usual)

```sh
docker compose up -d postgres keycloak localstack
set -a && source .env.example && set +a
go run ./cmd/wagering
```

In another terminal, wait for ready then run the harness:

```sh
set -a && source .env.example && set +a
curl -sS -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8080/health/ready
go run ./cmd/loadtest
```

Three host processes (round-robin):

```sh
HTTP_ADDR=:8080 go run ./cmd/wagering
HTTP_ADDR=:8082 go run ./cmd/wagering
HTTP_ADDR=:8083 go run ./cmd/wagering
WAGERING_BASES=http://127.0.0.1:8080,http://127.0.0.1:8082,http://127.0.0.1:8083 go run ./cmd/loadtest
```

Compose overlay ports: [`multiple-instances.md`](multiple-instances.md). Tokens must be
issued with the in-network issuer if the processes are Compose containers.

## How to read the report

- `distinct_wallets.invariants.ok`, `idempotent_replay_burst.invariants.ok`,
  `same_wallet_contention.invariants.ok` — financial outcome.
- `http_errors` must be 0. `client_errors` on the contention scenario are the expected 422s.
- `outbox_after.drained` and `outbox_after.wait_s` — publish lag.
- `outbox_after.metrics.conflicts_*` — catalog counters at scrape time (process lifetime,
  not only this burst).
- `outbox_after.metrics.duplicates_bet_http` — should move by `LOADTEST_REPLAY_N - 1` on a
  quiet process.

Offline tests of percentiles, scrape parsing and invariant evaluation:

```sh
go test ./cmd/loadtest/...
```
