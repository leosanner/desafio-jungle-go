-- Reverts Phase 5 outbox. Financial tables from 000002 remain.

DROP INDEX IF EXISTS wagering.outbox_events_unpublished_idx;
DROP TABLE IF EXISTS wagering.outbox_events;
