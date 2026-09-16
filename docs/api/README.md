# HTTP contracts

Documentation for the endpoints exposed by the service (`init.md` §9). One file per resource group once
content grows; this README keeps the index and shared conventions.

## Expected contents

- Authentication required per endpoint (scope/role/client) and public endpoints (`/health/*`).
- Request and response for each endpoint, with examples.
- HTTP status catalog and a standardized error body, distinguishing:
  invalid input · conflict (idempotency/duplicate) · business rejection · pending processing ·
  transient unavailability · unauthenticated · unauthorized.
- `failureCode` catalog: code, meaning, correctable × definitive, associated HTTP status.
- `Idempotency-Key` rules, canonical hash algorithm and normalizations.
- Ledger pagination (opaque cursor, ordering, limits).

Phase 4 documents authentication for the registered business paths. Financial handlers
remain stubs (`501`) until the HTTP phase. Health probes stay public.

## Index

| Document | Endpoints |
| --- | --- |
| [health.md](health.md) | `GET /health/live`, `GET /health/ready` (public, unauthenticated) |
| [auth.md](auth.md) | Bearer JWT, `providerId` from `azp`, 401/403/501, local clients |
| [failure-codes.md](failure-codes.md) | Domain `failureCode` catalog (HTTP mapping later) |
