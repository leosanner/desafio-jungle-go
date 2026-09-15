-- Phase 3 financial schema: wallets, wager transactions, append-only ledger.
-- Money is BIGINT minor units and CHAR(3) ISO 4217. No REAL/DOUBLE PRECISION/MONEY.

CREATE TABLE wagering.wallets (
    id             TEXT        PRIMARY KEY,
    player_id      TEXT        NOT NULL,
    currency       CHAR(3)     NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    balance_minor  BIGINT      NOT NULL CHECK (balance_minor >= 0),
    version        BIGINT      NOT NULL CHECK (version >= 1),
    created_at     TIMESTAMPTZ NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL,
    CONSTRAINT wallets_player_id_currency_key UNIQUE (player_id, currency)
);

CREATE TABLE wagering.wager_transactions (
    id                       TEXT        PRIMARY KEY,
    origin                   TEXT        NOT NULL CHECK (origin IN ('INTERNAL', 'EXTERNAL')),
    provider_id              TEXT,
    external_transaction_id  TEXT,
    idempotency_key          TEXT,
    payload_hash             TEXT,
    wallet_id                TEXT        NOT NULL REFERENCES wagering.wallets (id),
    player_id                TEXT        NOT NULL,
    round_id                 TEXT,
    game_id                  TEXT,
    kind                     TEXT        NOT NULL CHECK (kind IN ('OPENING', 'BET', 'WIN', 'LOSS', 'REFUND', 'ROLLBACK')),
    amount_minor             BIGINT      NOT NULL,
    currency                 CHAR(3)     NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    reference_external_id    TEXT,
    resolved_reference_id    TEXT        REFERENCES wagering.wager_transactions (id),
    status                   TEXT        NOT NULL CHECK (status IN ('PENDING', 'PENDING_REFERENCE', 'PROCESSED', 'REJECTED', 'FAILED')),
    failure_code             TEXT,
    result_balance_minor     BIGINT,
    result_currency          CHAR(3)     CHECK (result_currency IS NULL OR result_currency ~ '^[A-Z]{3}$'),
    attempt_count            INTEGER     NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at          TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL,
    updated_at               TIMESTAMPTZ NOT NULL,
    CONSTRAINT wager_transactions_result_balance_nullability CHECK (
        (result_balance_minor IS NULL) = (result_currency IS NULL)
    ),
    CONSTRAINT wager_transactions_internal_shape CHECK (
        origin <> 'INTERNAL'
        OR (
            kind = 'OPENING'
            AND provider_id IS NULL
            AND external_transaction_id IS NULL
            AND idempotency_key IS NULL
            AND payload_hash IS NULL
            AND round_id IS NULL
            AND game_id IS NULL
            AND reference_external_id IS NULL
        )
    ),
    CONSTRAINT wager_transactions_external_shape CHECK (
        origin <> 'EXTERNAL'
        OR (
            kind IN ('BET', 'WIN', 'LOSS', 'REFUND', 'ROLLBACK')
            AND provider_id IS NOT NULL
            AND external_transaction_id IS NOT NULL
            AND idempotency_key IS NOT NULL
            AND payload_hash IS NOT NULL
            AND round_id IS NOT NULL
            AND game_id IS NOT NULL
        )
    ),
    CONSTRAINT wager_transactions_reference_required CHECK (
        origin <> 'EXTERNAL'
        OR kind NOT IN ('REFUND', 'ROLLBACK')
        OR reference_external_id IS NOT NULL
    ),
    CONSTRAINT wager_transactions_amount_by_kind CHECK (
        (kind = 'LOSS' AND amount_minor = 0)
        OR (kind IN ('OPENING', 'BET', 'WIN', 'REFUND', 'ROLLBACK') AND amount_minor > 0)
    )
);

CREATE UNIQUE INDEX wager_transactions_provider_external_id_uidx
    ON wagering.wager_transactions (provider_id, external_transaction_id)
    WHERE origin = 'EXTERNAL';

CREATE UNIQUE INDEX wager_transactions_provider_idempotency_key_uidx
    ON wagering.wager_transactions (provider_id, idempotency_key)
    WHERE origin = 'EXTERNAL';

CREATE UNIQUE INDEX wager_transactions_opening_per_wallet_uidx
    ON wagering.wager_transactions (wallet_id)
    WHERE kind = 'OPENING';

CREATE UNIQUE INDEX wager_transactions_processed_reversal_uidx
    ON wagering.wager_transactions (resolved_reference_id, kind)
    WHERE status = 'PROCESSED'
      AND kind IN ('REFUND', 'ROLLBACK')
      AND resolved_reference_id IS NOT NULL;

CREATE INDEX wager_transactions_wallet_id_idx
    ON wagering.wager_transactions (wallet_id);

CREATE INDEX wager_transactions_pending_resume_idx
    ON wagering.wager_transactions (status, next_attempt_at)
    WHERE status IN ('PENDING', 'PENDING_REFERENCE');

CREATE TABLE wagering.wallet_ledger_entries (
    id                   TEXT        PRIMARY KEY,
    wallet_id            TEXT        NOT NULL REFERENCES wagering.wallets (id),
    transaction_id       TEXT        NOT NULL REFERENCES wagering.wager_transactions (id),
    direction            TEXT        NOT NULL CHECK (direction IN ('DEBIT', 'CREDIT')),
    amount_minor         BIGINT      NOT NULL CHECK (amount_minor > 0),
    currency             CHAR(3)     NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    balance_before_minor BIGINT      NOT NULL CHECK (balance_before_minor >= 0),
    balance_after_minor  BIGINT      NOT NULL CHECK (balance_after_minor >= 0),
    created_at           TIMESTAMPTZ NOT NULL,
    CONSTRAINT wallet_ledger_entries_wallet_id_transaction_id_key UNIQUE (wallet_id, transaction_id),
    CONSTRAINT wallet_ledger_entries_balance_invariant CHECK (
        (
            direction = 'CREDIT'
            AND balance_after_minor::numeric = balance_before_minor::numeric + amount_minor::numeric
        )
        OR (
            direction = 'DEBIT'
            AND balance_after_minor::numeric = balance_before_minor::numeric - amount_minor::numeric
        )
    )
);

CREATE INDEX wallet_ledger_entries_wallet_created_id_idx
    ON wagering.wallet_ledger_entries (wallet_id, created_at, id);

CREATE FUNCTION wagering.wallet_ledger_entries_append_only()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'wallet_ledger_entries is append-only';
END;
$$;

CREATE TRIGGER wallet_ledger_entries_append_only
    BEFORE UPDATE OR DELETE ON wagering.wallet_ledger_entries
    FOR EACH ROW
    EXECUTE FUNCTION wagering.wallet_ledger_entries_append_only();

REVOKE UPDATE, DELETE, TRUNCATE ON wagering.wallet_ledger_entries FROM PUBLIC;
