package domain

import (
	"errors"
	"testing"
)

func TestLedgerValidatesInvariant(t *testing.T) {
	t.Parallel()
	money := mustMoney(t, "25.00", "BRL")
	before := mustMoney(t, "100.00", "BRL")
	after := mustMoney(t, "75.00", "BRL")
	e, err := NewLedgerEntry("led-1", "wal-1", "tx-1", DirectionDebit, money, before, after, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if e.Direction() != DirectionDebit {
		t.Fatalf("direction = %s", e.Direction())
	}

	_, err = NewLedgerEntry("led-1", "wal-1", "tx-1", DirectionDebit, money, before, mustMoney(t, "80.00", "BRL"), testNow)
	if !errors.Is(err, ErrLedgerInvariant) {
		t.Fatalf("bad after: %v", err)
	}

	_, err = NewLedgerEntry("led-1", "wal-1", "tx-1", DirectionCredit, money, before, after, testNow)
	if !errors.Is(err, ErrLedgerInvariant) {
		t.Fatalf("credit with debit after: %v", err)
	}

	_, err = NewLedgerEntry("led-1", "wal-1", "tx-1", DirectionDebit, mustZero(t, "BRL"), before, before, testNow)
	if !errors.Is(err, ErrPositiveAmountRequired) {
		t.Fatalf("zero money: %v", err)
	}
}

func TestRehydrateLedgerDoesNotChangeMeaning(t *testing.T) {
	t.Parallel()
	money := mustMoney(t, "10.00", "BRL")
	before := mustZero(t, "BRL")
	after := money
	e, err := RehydrateLedgerEntry("led-1", "wal-1", "tx-1", DirectionCredit, money, before, after, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if e.ID() != "led-1" || e.WalletID() != "wal-1" {
		t.Fatalf("ids: %+v", e)
	}
}
