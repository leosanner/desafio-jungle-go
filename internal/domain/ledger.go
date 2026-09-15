package domain

import (
	"fmt"
	"strings"
	"time"
)

// WalletLedgerEntry is an immutable financial posting.
type WalletLedgerEntry struct {
	id            string
	walletID      string
	transactionID string
	direction     Direction
	money         Money
	balanceBefore Money
	balanceAfter  Money
	createdAt     time.Time
}

// NewLedgerEntry validates balanceAfter = balanceBefore ± money for the direction.
func NewLedgerEntry(
	id, walletID, transactionID string,
	direction Direction,
	money, balanceBefore, balanceAfter Money,
	createdAt time.Time,
) (WalletLedgerEntry, error) {
	return newLedgerEntry(id, walletID, transactionID, direction, money, balanceBefore, balanceAfter, createdAt)
}

// RehydrateLedgerEntry rebuilds an entry from persistence. It validates the invariant
// but does not apply a movement to a wallet.
func RehydrateLedgerEntry(
	id, walletID, transactionID string,
	direction Direction,
	money, balanceBefore, balanceAfter Money,
	createdAt time.Time,
) (WalletLedgerEntry, error) {
	return newLedgerEntry(id, walletID, transactionID, direction, money, balanceBefore, balanceAfter, createdAt)
}

func newLedgerEntry(
	id, walletID, transactionID string,
	direction Direction,
	money, balanceBefore, balanceAfter Money,
	createdAt time.Time,
) (WalletLedgerEntry, error) {
	if err := requireID("id", id); err != nil {
		return WalletLedgerEntry{}, err
	}
	if err := requireID("walletId", walletID); err != nil {
		return WalletLedgerEntry{}, err
	}
	if err := requireID("transactionId", transactionID); err != nil {
		return WalletLedgerEntry{}, err
	}
	if _, err := ParseDirection(string(direction)); err != nil {
		return WalletLedgerEntry{}, err
	}
	if !money.IsPositive() {
		return WalletLedgerEntry{}, validation(FailurePositiveAmountRequired, ErrPositiveAmountRequired)
	}
	if err := money.requireCompatible(balanceBefore); err != nil {
		return WalletLedgerEntry{}, err
	}
	if err := money.requireCompatible(balanceAfter); err != nil {
		return WalletLedgerEntry{}, err
	}
	if balanceBefore.IsNegative() || balanceAfter.IsNegative() {
		return WalletLedgerEntry{}, validation(FailureLedgerInvariant, fmt.Errorf("%w: negative balance on ledger", ErrLedgerInvariant))
	}

	var expected Money
	var err error
	switch direction {
	case DirectionDebit:
		expected, err = balanceBefore.Sub(money)
	case DirectionCredit:
		expected, err = balanceBefore.Add(money)
	}
	if err != nil {
		return WalletLedgerEntry{}, err
	}
	eq, err := expected.Equal(balanceAfter)
	if err != nil {
		return WalletLedgerEntry{}, err
	}
	if !eq {
		return WalletLedgerEntry{}, validation(FailureLedgerInvariant, fmt.Errorf("%w: balanceAfter != balanceBefore ± money", ErrLedgerInvariant))
	}

	return WalletLedgerEntry{
		id:            id,
		walletID:      walletID,
		transactionID: transactionID,
		direction:     direction,
		money:         money,
		balanceBefore: balanceBefore,
		balanceAfter:  balanceAfter,
		createdAt:     createdAt.UTC(),
	}, nil
}

func (e WalletLedgerEntry) ID() string            { return e.id }
func (e WalletLedgerEntry) WalletID() string      { return e.walletID }
func (e WalletLedgerEntry) TransactionID() string { return e.transactionID }
func (e WalletLedgerEntry) Direction() Direction  { return e.direction }
func (e WalletLedgerEntry) Money() Money          { return e.money }
func (e WalletLedgerEntry) BalanceBefore() Money  { return e.balanceBefore }
func (e WalletLedgerEntry) BalanceAfter() Money   { return e.balanceAfter }
func (e WalletLedgerEntry) CreatedAt() time.Time  { return e.createdAt }

func requireID(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return validation(FailureMissingIdentity, fmt.Errorf("%w: %s", ErrMissingIdentity, name))
	}
	return nil
}
