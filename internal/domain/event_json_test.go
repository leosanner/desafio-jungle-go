package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEventJSONUsesCamelCaseAndMoneyStrings(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	money := mustMoney(t, "25.00", "BRL")
	ev := WagerTransactionProcessed{
		TransactionID:         "tx-1",
		WalletID:              "wal-1",
		PlayerID:              "player-1",
		Origin:                OriginExternal,
		ProviderID:            "provider-a",
		ExternalTransactionID: "ext-1",
		Kind:                  KindBet,
		Money:                 money,
		OccurredAtUTC:         now,
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`"transactionId":"tx-1"`,
		`"walletId":"wal-1"`,
		`"providerId":"provider-a"`,
		`"kind":"BET"`,
		`"amount":"25.00"`,
		`"currency":"BRL"`,
		`"occurredAt":"2026-09-16T12:00:00Z"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("json %s missing %s", s, want)
		}
	}
	if strings.Contains(s, "TransactionID") {
		t.Errorf("Go field name leaked: %s", s)
	}
}

func TestOpeningEventOmitsEmptyProvider(t *testing.T) {
	t.Parallel()
	ev := WagerTransactionProcessed{
		TransactionID: "tx-open",
		WalletID:      "wal-1",
		PlayerID:      "player-1",
		Origin:        OriginInternal,
		Kind:          KindOpening,
		Money:         mustMoney(t, "10.00", "BRL"),
		OccurredAtUTC: time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC),
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "providerId") || strings.Contains(string(b), "externalTransactionId") {
		t.Errorf("unexpected provider fields: %s", b)
	}
}

func TestWalletBalanceChangedJSONPayload(t *testing.T) {
	t.Parallel()
	before := mustMoney(t, "100.00", "BRL")
	after := mustMoney(t, "75.00", "BRL")
	ev := NewWalletBalanceChanged(WalletBalanceChangedParams{
		WalletID:      "wal-1",
		TransactionID: "tx-1",
		Direction:     DirectionDebit,
		Money:         mustMoney(t, "25.00", "BRL"),
		BalanceBefore: before,
		BalanceAfter:  after,
		WalletVersion: 2,
		OccurredAt:    time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC),
	})
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`"walletId":"wal-1"`,
		`"transactionId":"tx-1"`,
		`"direction":"DEBIT"`,
		`"walletVersion":2`,
		`"balanceBefore":{"amount":"100.00","currency":"BRL"}`,
		`"balanceAfter":{"amount":"75.00","currency":"BRL"}`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("json %s missing %s", s, want)
		}
	}
}
