# Failure codes

Stable `failureCode` values produced by the domain (`init.md` §7, OPS-13). HTTP mapping:
[ADR 0013](../adr/0013-http-status-mapping.md), [status.md](status.md). Codes are classifiable in
Go via `domain.ClassifiedError` (`errors.As`).

Correctable codes mean the client can fix the request and send a **new** operation. Definitive codes
are recorded outcomes (`REJECTED`) that replay must return unchanged.

| Code | Class | Correctable | When |
| --- | --- | --- | --- |
| `INVALID_AMOUNT` | Validation | yes | Empty, NaN, Infinity, scientific notation, wrong scale, negative external amount |
| `INVALID_CURRENCY` | Validation | yes | Currency not 3 uppercase letters |
| `INCOMPATIBLE_CURRENCY` | Validation / Rejection | yes | Arithmetic or movement vs wallet currency |
| `UNINITIALIZED` | Validation | yes | Zero-value `Money` |
| `OVERFLOW` | Validation | yes | `int64` minor-unit overflow |
| `JSON_AMOUNT_NOT_STRING` | Validation | yes | JSON `amount` is a number (float path forbidden) |
| `POSITIVE_AMOUNT_REQUIRED` | Validation | yes | BET/WIN/REFUND/ROLLBACK/OPENING with non-positive amount |
| `ZERO_AMOUNT_REQUIRED` | Validation | yes | LOSS with amount other than `"0.00"` |
| `OPENING_NOT_ALLOWED` | Validation | yes | `OPENING` via HTTP/SQS / `Apply` |
| `MISSING_REFERENCE` | Validation | yes | REFUND/ROLLBACK without `referenceExternalTransactionId` |
| `MISSING_IDENTITY` | Validation | yes | Required id/key empty |
| `INVALID_KIND` / `INVALID_STATUS` / `INVALID_DIRECTION` / `INVALID_ORIGIN` | Validation | yes | Unknown enumeration |
| `INSUFFICIENT_FUNDS` | Rejection | no | BET debit would make balance negative |
| `INSUFFICIENT_FUNDS_REVERSAL` | Rejection | no | ROLLBACK debit would make balance negative (distinct from a bet) |
| `DUPLICATE_REVERSAL` | Rejection | no | Second successful reversal of the same kind, or REFUND+ROLLBACK of the same BET |
| `REFERENCE_MISMATCH` | Rejection | no | Provider/player/wallet/round/currency/amount disagree |
| `REFERENCE_KIND_INVALID` | Rejection | no | e.g. REFUND of a WIN |
| `REFERENCE_UNSUCCESSFUL` | Rejection | no | Reference is `REJECTED` or `FAILED` |
| `IDEMPOTENCY_PAYLOAD_CONFLICT` | Rejection | no | Same idempotency key, different payload hash |
| `DUPLICATE_EXTERNAL_TRANSACTION` | Rejection | no | Same `(providerId, externalTransactionId)` under another key |
| `INVALID_TRANSITION` | Rejection | no | Transition from a terminal status |
| `LEDGER_INVARIANT` | Validation | yes | `balanceAfter != balanceBefore ± money` (corrupt input) |

HTTP: domain `Validation` → `400`; `IDEMPOTENCY_PAYLOAD_CONFLICT` and `DUPLICATE_EXTERNAL_TRANSACTION`
→ `409`; other persisted `Rejection` → `422` with the operation result. `Wait` (`PENDING_REFERENCE`)
→ `202`.

`PENDING_REFERENCE` is not a failure code: missing or still-pending references wait (class `Wait`).
`REFERENCE_NOT_FOUND` after TTL is Phase 8.

See [ADR 0007](../adr/0007-wager-transaction-state-machine.md) and
[ADR 0008](../adr/0008-refund-rollback-combinations.md).
