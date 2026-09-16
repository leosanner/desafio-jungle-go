-- Phase 5 transactional outbox. Rows are inserted unpublished in the same
-- commit as wallet/transaction/ledger. The publisher worker is Phase 6.

CREATE TABLE wagering.outbox_events (
    event_id         TEXT        PRIMARY KEY,
    event_type       TEXT        NOT NULL,
    event_version    INTEGER     NOT NULL CHECK (event_version >= 1),
    aggregate_id     TEXT        NOT NULL,
    correlation_id   TEXT        NOT NULL,
    causation_id     TEXT,
    occurred_at      TIMESTAMPTZ NOT NULL,
    payload          JSONB       NOT NULL,
    attempts         INTEGER     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at  TIMESTAMPTZ NOT NULL,
    published_at     TIMESTAMPTZ
);

CREATE INDEX outbox_events_unpublished_idx
    ON wagering.outbox_events (next_attempt_at)
    WHERE published_at IS NULL;
