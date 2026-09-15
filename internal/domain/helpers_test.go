package domain

import (
	"testing"
	"time"
)

var testNow = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

func mustMoney(t *testing.T, amount, currency string) Money {
	t.Helper()
	m, err := ParseMoney(amount, currency)
	if err != nil {
		t.Fatalf("ParseMoney(%q, %q): %v", amount, currency, err)
	}
	return m
}

func mustZero(t *testing.T, currency string) Money {
	t.Helper()
	z, err := Zero(currency)
	if err != nil {
		t.Fatalf("Zero(%q): %v", currency, err)
	}
	return z
}

func openWallet(t *testing.T, initial string) Opening {
	t.Helper()
	out, err := OpenWallet(OpenWalletParams{
		WalletID:    "wal-1",
		PlayerID:    "player-1",
		OpeningTxID: "tx-open",
		LedgerID:    "led-open",
		Initial:     mustMoney(t, initial, "BRL"),
		Now:         testNow,
	})
	if err != nil {
		t.Fatalf("OpenWallet: %v", err)
	}
	return out
}

func externalTx(t *testing.T, kind Kind, amount, id string) WagerTransaction {
	t.Helper()
	return externalTxRef(t, kind, amount, id, "")
}

func externalTxRef(t *testing.T, kind Kind, amount, id, ref string) WagerTransaction {
	t.Helper()
	tx, err := NewExternalTransaction(ExternalTxParams{
		ID:                  id,
		ProviderID:          "provider-a",
		ExternalID:          "ext-" + id,
		IdempotencyKey:      "provider-a:ext-" + id,
		PayloadHash:         "hash-" + id,
		WalletID:            "wal-1",
		PlayerID:            "player-1",
		RoundID:             "round-1",
		GameID:              "fortune-chimp",
		Kind:                kind,
		Money:               mustMoney(t, amount, "BRL"),
		ReferenceExternalID: ref,
		Now:                 testNow,
	})
	if err != nil {
		t.Fatalf("NewExternalTransaction: %v", err)
	}
	return tx
}

func applyOn(t *testing.T, w *Wallet, op WagerTransaction, ref *WagerTransaction, reversals []WagerTransaction) ApplyResult {
	t.Helper()
	res, err := Apply(ApplyInput{
		Wallet:             w,
		Operation:          op,
		Reference:          ref,
		ReferenceLookedUp:  true,
		ProcessedReversals: reversals,
		LedgerID:           "led-" + op.id,
		Now:                testNow,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return res
}
