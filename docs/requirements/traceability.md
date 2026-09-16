# Traceability matrix

Every requirement from [`init.md`](../../init.md) with status and verifiable evidence. Update it with
the `spec-audit` skill or when completing a deliverable.

**Legend:** `—` not started · `🚧` partial · `✅` implemented with evidence · `⚠️` risk/divergence

## Disqualifying criteria (§14)

| ID | Criterion | Status | Evidence |
| --- | --- | --- | --- |
| ELIM-01 | Effective authentication on every business endpoint | ✅ | Middleware on all registered business routes (`internal/adapter/http/server.go`); missing/invalid Bearer → 401 (`TestBusinessRouteMissingToken`, `TestBusinessRouteInvalidToken`, `TestAuthRealIdP`). Health stays public. Handlers are Phase 4 stubs (`501`) |
| ELIM-02 | No unauthorized access to operations or transactions | ✅ | Path isolation `TestProviderIsolationOnPath` / `TestAuthRealIdP/provider_isolation`; wallets `TestWalletRestrictedToInternal`; body `providerId` `TestPostWageringRejectsInternalAndBodyMismatch`; generic 403 `TestForbiddenBodyDoesNotLeak`. `GET /wagering/transactions/{id}` ownership is Phase 5 |
| ELIM-03 | No floating-point money arithmetic | 🚧 | Domain: `internal/domain` (`TestDomainSourcesHaveNoFloatMoney`, `TestParseMoneyRejectsInvalid`, `TestMoneyJSON`). Persistence: `migrations/000002_financial_schema.up.sql` (`BIGINT` `*_minor`); `internal/adapter/postgres/repo.go` (`moneyFromMinorScan` → `int64`). HTTP/SQS money codecs not migrated |
| ELIM-04 | No negative balance under concurrency | 🚧 | Schema `CHECK (balance_minor >= 0)`; `TestConcurrentBetsSerializePerWallet` (100.00 + two 80.00 → balance 20.00). Not yet proven with ≥3 processes |
| ELIM-05 | No duplicated movement | — | |
| ELIM-06 | Persistent idempotency (not memory-only) | — | |
| ELIM-07 | Works correctly with multiple instances | — | |
| ELIM-08 | No publishing before commit | — | |
| ELIM-09 | Auditable ledger | 🚧 | `migrations/000002_financial_schema.up.sql` (`wallet_ledger_entries`, `UNIQUE (wallet_id, transaction_id)`, trigger `wallet_ledger_entries_append_only`, `REVOKE UPDATE, DELETE, TRUNCATE`); `TestLedgerAppendOnly`; `internal/adapter/postgres/ledger.go`. HTTP `GET .../ledger` later |
| ELIM-10 | Real PostgreSQL, SQS and IdP in tests (no full mocking) | 🚧 | TST-04 uses real PostgreSQL (`-tags=integration`, skip-if-no-`POSTGRES_DSN`). TST-07 uses real Keycloak (`OIDC_ISSUER`). SQS not yet in this suite |

## Stack and composition (§4)

