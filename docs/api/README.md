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

Phase 2 documents the domain `failureCode` catalog. HTTP status mapping and wagering routes remain
TBD until the HTTP phase.

## Index

| Document | Endpoints |
| --- | --- |
| [health.md](health.md) | `GET /health/live`, `GET /health/ready` (public, unauthenticated) |
| [failure-codes.md](failure-codes.md) | Domain `failureCode` catalog (HTTP mapping later) |
