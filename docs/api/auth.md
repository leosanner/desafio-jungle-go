# Authentication

OIDC resource-server behaviour (`init.md` §2 and §9). Decision: [ADR 0011](../adr/0011-oidc-keycloak-auth.md).

`Content-Type: application/json` on the error bodies below. Tokens, client secrets and full JWTs are never logged.

## Public routes

[`GET /health/live`](health.md) and [`GET /health/ready`](health.md) stay unauthenticated.

Every other registered route requires `Authorization: Bearer <access_token>`.

## Obtaining a token (local)

Realm `wagering` is imported by Compose from `docker/keycloak/realm-wagering.json`. Dummy secrets (local only):

| Client | Secret | Identity |
| --- | --- | --- |
| `wagering-internal` | `internal-secret` | Internal wallet service (`OIDC_INTERNAL_CLIENT`) |
| `provider-a` | `provider-a-secret` | `providerId` = `provider-a` |
| `provider-b` | `provider-b-secret` | `providerId` = `provider-b` |

Audience of access tokens: `wagering-api` (`OIDC_AUDIENCE`). Grant: `client_credentials`.

```sh
TOKEN=$(curl -sS -X POST http://localhost:8081/realms/wagering/protocol/openid-connect/token \
  -d grant_type=client_credentials \
  -d client_id=provider-a \
  -d client_secret=provider-a-secret | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')
```

Use host `http://keycloak:8080` when calling Keycloak from another Compose service. A token’s `iss` must match the process `OIDC_ISSUER` (host vs in-network URLs are not interchangeable).

## `providerId`

Taken from the JWT `azp` (OAuth client id). If `azp` equals `OIDC_INTERNAL_CLIENT`, the caller is internal and has no `providerId`. Path and body `providerId` are accepted only when they equal this value.

## Authorization

| Route | Who |
| --- | --- |
| `POST /wallets`, `GET /wallets/{walletId}`, `GET /wallets/{walletId}/ledger`, `POST /wallets/{walletId}/reconciliation` | Internal client only |
| `POST /wagering/transactions` | Provider clients only; JSON `providerId` must match `azp` when present |
| `GET /providers/{providerId}/wagering/transactions/{externalTransactionId}` | Internal, or the provider whose `azp` equals the path `{providerId}` |
| `GET /wagering/transactions/{transactionId}` | Any authenticated actor (ownership check in Phase 5) |

Financial handlers are not implemented in Phase 4: after a successful gate the stub returns `501`.

## Status catalog (auth)

| Status | `error` | When |
| --- | --- | --- |
| `401` | `unauthenticated` | Missing header, malformed `Bearer`, invalid signature, wrong `iss`/`aud`, expired, missing `azp` |
| `403` | `forbidden` | Authenticated but the client is not allowed (other provider, provider on a wallet route, internal on `POST /wagering/transactions`, body `providerId` mismatch) |
| `400` | `invalid` | `POST /wagering/transactions` with a body that is not JSON |
| `501` | `not_implemented` | Gate passed; use case not implemented yet |
| `503` | `unavailable` | JWKS / IdP fetch failed while verifying |

401 example:

```json
{"error":"unauthenticated","message":"missing or invalid bearer token"}
```

403 example (generic; does not echo the other provider’s id or payload):

```json
{"error":"forbidden","message":"insufficient permissions"}
```

`WWW-Authenticate: Bearer realm="wagering"` is set on 401 (and `error="invalid_token"` when a Bearer was present). Denied requests do not run the handler and do not write financial rows.

## SQS

Broker access uses AWS credentials (`AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`). There is no consumer in this phase. Payload `providerId` on SQS is not an HTTP identity; the future consumer still applies domain rules.