| ID | Requirement | Status | Evidence | Notes |
| --- | --- | --- | --- | --- |
| STK-01 | Go version declared in `go.mod` and Dockerfile | ✅ | `go.mod` (`go 1.25.11`); `Dockerfile` (`golang:1.25-bookworm`); [ADR 0002](../adr/0002-go-version-and-http-router.md) | |
| STK-02 | `go.mod` and `go.sum` versioned | ✅ | `go.mod`, `go.sum` (`github.com/leosanner/desafio-jungle-go`) | |
| STK-03 | Docker Compose with PostgreSQL, IdP and LocalStack/MiniStack | ✅ | `docker-compose.yml` (postgres 16, Keycloak 26 + realm import, LocalStack 4.6); [test-dependencies.md](../runbooks/test-dependencies.md) | Realm `wagering` imported from `docker/keycloak/realm-wagering.json` |
| STK-04 | Versioned migrations with documented apply and rollback | ✅ | `migrations/000001_bootstrap.up.sql` / `.down.sql`; `migrations/000002_financial_schema.up.sql` / `.down.sql`; [README](../../README.md) Migrations; [ADR 0003](../adr/0003-database-access-and-migrations.md) | App Up on start from `MIGRATIONS_PATH`; CLI `down 1`; schema `wagering` plus financial tables |
| STK-05 | DB library, `Money` mapping and cross-repository transaction documented | ✅ | [ADR 0003](../adr/0003-database-access-and-migrations.md) (`pgx/v5`); [ADR 0006](../adr/0006-money-representation.md) (`int64` cents → `BIGINT`, Proposed); [ADR 0009](../adr/0009-sql-unit-of-work.md) (Proposed); `internal/app/ports.go` (`UnitOfWork`); `internal/adapter/postgres/uow.go`; `migrations/000002_financial_schema.up.sql`; `TestMapError`; `TestUnitOfWorkAtomicity` | ADRs 0006/0009 still Proposed |
| FX-01 | Composition via `fx.Module`, `fx.Provide`, `fx.Invoke` with constructors | ✅ | `internal/composition/`; `cmd/wagering/main.go`; `TestValidateApp`; [ADR 0004](../adr/0004-fx-lifecycle-and-shutdown.md) | |
| FX-02 | Config and dependency validation on start | ✅ | `internal/config/`; `TestInvalidConfigFailsStart`; [ADR 0004](../adr/0004-fx-lifecycle-and-shutdown.md) | |
| FX-03 | Workers with cancellation, deadlines and observable termination | — | [ADR 0004](../adr/0004-fx-lifecycle-and-shutdown.md) (pattern only) | No SQS/outbox/pending-reference workers in Phase 1 |
| FX-04 | Shutdown: stops input, finishes/releases in-flight work | 🚧 | `internal/adapter/http/server.go` (`Shutdown`); `TestReadyUnavailableWhenShuttingDown`; [health.md](../api/health.md) | HTTP drain done; in-flight workers N/A until later |
| FX-05 | Dependencies close after the components using them | 🚧 | `internal/composition/postgres.go` (pool `OnStop`); [ADR 0004](../adr/0004-fx-lifecycle-and-shutdown.md) | HTTP registered after postgres so reverse `OnStop` closes the pool last; workers N/A |
| FX-06 | Domain independent of Fx, HTTP, SQS and persistence | ✅ | `internal/domain/deps_test.go`; [ADR 0001](../adr/0001-package-layout-and-layer-boundaries.md) | |

## Authentication and authorization (§2)

| ID | Requirement | Status | Evidence | Notes |
| --- | --- | --- | --- | --- |
| AUTH-01 | External OAuth 2.0/OIDC IdP (Keycloak) with automatic provisioning | ✅ | Compose Keycloak `start-dev --import-realm`; [`docker/keycloak/realm-wagering.json`](../../docker/keycloak/realm-wagering.json); [ADR 0011](../adr/0011-oidc-keycloak-auth.md) | |
| AUTH-02 | `client_credentials` between services | ✅ | Realm confidential clients; [auth.md](../api/auth.md); `TestOIDCVerifierRealIdP` | |
| AUTH-03 | Authorized `providerId` derived from the identity | ✅ | JWT `azp` → `app.Actor.ProviderID` (`internal/adapter/auth/verifier.go`); [ADR 0011](../adr/0011-oidc-keycloak-auth.md); `TestVerifyAcceptsProviderToken` | Body/path never trusted alone |
| AUTH-04 | Provider accesses only its own transactions, including replays | 🚧 | Path isolation `TestProviderIsolationOnPath`, `TestAuthRealIdP`; POST body `providerId` must match `azp`. Persisted replay isolation is Phase 5 | |
| AUTH-05 | Wallet operations restricted to the internal service | ✅ | `requireInternal` on wallet routes; `OIDC_INTERNAL_CLIENT`; `TestWalletRestrictedToInternal`; `TestAuthRealIdP/provider_cannot_open_wallet` | |
| AUTH-06 | Broker access controlled by credentials/policies | 🚧 | LocalStack/AWS static keys required (`AWS_ACCESS_KEY_ID` / `SECRET`); [ADR 0011](../adr/0011-oidc-keycloak-auth.md). No consumer yet | |
| AUTH-07 | IdP, validation and permission rationale in `ARCHITECTURE.md` | ✅ | [ARCHITECTURE.md](../../ARCHITECTURE.md) Authentication; [ADR 0011](../adr/0011-oidc-keycloak-auth.md); [auth.md](../api/auth.md) | ADR Proposed |

## Guarantees (§5)

