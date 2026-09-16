//go:build integration

package postgres

import (
	"strings"
	"testing"

	"github.com/leosanner/desafio-jungle-go/internal/app"
)

func TestFinancialSchemaConstraints(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)

	cases := []struct {
		name       string
		constraint string
		sentinel   error
		run        func(t *testing.T) error
	}{
		{
			name:       "negative wallet balance",
			constraint: "wallets_balance_minor_check",
			sentinel:   ErrCheckViolation,
			run: func(t *testing.T) error {
				return db.exec(t.Context(), `
					INSERT INTO wagering.wallets (id, player_id, currency, balance_minor, version, created_at, updated_at)
					VALUES ($1, $2, 'BRL', -1, 1, $3, $3)`,
					uniqueID(t, "wal-"), uniqueID(t, "player-"), integrationNow)
			},
		},
		{
			name:       "duplicate player_id currency",
			constraint: "wallets_player_id_currency_key",
			sentinel:   app.ErrConflict,
			run: func(t *testing.T) error {
				playerID := uniqueID(t, "player-")
				if err := db.exec(t.Context(), `
					INSERT INTO wagering.wallets (id, player_id, currency, balance_minor, version, created_at, updated_at)
					VALUES ($1, $2, 'BRL', 0, 1, $3, $3)`,
					uniqueID(t, "wal-"), playerID, integrationNow); err != nil {
					t.Fatalf("first wallet: %v", err)
				}
				return db.exec(t.Context(), `
					INSERT INTO wagering.wallets (id, player_id, currency, balance_minor, version, created_at, updated_at)
					VALUES ($1, $2, 'BRL', 0, 1, $3, $3)`,
					uniqueID(t, "wal-"), playerID, integrationNow)
			},
		},
		{
			name:       "ledger credit balance invariant",
			constraint: "wallet_ledger_entries_balance_invariant",
			sentinel:   ErrCheckViolation,
			run: func(t *testing.T) error {
				walletID, playerID := seedWalletSQL(t, db, 0)
				txID := seedOpeningTxSQL(t, db, walletID, playerID, 10000)
				return db.exec(t.Context(), `
					INSERT INTO wagering.wallet_ledger_entries (
						id, wallet_id, transaction_id, direction, amount_minor, currency,
						balance_before_minor, balance_after_minor, created_at
					) VALUES ($1, $2, $3, 'CREDIT', 10000, 'BRL', 0, 5000, $4)`,
					uniqueID(t, "led-"), walletID, txID, integrationNow)
			},
		},
		{
			name:       "duplicate ledger wallet_id transaction_id",
			constraint: "wallet_ledger_entries_wallet_id_transaction_id_key",
			sentinel:   app.ErrConflict,
			run: func(t *testing.T) error {
				walletID, playerID := seedWalletSQL(t, db, 10000)
				txID := seedOpeningTxSQL(t, db, walletID, playerID, 10000)
				_ = seedLedgerSQL(t, db, walletID, txID, "CREDIT", 10000, 0, 10000)
				return db.exec(t.Context(), `
					INSERT INTO wagering.wallet_ledger_entries (
						id, wallet_id, transaction_id, direction, amount_minor, currency,
						balance_before_minor, balance_after_minor, created_at
					) VALUES ($1, $2, $3, 'CREDIT', 10000, 'BRL', 0, 10000, $4)`,
					uniqueID(t, "led-"), walletID, txID, integrationNow)
			},
		},
		{
			name:       "duplicate provider external_transaction_id",
			constraint: "wager_transactions_provider_external_id_uidx",
			sentinel:   app.ErrConflict,
			run: func(t *testing.T) error {
				walletID, playerID := seedWalletSQL(t, db, 0)
				provider := uniqueID(t, "prov-")
				extID := uniqueID(t, "ext-")
				_ = seedExternalTxSQL(t, db, walletID, playerID, "BET", 1000, provider, extID, uniqueID(t, "key-"))
				return db.exec(t.Context(), `
					INSERT INTO wagering.wager_transactions (
						id, origin, provider_id, external_transaction_id, idempotency_key, payload_hash,
						wallet_id, player_id, round_id, game_id, kind, amount_minor, currency, status, created_at, updated_at
					) VALUES ($1, 'EXTERNAL', $2, $3, $4, $5, $6, $7, 'round-1', 'game-1', 'BET', 1000, 'BRL', 'PENDING', $8, $8)`,
					uniqueID(t, "tx-"), provider, extID, uniqueID(t, "key-"), uniqueID(t, "hash-"),
					walletID, playerID, integrationNow)
			},
		},
		{
			name:       "duplicate provider idempotency_key",
			constraint: "wager_transactions_provider_idempotency_key_uidx",
			sentinel:   app.ErrConflict,
			run: func(t *testing.T) error {
				walletID, playerID := seedWalletSQL(t, db, 0)
				provider := uniqueID(t, "prov-")
				key := uniqueID(t, "key-")
				_ = seedExternalTxSQL(t, db, walletID, playerID, "BET", 1000, provider, uniqueID(t, "ext-"), key)
				return db.exec(t.Context(), `
					INSERT INTO wagering.wager_transactions (
						id, origin, provider_id, external_transaction_id, idempotency_key, payload_hash,
						wallet_id, player_id, round_id, game_id, kind, amount_minor, currency, status, created_at, updated_at
					) VALUES ($1, 'EXTERNAL', $2, $3, $4, $5, $6, $7, 'round-1', 'game-1', 'BET', 1000, 'BRL', 'PENDING', $8, $8)`,
					uniqueID(t, "tx-"), provider, uniqueID(t, "ext-"), key, uniqueID(t, "hash-"),
					walletID, playerID, integrationNow)
			},
		},
		{
			name:       "second OPENING on same wallet",
			constraint: "wager_transactions_opening_per_wallet_uidx",
			sentinel:   app.ErrConflict,
			run: func(t *testing.T) error {
				walletID, playerID := seedWalletSQL(t, db, 10000)
				_ = seedOpeningTxSQL(t, db, walletID, playerID, 10000)
				return db.exec(t.Context(), `
					INSERT INTO wagering.wager_transactions (
						id, origin, wallet_id, player_id, kind, amount_minor, currency, status, created_at, updated_at
					) VALUES ($1, 'INTERNAL', $2, $3, 'OPENING', 10000, 'BRL', 'PROCESSED', $4, $4)`,
					uniqueID(t, "tx-"), walletID, playerID, integrationNow)
			},
		},
		{
			name:       "INTERNAL with provider_id set",
			constraint: "wager_transactions_internal_shape",
			sentinel:   ErrCheckViolation,
			run: func(t *testing.T) error {
				walletID, playerID := seedWalletSQL(t, db, 0)
				return db.exec(t.Context(), `
					INSERT INTO wagering.wager_transactions (
						id, origin, provider_id, wallet_id, player_id, kind, amount_minor, currency, status, created_at, updated_at
					) VALUES ($1, 'INTERNAL', 'provider-x', $2, $3, 'OPENING', 10000, 'BRL', 'PROCESSED', $4, $4)`,
					uniqueID(t, "tx-"), walletID, playerID, integrationNow)
			},
		},
		{
			name:       "EXTERNAL OPENING rejected",
			constraint: "wager_transactions_external_shape",
			sentinel:   ErrCheckViolation,
			run: func(t *testing.T) error {
				walletID, playerID := seedWalletSQL(t, db, 0)
				return db.exec(t.Context(), `
					INSERT INTO wagering.wager_transactions (
						id, origin, provider_id, external_transaction_id, idempotency_key, payload_hash,
						wallet_id, player_id, round_id, game_id, kind, amount_minor, currency, status, created_at, updated_at
					) VALUES ($1, 'EXTERNAL', $2, $3, $4, $5, $6, $7, 'round-1', 'game-1', 'OPENING', 10000, 'BRL', 'PENDING', $8, $8)`,
					uniqueID(t, "tx-"), uniqueID(t, "prov-"), uniqueID(t, "ext-"), uniqueID(t, "key-"),
					uniqueID(t, "hash-"), walletID, playerID, integrationNow)
			},
		},
		{
			name:       "LOSS with non-zero amount",
			constraint: "wager_transactions_amount_by_kind",
			sentinel:   ErrCheckViolation,
			run: func(t *testing.T) error {
				walletID, playerID := seedWalletSQL(t, db, 0)
				return db.exec(t.Context(), `
					INSERT INTO wagering.wager_transactions (
						id, origin, provider_id, external_transaction_id, idempotency_key, payload_hash,
						wallet_id, player_id, round_id, game_id, kind, amount_minor, currency, status, created_at, updated_at
					) VALUES ($1, 'EXTERNAL', $2, $3, $4, $5, $6, $7, 'round-1', 'game-1', 'LOSS', 1000, 'BRL', 'PENDING', $8, $8)`,
					uniqueID(t, "tx-"), uniqueID(t, "prov-"), uniqueID(t, "ext-"), uniqueID(t, "key-"),
					uniqueID(t, "hash-"), walletID, playerID, integrationNow)
			},
		},
		{
			name:       "BET with zero amount",
			constraint: "wager_transactions_amount_by_kind",
			sentinel:   ErrCheckViolation,
			run: func(t *testing.T) error {
				walletID, playerID := seedWalletSQL(t, db, 0)
				return db.exec(t.Context(), `
					INSERT INTO wagering.wager_transactions (
						id, origin, provider_id, external_transaction_id, idempotency_key, payload_hash,
						wallet_id, player_id, round_id, game_id, kind, amount_minor, currency, status, created_at, updated_at
					) VALUES ($1, 'EXTERNAL', $2, $3, $4, $5, $6, $7, 'round-1', 'game-1', 'BET', 0, 'BRL', 'PENDING', $8, $8)`,
					uniqueID(t, "tx-"), uniqueID(t, "prov-"), uniqueID(t, "ext-"), uniqueID(t, "key-"),
					uniqueID(t, "hash-"), walletID, playerID, integrationNow)
			},
		},
		{
			name:       "duplicate inbox consumer_name message_id",
			constraint: "inbox_messages_pkey",
			sentinel:   app.ErrConflict,
			run: func(t *testing.T) error {
				hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				msgID := uniqueID(t, "msg-")
				if err := db.exec(t.Context(), `
					INSERT INTO wagering.inbox_messages (consumer_name, message_id, payload_hash, received_at, completed_at)
					VALUES ('wager-transactions', $1, $2, $3, $3)`,
					msgID, hash, integrationNow); err != nil {
					t.Fatalf("first inbox: %v", err)
				}
				return db.exec(t.Context(), `
					INSERT INTO wagering.inbox_messages (consumer_name, message_id, payload_hash, received_at, completed_at)
					VALUES ('wager-transactions', $1, $2, $3, $3)`,
					msgID, hash, integrationNow)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.run(t)
			requireConstraint(t, err, tc.sentinel, tc.constraint)
		})
	}
}

func TestLedgerAppendOnly(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)

	walletID, playerID := seedWalletSQL(t, db, 10000)
	txID := seedOpeningTxSQL(t, db, walletID, playerID, 10000)
	ledID := seedLedgerSQL(t, db, walletID, txID, "CREDIT", 10000, 0, 10000)

	const triggerMsg = "wallet_ledger_entries is append-only"

	t.Run("update", func(t *testing.T) {
		err := db.exec(t.Context(), `
			UPDATE wagering.wallet_ledger_entries SET amount_minor = 1 WHERE id = $1`, ledID)
		if err == nil || !strings.Contains(err.Error(), triggerMsg) {
			t.Fatalf("UPDATE error = %v, want containing %q", err, triggerMsg)
		}
	})

	t.Run("delete", func(t *testing.T) {
		err := db.exec(t.Context(), `
			DELETE FROM wagering.wallet_ledger_entries WHERE id = $1`, ledID)
		if err == nil || !strings.Contains(err.Error(), triggerMsg) {
			t.Fatalf("DELETE error = %v, want containing %q", err, triggerMsg)
		}
	})
}
