-- Phase 7 inbound inbox. Completed rows are inserted in the same commit as
-- domain/ledger/outbox (ADR 0017). Identity is (consumer_name, message_id).

CREATE TABLE wagering.inbox_messages (
    consumer_name  TEXT        NOT NULL,
    message_id     TEXT        NOT NULL,
    payload_hash   TEXT        NOT NULL CHECK (payload_hash ~ '^[0-9a-f]{64}$'),
    received_at    TIMESTAMPTZ NOT NULL,
    completed_at   TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (consumer_name, message_id),
    CHECK (completed_at >= received_at)
);