| ID | Requirement | Status | Evidence | Notes |
| --- | --- | --- | --- | --- |
| GAR-01 | Money without floats in parsing, arithmetic, serialization and persistence | ✅ | `internal/domain/money.go`; `money_test.go`; [ADR 0006](../adr/0006-money-representation.md); `migrations/000002_financial_schema.up.sql` (`BIGINT`); `internal/adapter/postgres/repo.go` (`moneyFromMinorScan`) | HTTP adapter has no money JSON yet (no float path) | |
| GAR-02 | Persistent idempotency surviving restarts | — | | |
| GAR-03 | Invariants in the database, independent of local locks and FIFO dedup | ✅ | `migrations/000002_financial_schema.up.sql`; `TestFinancialSchemaConstraints` (raw SQL, no app locks); `TestLedgerAppendOnly` | |
| GAR-04 | Publishing only after commit | — | | |
| GAR-05 | Append-only ledger | ✅ | `000002` trigger `wallet_ledger_entries_append_only`; `REVOKE UPDATE, DELETE, TRUNCATE`; `TestLedgerAppendOnly`; `internal/adapter/postgres/ledger.go` (insert/list only) | HTTP ledger read later |
| GAR-06 | Independent wallets in parallel; no global lock | 🚧 | [ADR 0010](../adr/0010-per-wallet-concurrency.md) (row lock per wallet; no global mutex); `GetByIDForUpdate` | Parallel independent wallets not proven | |
| GAR-07 | No lost updates | 🚧 | [ADR 0010](../adr/0010-per-wallet-concurrency.md); `GetByIDForUpdate`; version-conditioned `UPDATE`; `TestConcurrentBetsSerializePerWallet` (one process, two txs) | ≥3 processes still Phase 9 |
| GAR-08 | Uniqueness, non-negativity and immutability enforced by the schema | ✅ | `000002`; `TestFinancialSchemaConstraints`; `TestLedgerAppendOnly`; `TestDuplicateWalletInsertConflict` | |

## Domain model (§6)

