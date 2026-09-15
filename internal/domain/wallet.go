package domain

import (
	"fmt"
	"time"
)

const initialWalletVersion int64 = 1

// Wallet is the financial aggregate root.
type Wallet struct {
	id        string
	playerID  string
	currency  string
	balance   Money
	version   int64
	createdAt time.Time
	updatedAt time.Time
}

// RehydrateWallet rebuilds a wallet from persistence without applying movements or emitting events.
func RehydrateWallet(
	id, playerID string,
	balance Money,
	version int64,
	createdAt, updatedAt time.Time,
) (Wallet, error) {
	if err := requireID("id", id); err != nil {
		return Wallet{}, err
	}
	if err := requireID("playerId", playerID); err != nil {
		return Wallet{}, err
	}
	if err := balance.RequireInitialized(); err != nil {
		return Wallet{}, err
	}
	if balance.IsNegative() {
		return Wallet{}, validation(FailureLedgerInvariant, fmt.Errorf("%w: negative wallet balance", ErrLedgerInvariant))
	}
	if version < initialWalletVersion {
		return Wallet{}, validation(FailureMissingIdentity, fmt.Errorf("%w: version", ErrMissingIdentity))
	}
	cur, err := ParseCurrency(balance.Currency())
	if err != nil {
		return Wallet{}, err
	}
	return Wallet{
		id:        id,
		playerID:  playerID,
		currency:  cur,
		balance:   balance,
		version:   version,
		createdAt: createdAt.UTC(),
		updatedAt: updatedAt.UTC(),
	}, nil
}

func (w Wallet) ID() string           { return w.id }
func (w Wallet) PlayerID() string     { return w.playerID }
func (w Wallet) Currency() string     { return w.currency }
func (w Wallet) Balance() Money       { return w.balance }
func (w Wallet) Version() int64       { return w.version }
func (w Wallet) CreatedAt() time.Time { return w.createdAt }
func (w Wallet) UpdatedAt() time.Time { return w.updatedAt }

func (w *Wallet) requireCurrency(amount Money) error {
	if err := amount.RequireInitialized(); err != nil {
		return err
	}
	if amount.Currency() != w.currency {
		return validation(FailureIncompatibleCurrency, fmt.Errorf("%w: movement %s wallet %s", ErrIncompatibleCurrency, amount.Currency(), w.currency))
	}
	return nil
}

// Debit subtracts a positive amount, keeping balance >= 0, and returns the ledger entry.
// The caller must persist wallet and entry in the same commit.
func (w *Wallet) Debit(ledgerID, transactionID string, amount Money, now time.Time) (WalletLedgerEntry, error) {
	return w.move(ledgerID, transactionID, amount, DirectionDebit, now)
}

// Credit adds a positive amount and returns the ledger entry.
func (w *Wallet) Credit(ledgerID, transactionID string, amount Money, now time.Time) (WalletLedgerEntry, error) {
	return w.move(ledgerID, transactionID, amount, DirectionCredit, now)
}

func (w *Wallet) move(ledgerID, transactionID string, amount Money, dir Direction, now time.Time) (WalletLedgerEntry, error) {
	if w == nil {
		return WalletLedgerEntry{}, validation(FailureMissingIdentity, fmt.Errorf("%w: wallet", ErrMissingIdentity))
	}
	if err := w.requireCurrency(amount); err != nil {
		return WalletLedgerEntry{}, err
	}
	if !amount.IsPositive() {
		return WalletLedgerEntry{}, validation(FailurePositiveAmountRequired, ErrPositiveAmountRequired)
	}

	before := w.balance
	var after Money
	var err error
	switch dir {
	case DirectionDebit:
		after, err = before.Sub(amount)
		if err != nil {
			return WalletLedgerEntry{}, err
		}
		if after.IsNegative() {
			return WalletLedgerEntry{}, rejection(FailureInsufficientFunds, ErrInsufficientFunds)
		}
	case DirectionCredit:
		after, err = before.Add(amount)
		if err != nil {
			return WalletLedgerEntry{}, err
		}
	default:
		return WalletLedgerEntry{}, validation(FailureInvalidDirection, ErrInvalidDirection)
	}

	entry, err := NewLedgerEntry(ledgerID, w.id, transactionID, dir, amount, before, after, now)
	if err != nil {
		return WalletLedgerEntry{}, err
	}
	w.balance = after
	w.version++
	w.updatedAt = now.UTC()
	return entry, nil
}
