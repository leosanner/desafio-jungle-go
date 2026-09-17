# 0011 — OIDC authentication with Keycloak

- **Status:** Accepted
- **Date:** 2026-09-16
- **Related requirements:** `init.md` §2 (IdP, `client_credentials`, `providerId` from identity, provider isolation, internal-only wallets, broker credentials); §4 authentication; §9 health stays public; §13 TST-07; §14 ELIM-01/02; §15 ENT-04/05; matrix IDs AUTH-01..07, ELIM-01/02, TST-07, ENT-04

## Context

Every business HTTP endpoint must authenticate against an **external** OAuth 2.0/OIDC IdP. The service must not register passwords or mint its own tokens. The authenticated identity determines the authorized `providerId`. Providers may only see and replay their own transactions; wallet open/read/ledger/reconciliation is restricted to an internal service. Messaging access is controlled by broker credentials, while the consumer still applies domain validation.

Keycloak is already in Compose (Phase 1) but unused. Phase 4 introduces validation and provisioning; Phase 5 fills in the financial handlers behind the same gates.

## Options considered

### Option A — Keycloak realm import + `client_credentials` + JWKS (go-oidc)

Confidential clients obtain access tokens with `client_credentials`. The API is a resource server: Bearer JWT, signature via JWKS, `iss` / `aud` / `exp` / RS256. `providerId` is the token `azp` (OAuth client id) except for the configured internal client. Realm JSON is imported on Keycloak start.

- Pros: Matches the challenge recommendation; automatic local identities (ENT-04); no password database in the app; JWKS rotation is handled; N instances share the IdP, not in-memory sessions; `azp` is a stable, auditable provider identity (AUTH-03) that cannot be taken from the body/path alone.
- Cons: Hostname/issuer split between Compose DNS (`keycloak:8080`) and host (`localhost:8081`) needs an optional JWKS URL; access tokens are used with an ID-token verifier (standard for JWT access tokens).

### Option B — App-issued JWTs (HMAC secret in env)

- Pros: Fewer containers.
- Cons: Disqualifying: own token issuance / password-equivalent secret; no external IdP.

### Option C — Session cookies + Keycloak authorization code for humans

- Pros: Better for browsers.
- Cons: Callers are services, not users; `client_credentials` is the specified grant.

### Option D — Introspection on every request

- Pros: Immediate revocation.
- Cons: Extra round-trip per request; Keycloak becomes a runtime SPOF on the hot path; JWKS + short-lived tokens are enough here.

## Decision

- **IdP:** Keycloak 26 (Compose). Realm `wagering` is imported from [`docker/keycloak/realm-wagering.json`](../../docker/keycloak/realm-wagering.json) via `start-dev --import-realm`. Local secrets are dummies (same class as LocalStack keys and the Compose admin password).
- **Grant:** OAuth 2.0 `client_credentials` between services. No authorization code, no resource-owner password, no app-issued tokens.
- **Clients (client id = identity):**
  - `wagering-api` — audience only (no flows). Access tokens must include this `aud`.
  - `wagering-internal` — internal wallet service (`OIDC_INTERNAL_CLIENT`).
  - `provider-a`, `provider-b` — game providers for isolation tests.
- **`providerId`:** derived from the JWT `azp` (client id). If `azp == OIDC_INTERNAL_CLIENT`, the actor is internal and has no `providerId`. Otherwise `providerId == azp`. Path and body `providerId` are never trusted unless they equal this value. Realm roles `internal` / `provider` are provisioned for documentation; authorization uses `azp`.
- **Validation library:** [`github.com/coreos/go-oidc/v3`](https://github.com/coreos/go-oidc). `oidc.NewVerifier` + `oidc.NewRemoteKeySet` (JWKS). Checks: signature, `iss` equals `OIDC_ISSUER`, `aud` contains `OIDC_AUDIENCE`, `exp`, algorithm RS256 (`alg=none` / HMAC rejected because the key set is RSA).
- **Issuer vs JWKS:** `OIDC_ISSUER` is the expected `iss` (must match the token). `OIDC_JWKS_URL` is optional; when empty it is `{issuer}/protocol/openid-connect/certs`. Compose sets issuer and JWKS to the in-network Keycloak URL; host `.env.example` uses `localhost:8081`.
- **HTTP:** `/health/*` remains public. All other registered routes require `Authorization: Bearer`. Missing/invalid/expired → **401** with no handler side effects. Authenticated but not allowed → **403**, generic body (no leak). JWKS/IdP fetch failure at verify time → **503** (transient). Startup fetches JWKS and fails fast if unreachable.
- **Route gates (Phase 4 registers the paths; financial handlers arrive in Phase 5):**
  - Wallet routes (`POST /wallets`, `GET /wallets/{walletId}`, ledger, reconciliation): internal client only.
  - `POST /wagering/transactions`: provider clients only; if the JSON body has `providerId`, it must equal `azp` (peek only; amounts are not parsed).
  - `GET /providers/{providerId}/...`: internal **or** `azp ==` path `providerId`.
  - `GET /wagering/transactions/{transactionId}`: any authenticated actor in Phase 4; ownership lookup is Phase 5.
- **Broker (AUTH-06):** SQS access uses AWS credentials (`AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`; LocalStack dummies in Compose). There is no anonymous queue access. The future consumer still applies domain validation and must not treat payload `providerId` as an authenticated HTTP identity.

## Consequences

- Positive: ELIM-01/02 and AUTH-01..05 have a concrete, testable model; ENT-04 is a realm file; multiple app instances share Keycloak; domain stays free of OIDC.
- Negative / accepted risks: tokens from `localhost:8081` are rejected by a process configured with issuer `http://keycloak:8080/...` (and vice versa) — documented. Revocation before `exp` is not checked (no introspection).
- Known limitations: Phase 4 handlers return `501` after a successful gate so TST-07 can run without financial use cases. Isolation of `GET /wagering/transactions/{transactionId}` and idempotent replay against persisted rows is Phase 5. SQS publisher identity is Phase 7.

## Verification

- Unit: JWKS test server; valid RS256 accepted; expired, wrong `iss`, wrong `aud`, `alg=none`/HMAC, missing bearer → unauthenticated; IdP down → unavailable.
- Unit: internal client forbidden on provider-only routes; provider-a forbidden on provider-b path and on wallet routes; matching provider allowed (stub 501); body `providerId` mismatch → 403; health stays unauthenticated 200.
- Integration (`-tags=integration`, real Keycloak): `client_credentials` tokens; missing/invalid credentials; provider isolation; internal restriction; denied requests do not change financial tables.
- Startup: missing `OIDC_*` fails config validation; JWKS unreachable fails `app.Start`.