| ID | Requirement | Status | Evidence | Notes |
| --- | --- | --- | --- | --- |
| DOM-01 | Encapsulated state, validating constructors, explicit transitions | ✅ | `internal/domain` (`New*`, `MarkProcessed`/`Rejected`/…, unexported fields) | | |
| DOM-02 | Separate creation and rehydration | ✅ | `RehydrateWallet`, `RehydrateTransaction`, `RehydrateLedgerEntry`; `TestRehydrate*` | | |
| DOM-03 | Uninitialized/invalid values rejected | ✅ | `TestMoneyZeroValueRejected`; validating constructors | | |
| DOM-04 | Classifiable errors (`errors.Is`/`As`), no `panic` for business rules | ✅ | `ClassifiedError`, sentinels; tests use `errors.Is` | | |
| DOM-05 | I/O takes `context.Context` | ✅ | `internal/app/ports.go` (`UnitOfWork.Within`, all repository methods); `internal/adapter/postgres` implementations take `ctx`; `internal/app/deps_test.go` | Domain still has no I/O | |
| MON-01 | Immutable `Money`: parse from string, zero, add, subtract, negate, compare, serialize | ✅ | `internal/domain/money.go`; `TestParseMoneyAcceptsCanonical`; `TestMoneyArithmetic`; `TestMoneyJSON` | | |
| MON-02 | Contract `{"amount":"25.00","currency":"BRL"}`, scale 2, ISO 4217 | ✅ | `TestMoneyJSON`; `ParseCurrency` | | |
| MON-03 | Rejects empty, NaN, Infinity, scientific notation, excess scale, negative external input | ✅ | `TestParseMoneyRejectsInvalid` | | |
| MON-04 | No silent rounding; normalization before hashing documented | ✅ | [ADR 0006](../adr/0006-money-representation.md); non-canonical forms rejected | Hash algorithm later | |
| MON-05 | Incompatible currencies rejected (with tests) | ✅ | `TestMoneyIncompatibleCurrency`; `TestWalletCurrencyMismatch` | | |
| MON-06 | Overflow handled (if `int64`) | ✅ | `TestMoneyOverflow`; `TestParseMoneyRejectsInvalid` (`overflow`) | | |
| MON-07 | Exact persistence of amount and currency; representation and limits documented | ✅ | [ADR 0006](../adr/0006-money-representation.md); `migrations/000002_financial_schema.up.sql` (`*_minor BIGINT`, `CHAR(3)` currency); `internal/adapter/postgres` (`Minor()` / `MoneyFromMinor`) | | |
| WAL-01 | Identity, player, currency, balance, version, timestamps | ✅ | `internal/domain/wallet.go`; `TestOpenWalletPositiveCreatesOpening` | | |
| WAL-02 | One wallet per `(playerId, currency)` | ✅ | `UNIQUE (player_id, currency)`; `TestDuplicateWalletInsertConflict`; `TestFinancialSchemaConstraints` (`duplicate player_id currency`) | |
| WAL-03 | Debit keeps balance ≥ 0; currency matches | ✅ | `TestWalletDebitInsufficientFunds`; `TestWalletCurrencyMismatch` | | |
| WAL-04 | Financial change with ledger entry in the same commit | ✅ | Domain `Debit`/`Credit`/`Apply`; `UnitOfWork.Within`; `TestUnitOfWorkAtomicity`; [ADR 0009](../adr/0009-sql-unit-of-work.md) | |
| WAL-05 | Initial version 1, increments only on balance change | ✅ | `TestOpenWallet*`; `TestWalletDebitCreditAndVersion`; `TestApplyBetWinLoss` (LOSS) | | |
| WAL-06 | Concurrency strategy documented | ✅ | [ADR 0010](../adr/0010-per-wallet-concurrency.md) (Proposed); `GetByIDForUpdate` + version-conditioned `UPDATE` in `internal/adapter/postgres/wallet.go` | Not proven with N processes (CON-01/02) | |
| WTX-01 | External transaction fields persisted (ids, provider, key, hash, round, game, reference, result) | ✅ | `internal/domain/transaction.go`; `migrations/000002_financial_schema.up.sql`; `internal/adapter/postgres/transaction.go` (insert/scan of ids, provider, key, hash, round, game, reference, result) | | |
| WTX-02 | Validated state machine; immutable terminal states; documented | ✅ | [ADR 0007](../adr/0007-wager-transaction-state-machine.md); `TestStateMachineTransitions`; `TestRejectedIsTerminal` | | |
| WTX-03 | Replay returns persisted result without re-applying | 🚧 | Rehydration does not re-apply; `ResultBalance` snapshot on PROCESSED | HTTP/SQS replay use case later | |
| WTX-04 | `PENDING` durably resumable by another instance | — | | |
| WTX-05 | `OPENING` rejected via HTTP/SQS; schema distinguishes internal/external; no duplicate opening credit | 🚧 | Domain: `NewExternalTransaction` / `Apply` reject `OPENING`; `TestNewExternalTransactionRejectsOpening`. Schema: `000002` origin CHECKs, internal/external shape, `wager_transactions_opening_per_wallet_uidx` | HTTP/SQS still later | |
| WTX-06 | Transient vs permanent failure distinction documented | ✅ | [ADR 0007](../adr/0007-wager-transaction-state-machine.md); `FailureClass` | | |
| LED-01 | Entry fields and `balanceAfter = balanceBefore ± money` validation | ✅ | `NewLedgerEntry`; `TestLedgerValidatesInvariant` | | |
| LED-02 | `(walletId, transactionId)` uniqueness and update/delete protection in the database | ✅ | `000002` `UNIQUE (wallet_id, transaction_id)`; trigger; `TestFinancialSchemaConstraints` (duplicate ledger); `TestLedgerAppendOnly` | |
| LED-03 | `LOSS` and rejections produce no entry | ✅ | `TestApplyBetWinLoss`; `TestApplyBetInsufficientFunds` | | |
| INB-01 | Inbox with `(consumerName, messageId)` uniqueness, hash, received and completed timestamps | — | | |
| INB-02 | Inbox in the same transaction as domain, ledger and events | — | | |
| OUT-01 | Outbox with stable identity, attempts, next attempt, published timestamp and backoff | — | | |

## Operations and references (§7)

