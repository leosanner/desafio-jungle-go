# HTTP contracts

Documentation for the endpoints exposed by the service (`init.md` §9). One file per resource group.
This README is the index.

Health probes and `GET /metrics` stay public. Every other registered route requires a Bearer token.

Shared conventions live in the files below: authentication per route, request/response examples,
HTTP status catalog, `failureCode` catalog (correctable × definitive), `Idempotency-Key` / canonical
hash, and ledger pagination (opaque cursor, ordering, limits).

## Index

| Document | Endpoints |
| --- | --- |
| [health.md](health.md) | `GET /health/live`, `GET /health/ready` (public, unauthenticated) |
| [metrics.md](metrics.md) | `GET /metrics` (public Prometheus scrape), log fields |
| [auth.md](auth.md) | Bearer JWT, `providerId` from `azp`, 401/403, local clients |
| [status.md](status.md) | HTTP status catalog and error body |
| [wallets.md](wallets.md) | `POST/GET /wallets`, ledger, reconciliation (internal) |
| [wagering.md](wagering.md) | `POST/GET` wagering transactions, idempotency |
| [failure-codes.md](failure-codes.md) | Domain `failureCode` catalog and HTTP mapping |
