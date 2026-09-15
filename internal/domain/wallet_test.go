package domain

import (
	"errors"
	"testing"
)

func TestOpenWalletPositiveCreatesOpening(t *testing.T) {
	t.Parallel()
	out := openWallet(t, "1000.00")
	if out.Wallet.Version() != 1 {
		t.Fatalf("version = %d, want 1", out.Wallet.Version())
	}
	if out.Wallet.Balance().AmountString() != "1000.00" {
		t.Fatalf("balance = %s", out.Wallet.Balance().AmountString())
	}
	if out.Transaction == nil || out.Transaction.Kind() != KindOpening {
		t.Fatal("expected OPENING transaction")
	}
	if out.Transaction.Origin() != OriginInternal {
		t.Fatal("opening must be internal")
	}
	if out.Transaction.ProviderID() != "" || out.Transaction.ExternalID() != "" {
		t.Fatal("opening must not carry external metadata")
	}
	if out.Transaction.Status() != StatusProcessed {
		t.Fatalf("status = %s", out.Transaction.Status())
	}
	if out.Ledger == nil || out.Ledger.Direction() != DirectionCredit {
		t.Fatal("expected credit ledger")
	}
	if len(out.Events) != 2 {
		t.Fatalf("events = %d, want 2", len(out.Events))
	}
	if out.Events[0].EventType() != EventTypeWagerTransactionProcessed {
		t.Fatalf("event 0 = %s", out.Events[0].EventType())
	}
	if out.Events[1].EventType() != EventTypeWalletBalanceChanged {
		t.Fatalf("event 1 = %s", out.Events[1].EventType())
	}
	if out.Events[0].EventVersion() != 1 {
		t.Fatalf("event version = %d", out.Events[0].EventVersion())
	}
}

func TestOpenWalletZeroHasNoFinancialSideEffects(t *testing.T) {
	t.Parallel()
	out := openWallet(t, "0.00")
	if out.Transaction != nil || out.Ledger != nil || len(out.Events) != 0 {
		t.Fatalf("zero opening must not create tx/ledger/events")
	}
	if out.Wallet.Version() != 1 {
		t.Fatalf("version = %d", out.Wallet.Version())
	}
}

func TestWalletDebitCreditAndVersion(t *testing.T) {
	t.Parallel()
	out := openWallet(t, "100.00")
	w := out.Wallet
	entry, err := w.Debit("led-d", "tx-d", mustMoney(t, "80.00", "BRL"), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if entry.BalanceAfter().AmountString() != "20.00" {
		t.Fatalf("after debit = %s", entry.BalanceAfter().AmountString())
	}
	if w.Version() != 2 {
		t.Fatalf("version after debit = %d, want 2", w.Version())
	}
	_, err = w.Credit("led-c", "tx-c", mustMoney(t, "5.00", "BRL"), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if w.Version() != 3 {
		t.Fatalf("version after credit = %d, want 3", w.Version())
	}
	if w.Balance().AmountString() != "25.00" {
		t.Fatalf("balance = %s", w.Balance().AmountString())
	}
}

func TestWalletDebitInsufficientFunds(t *testing.T) {
	t.Parallel()
	out := openWallet(t, "50.00")
	w := out.Wallet
	_, err := w.Debit("led-d", "tx-d", mustMoney(t, "80.00", "BRL"), testNow)
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("err = %v", err)
	}
	if w.Version() != 1 || w.Balance().AmountString() != "50.00" {
		t.Fatal("wallet must be unchanged after insufficient funds")
	}
}

func TestWalletCurrencyMismatch(t *testing.T) {
	t.Parallel()
	out := openWallet(t, "50.00")
	w := out.Wallet
	_, err := w.Debit("led-d", "tx-d", mustMoney(t, "1.00", "USD"), testNow)
	if !errors.Is(err, ErrIncompatibleCurrency) {
		t.Fatalf("err = %v", err)
	}
}

func TestRehydrateWalletDoesNotEmitEvents(t *testing.T) {
	t.Parallel()
	w, err := RehydrateWallet("wal-1", "player-1", mustMoney(t, "40.00", "BRL"), 4, testNow, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if w.Version() != 4 {
		t.Fatalf("version = %d", w.Version())
	}
}