| ID | Requirement | Status | Evidence | Notes |
| --- | --- | --- | --- | --- |
| OPS-01 | `BET`: debit, positive amount, sufficient funds | ✅ | `TestApplyBetWinLoss`; `TestApplyBetInsufficientFunds` | | |
| OPS-02 | `WIN`: credit, positive amount, optional reference to a bet in the same round | ✅ | `TestApplyBetWinLoss`; `TestApplyWinWithBetReference` | | |
| OPS-03 | `LOSS`: `"0.00"`, no ledger, no version bump, emits `WagerTransactionProcessed` | ✅ | `TestApplyBetWinLoss`; `TestNewExternalTransactionAmountPolicy` | | |
| OPS-04 | `REFUND`: fully returns a processed `BET` | ✅ | `TestApplyRefundOfBet` | | |
| OPS-05 | `ROLLBACK`: fully reverses a processed `BET`/`WIN`/`REFUND` | ✅ | `TestApplyRollbackOfRefund`; `TestApplyRollbackOfWinWithoutFunds` | | |
| OPS-06 | Mandatory reference resolved by `(providerId, referenceExternalTransactionId)` | 🚧 | `requireReference` / `matchReference` in `apply.go` | Lookup is the use case's job | |
| OPS-07 | Reference matches provider, player, wallet, currency, round and amount | ✅ | `TestApplyReferenceMismatch` | | |
| OPS-08 | No two successful reversals of the same kind; REFUND/ROLLBACK combinations documented | ✅ | [ADR 0008](../adr/0008-refund-rollback-combinations.md); `TestApplyDuplicateRefundRejected`; `TestApplyRefundAndRollbackOfSameBetMutuallyExclusive` | | |
| OPS-09 | Reversal without funds rejected and auditable with its own `failureCode` | ✅ | `TestApplyRollbackOfWinWithoutFunds` (`INSUFFICIENT_FUNDS_REVERSAL`) | | |
| OPS-10 | `PENDING_REFERENCE` with worker and exponential backoff, survives restart | — | | |
| OPS-11 | Max attempts/TTL → `REJECTED` with reference-not-found code + event | — | | |
| OPS-12 | Behavior with a pending or unsuccessful reference documented | ✅ | [ADR 0007](../adr/0007-wager-transaction-state-machine.md); `TestApplyPendingAndUnsuccessfulReference` | | |
| OPS-13 | Stable, documented `failureCode`, correctable × definitive | ✅ | [`docs/api/failure-codes.md`](../api/failure-codes.md); `TestFailureCodeCorrectableVsDefinitive` | HTTP mapping later | |

## Concurrency (§8)

| ID | Requirement | Status | Evidence | Notes |
| --- | --- | --- | --- | --- |
| CON-01 | Per-wallet coordination with a justified strategy | 🚧 | [ADR 0010](../adr/0010-per-wallet-concurrency.md); `GetByIDForUpdate`; `TestConcurrentBetsSerializePerWallet` | Not proven with 3 processes (CON-02) |
| CON-02 | Demonstrated with ≥3 independent processes | — | | |
| CON-03 | 100.00 + two 80.00 bets → 1 processed, 1 rejected, balance 20.00, 1 debit | 🚧 | `TestConcurrentBetsSerializePerWallet` (two goroutines, one process, repository/UoW) | HTTP/SQS and ≥3 instances later |
| CON-04 | Resends don't change the outcome | — | | |
| CON-05 | Different wallets processed in parallel | — | | |

## HTTP (§9)

| ID | Requirement | Status | Evidence | Notes |
| --- | --- | --- | --- | --- |
| HTTP-01 | `POST /wallets` with `OPENING`, ledger and outbox in the same commit; zero without `OPENING`; duplicate → conflict | — | | |
| HTTP-02 | `GET /wallets/:walletId` | — | | |
| HTTP-03 | `GET /wallets/:walletId/ledger` with opaque cursor and stable ordering | — | | |
| HTTP-04 | `GET /wagering/transactions/:transactionId` | — | | |
| HTTP-05 | `GET /providers/:providerId/wagering/transactions/:externalTransactionId` | — | | |
| HTTP-06 | `POST /wagering/transactions` with mandatory `Idempotency-Key` | — | | |
| HTTP-07 | Canonical hash (sorted-key JSON), equivalent across HTTP/SQS, documented | — | | |
| HTTP-08 | Replay with `idempotentReplay: true` and original balance; conflict on different payload | 🚧 | `CheckIdempotencyReplay`; `TestIdempotencyPayloadConflict`; `ResultBalance` | HTTP adapter later | |
| HTTP-09 | Same `(providerId, externalTransactionId)` cannot be re-applied under another key | 🚧 | `CheckExternalIdentity`; `000002` `wager_transactions_provider_external_id_uidx` | HTTP adapter later | |
| HTTP-10 | Documented statuses and bodies: invalid, conflict, rejection, pending, unavailable | 🚧 | Auth statuses in [auth.md](../api/auth.md) (401/403/400/501/503). Financial status catalog still Phase 5 | |
| HTTP-11 | `POST /wallets/:walletId/reconciliation` on a consistent snapshot, without changing the balance | — | | |
| HTTP-12 | Divergence reported in response, logs and a metric | — | | |
| HTTP-13 | `GET /health/live` and `GET /health/ready` (PostgreSQL and SQS) | ✅ | `internal/adapter/http/health.go`; `health_test.go`; [docs/api/health.md](../api/health.md) | Public; ready probes postgres + both SQS queues; 503 on shutdown |

