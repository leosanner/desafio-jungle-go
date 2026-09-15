-- Reverts Phase 3 financial objects. Schema wagering remains (000001).

DROP TRIGGER IF EXISTS wallet_ledger_entries_append_only ON wagering.wallet_ledger_entries;
DROP FUNCTION IF EXISTS wagering.wallet_ledger_entries_append_only();
DROP TABLE IF EXISTS wagering.wallet_ledger_entries;
DROP TABLE IF EXISTS wagering.wager_transactions;
DROP TABLE IF EXISTS wagering.wallets;
