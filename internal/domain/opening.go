package domain

import "time"

// Opening is the result of creating a wallet. Zero initial balance yields no
// OPENING transaction, ledger or financial events; version is still 1.
type Opening struct {
	Wallet      Wallet
	Transaction *WagerTransaction
	Ledger      *WalletLedgerEntry
	Events      []Event
}

// OpenWalletParams identifies a new wallet. IDs are assigned by the caller (clock and ID generator injected).
type OpenWalletParams struct {
	WalletID    string
	PlayerID    string
	OpeningTxID string
	LedgerID    string
	Initial     Money
	Now         time.Time
}

// OpenWallet creates a wallet. Positive initial balance posts an internal OPENING
// credit in the same conceptual commit (transaction + ledger + events). Version stays 1.
func OpenWallet(p OpenWalletParams) (Opening, error) {
	if err := requireID("walletId", p.WalletID); err != nil {
		return Opening{}, err
	}
	if err := requireID("playerId", p.PlayerID); err != nil {
		return Opening{}, err
	}
	if err := p.Initial.RequireInitialized(); err != nil {
		return Opening{}, err
	}
	if p.Initial.IsNegative() {
		return Opening{}, validation(FailureInvalidAmount, ErrInvalidAmount)
	}

	now := p.Now.UTC()
	w, err := RehydrateWallet(p.WalletID, p.PlayerID, p.Initial, initialWalletVersion, now, now)
	if err != nil {
		return Opening{}, err
	}

	if p.Initial.IsZero() {
		return Opening{Wallet: w}, nil
	}

	if err := requireID("openingTxId", p.OpeningTxID); err != nil {
		return Opening{}, err
	}
	if err := requireID("ledgerId", p.LedgerID); err != nil {
		return Opening{}, err
	}

	zero, err := Zero(p.Initial.Currency())
	if err != nil {
		return Opening{}, err
	}
	entry, err := NewLedgerEntry(p.LedgerID, p.WalletID, p.OpeningTxID, DirectionCredit, p.Initial, zero, p.Initial, now)
	if err != nil {
		return Opening{}, err
	}

	tx, err := NewOpeningTransaction(OpeningTxParams{
		ID:       p.OpeningTxID,
		WalletID: p.WalletID,
		PlayerID: p.PlayerID,
		Money:    p.Initial,
		Now:      now,
	})
	if err != nil {
		return Opening{}, err
	}
	if err := tx.MarkProcessed(p.Initial, now); err != nil {
		return Opening{}, err
	}

	events := []Event{
		NewWagerTransactionProcessed(tx, now),
		NewWalletBalanceChanged(WalletBalanceChangedParams{
			WalletID:      w.id,
			TransactionID: tx.id,
			Direction:     DirectionCredit,
			Money:         p.Initial,
			BalanceBefore: zero,
			BalanceAfter:  p.Initial,
			WalletVersion: w.version,
			OccurredAt:    now,
		}),
	}

	return Opening{
		Wallet:      w,
		Transaction: &tx,
		Ledger:      &entry,
		Events:      events,
	}, nil
}