## SQS (§10)

| ID | Requirement | Status | Evidence | Notes |
| --- | --- | --- | --- | --- |
| SQS-01 | Queues `wager-transactions.fifo` and `wager-transactions-dlq.fifo` with redrive | 🚧 | `docker/localstack/init-sqs.sh`; `docker-compose.yml`; [docs/events/README.md](../events/README.md) | Queues + redrive provisioned; no consumer in Phase 1 |
| SQS-02 | Use case and idempotency shared with HTTP | — | | |
| SQS-03 | Envelope `messageId` as identity; hash checked on redelivery | — | | |
| SQS-04 | Deleted only after commit; committed rejection deletes | — | | |
| SQS-05 | Transient → retry with backoff; permanent/exhausted → DLQ | — | | |
| SQS-06 | Attempt limits, visibility timeout and invalid message handling documented | — | | |
| SQS-07 | SIGTERM: stop fetching, finish or release visibility | — | | |
| SQS-08 | `MessageGroupId`/`MessageDeduplicationId` documented; HTTP×SQS concurrency validated | — | | |

## Outbox (§11)

| ID | Requirement | Status | Evidence | Notes |
| --- | --- | --- | --- | --- |
| OBX-01 | State, balance, ledger, inbox and events committed atomically | — | | |
| OBX-02 | Separate worker, multiple publishers, contention, backoff, abandoned work recovery | — | | |
| OBX-03 | Recovery between commit→publish and publish→ack; `eventId` preserved | — | | |
| OBX-04 | Destination provisioned; routing and consumption contracts documented | — | | |
| OBX-05 | Events `WagerTransactionProcessed`, `Rejected`, `WalletBalanceChanged`, `PendingReference` with concrete types | 🚧 | `internal/domain/event.go`; [`docs/events/outbox-events.md`](../events/outbox-events.md) | Outbox persistence later | |
| OBX-06 | Envelope `eventId`, `eventType`, `aggregateId`, `correlationId`, `causationId?`, `occurredAt`, `version`, `data` | — | | |
| OBX-07 | Complete `WalletBalanceChanged` payload | 🚧 | `WalletBalanceChanged` fields; constructors in opening/`Apply` | Outbox snapshot later | |
| OBX-08 | Type/version set by constructor; UTC RFC 3339; money as strings; immutable snapshot | 🚧 | `NewWagerTransactionProcessed` etc.; `TestEventConstructorsSetTypeAndVersion`; Money JSON strings | Envelope/outbox later | |

## Observability (§12)

| ID | Requirement | Status | Evidence | Notes |
| --- | --- | --- | --- | --- |
| OBS-01 | JSON logs with correlation IDs, no credentials or full payloads | 🚧 | [ADR 0002](../adr/0002-go-version-and-http-router.md) (`log/slog` JSON, `LOG_LEVEL`) | Correlation IDs on business logs and redaction on financial paths not implemented |
| OBS-02 | Metrics: status, duplicates, retries, DLQ, conflicts, outbox lag, latency, reconciliation | — | | |
| OBS-03 | (Optional) OpenTelemetry tracing / dashboards | — | | |

## Verification (§13)

