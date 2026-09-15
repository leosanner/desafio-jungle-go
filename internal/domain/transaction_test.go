package domain

import (
	"errors"
	"testing"
)

func TestNewExternalTransactionRejectsOpening(t *testing.T) {
	t.Parallel()
	_, err := NewExternalTransaction(ExternalTxParams{
		ID:             "tx-1",
		ProviderID:     "provider-a",
		ExternalID:     "ext-1",
		IdempotencyKey: "k",
		PayloadHash:    "h",
		WalletID:       "wal-1",
		PlayerID:       "player-1",
		RoundID:        "round-1",
		GameID:         "g",
		Kind:           KindOpening,
		Money:          mustMoney(t, "1.00", "BRL"),
		Now:            testNow,
	})
	if !errors.Is(err, ErrOpeningNotAllowed) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewExternalTransactionAmountPolicy(t *testing.T) {
	t.Parallel()
	_, err := NewExternalTransaction(ExternalTxParams{
		ID: "tx-1", ProviderID: "p", ExternalID: "e", IdempotencyKey: "k", PayloadHash: "h",
		WalletID: "w", PlayerID: "pl", RoundID: "r", GameID: "g",
		Kind: KindBet, Money: mustZero(t, "BRL"), Now: testNow,
	})
	if !errors.Is(err, ErrPositiveAmountRequired) {
		t.Fatalf("BET zero: %v", err)
	}

	_, err = NewExternalTransaction(ExternalTxParams{
		ID: "tx-1", ProviderID: "p", ExternalID: "e", IdempotencyKey: "k", PayloadHash: "h",
		WalletID: "w", PlayerID: "pl", RoundID: "r", GameID: "g",
		Kind: KindLoss, Money: mustMoney(t, "1.00", "BRL"), Now: testNow,
	})
	if !errors.Is(err, ErrZeroAmountRequired) {
		t.Fatalf("LOSS positive: %v", err)
	}

	_, err = NewExternalTransaction(ExternalTxParams{
		ID: "tx-1", ProviderID: "p", ExternalID: "e", IdempotencyKey: "k", PayloadHash: "h",
		WalletID: "w", PlayerID: "pl", RoundID: "r", GameID: "g",
		Kind: KindRefund, Money: mustMoney(t, "1.00", "BRL"), Now: testNow,
	})
	if !errors.Is(err, ErrMissingReference) {
		t.Fatalf("REFUND without ref: %v", err)
	}
}

func TestStateMachineTransitions(t *testing.T) {
	t.Parallel()
	tx := externalTx(t, KindBet, "10.00", "tx-1")
	if tx.Status() != StatusPending {
		t.Fatalf("start = %s", tx.Status())
	}
	if err := tx.MarkPendingReference("", testNow); err != nil {
		t.Fatal(err)
	}
	if tx.Status() != StatusPendingReference {
		t.Fatalf("status = %s", tx.Status())
	}
	if err := tx.MarkProcessed(mustMoney(t, "90.00", "BRL"), testNow); err != nil {
		t.Fatal(err)
	}
	if tx.Status() != StatusProcessed {
		t.Fatal("expected PROCESSED")
	}
	if err := tx.MarkRejected(FailureInsufficientFunds, testNow); !errors.Is(err, ErrTerminalState) {
		t.Fatalf("terminal reject: %v", err)
	}
	bal, ok := tx.ResultBalance()
	if !ok || bal.AmountString() != "90.00" {
		t.Fatalf("result balance = %v %v", bal.AmountString(), ok)
	}
}

func TestRejectedIsTerminal(t *testing.T) {
	t.Parallel()
	tx := externalTx(t, KindBet, "10.00", "tx-1")
	if err := tx.MarkRejected(FailureInsufficientFunds, testNow); err != nil {
		t.Fatal(err)
	}
	if err := tx.MarkProcessed(mustMoney(t, "1.00", "BRL"), testNow); !errors.Is(err, ErrTerminalState) {
		t.Fatalf("err = %v", err)
	}
}

func TestRehydrateTransactionDoesNotTransition(t *testing.T) {
	t.Parallel()
	original := externalTx(t, KindBet, "10.00", "tx-1")
	if err := original.MarkProcessed(mustMoney(t, "90.00", "BRL"), testNow); err != nil {
		t.Fatal(err)
	}
	rb, _ := original.ResultBalance()
	restored, err := RehydrateTransaction(RehydrateTxParams{
		ID: original.ID(), Origin: original.Origin(), ProviderID: original.ProviderID(),
		ExternalID: original.ExternalID(), IdempotencyKey: original.IdempotencyKey(),
		PayloadHash: original.PayloadHash(), WalletID: original.WalletID(), PlayerID: original.PlayerID(),
		RoundID: original.RoundID(), GameID: original.GameID(), Kind: original.Kind(), Money: original.Money(),
		Status: original.Status(), ResultBalance: rb, HasResult: true,
		CreatedAt: original.CreatedAt(), UpdatedAt: original.UpdatedAt(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status() != StatusProcessed {
		t.Fatalf("status = %s", restored.Status())
	}
}

func TestEventConstructorsSetTypeAndVersion(t *testing.T) {
	t.Parallel()
	tx := externalTx(t, KindLoss, "0.00", "tx-loss")
	if err := tx.MarkProcessed(mustMoney(t, "10.00", "BRL"), testNow); err != nil {
		t.Fatal(err)
	}
	ev := NewWagerTransactionProcessed(tx, testNow)
	if ev.EventType() != EventTypeWagerTransactionProcessed || ev.EventVersion() != 1 {
		t.Fatalf("processed meta: %s v%d", ev.EventType(), ev.EventVersion())
	}
	if ev.OccurredAt() != testNow.UTC() {
		t.Fatalf("occurredAt = %s", ev.OccurredAt())
	}
}