| ID | Requirement | Status | Evidence | Notes |
| --- | --- | --- | --- | --- |
| TST-01 | Unit: Money (parsing, scale, limits, invalid input, currencies) | ✅ | `internal/domain/money_test.go` | | |
| TST-02 | Unit: wallet invariants and state transitions | ✅ | `wallet_test.go`; `transaction_test.go` | | |
| TST-03 | Unit: rules for the 5 kinds, zero policy, internal opening, payload conflict | ✅ | `apply_test.go`; `TestOpenWallet*`; `TestIdempotencyPayloadConflict` | | |
| TST-04 | Integration: migrations, constraints, immutability, atomicity | ✅ | `TestMigrationsUpAndDown`; `TestFinancialSchemaConstraints`; `TestLedgerAppendOnly`; `TestUnitOfWorkAtomicity`; `go test -tags=integration ./internal/adapter/postgres/...` with `POSTGRES_DSN`. [integration.md](../runbooks/integration.md) | Skip-if-no-env; Keycloak/SQS not required |
| TST-05 | Integration: inbox, redelivery, concurrent outbox, retry, DLQ, recovery | — | | |
| TST-06 | Integration: Fx composition, start/stop and resource release | 🚧 | `TestValidateApp`; `TestInvalidConfigFailsStart`; `start_stop_integration_test.go` (`-tags=integration`) | Graph validated in unit tests; full start/stop against real infra still optional/skip |
| TST-07 | Auth: real IdP; missing/invalid/expired; isolation; internal restriction; no effects | ✅ | Unit: `internal/adapter/auth/verifier_test.go`, `internal/adapter/http/auth_test.go` (expired, HMAC, isolation, no handler on 401). Integration: `TestOIDCVerifierRealIdP`, `TestAuthRealIdP` (`-tags=integration`, `OIDC_ISSUER`). Expired covered in unit tests (real Keycloak tokens are not expired) | |
| TST-08 | Same bet 50× in parallel → one debit | — | | |
| TST-09 | Two 80.00 bets on 100.00 | 🚧 | `TestConcurrentBetsSerializePerWallet` | Same-process UoW; HTTP and multi-instance later |
| TST-10 | Distinct wallets in parallel | — | | |
| TST-11 | Scenarios with ≥3 instances | — | | |
| TST-12 | Consumer interrupted after commit and before delete | — | | |
| TST-13 | Two publishers contending for the outbox | — | | |
| TST-14 | Reversal before its reference → resolution or expiry | — | | |
| TST-15 | Restart preserves idempotency, pending work and consistency; `PENDING` resumed | — | | |
| TST-16 | Balance vs ledger at the end; scenarios crossing HTTP and SQS | — | | |
| TST-17 | Duplicate tests exercise application-level deduplication | — | | |
| TST-18 | `go test -race` on applicable tests | ✅ | `go test -race ./...` including `internal/domain` | |

## Delivery (§15)

| ID | Requirement | Status | Evidence | Notes |
| --- | --- | --- | --- | --- |
| ENT-01 | Reproducible from a clean checkout | 🚧 | [README](../../README.md) | Phase 1 instructions written; Compose/image/binary owned by other agents |
| ENT-02 | README: prerequisites, env, queues, migrations, running, examples, tests | 🚧 | [README.md](../../README.md) | Phase 4: OIDC env, clients, token example; financial HTTP use cases still Phase 5 |
| ENT-03 | `.env.example` without real secrets | ✅ | `.env.example` | Local dummy keys only |
| ENT-04 | Automatic IdP provisioning and test identities | ✅ | `docker/keycloak/realm-wagering.json`; Compose `--import-realm`; [auth.md](../api/auth.md); [README](../../README.md) Authentication | Local dummy client secrets |
| ENT-05 | ARCHITECTURE.md with decisions, limitations, interpretations and unfinished work | 🚧 | [ARCHITECTURE.md](../../ARCHITECTURE.md); [docs/adr/](../adr/) | Phase 4 auth summarized; ADRs 0006–0011 Proposed; hash still TBD |
| ENT-06 | `docker compose up --build`, `go test ./...`, `go test -race ./...`, `go vet ./...` | ✅ | [README](../../README.md); Compose stack healthy; unit/`vet`/`-race` pass | |
| ENT-07 | Separate docs: test dependencies, integration, multiple instances, failures, build tags | 🚧 | [test-dependencies.md](../runbooks/test-dependencies.md); [integration.md](../runbooks/integration.md); [ADR 0005](../adr/0005-test-strategy-initial.md) | TST-04 and TST-07 runbooks; multi-instance / failure runbooks still placeholders |
| ENT-08 | `gofmt`-formatted code and reproducible dependencies | 🚧 | [ADR 0005](../adr/0005-test-strategy-initial.md); `gofmt -l .` in README | Domain added in Phase 2 |
