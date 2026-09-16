package domain

import (
	"strings"
	"testing"
)

func TestHashCanonicalPayloadStableAndExcludesTransport(t *testing.T) {
	t.Parallel()
	money := mustMoney(t, "25.00", "BRL")
	base := CanonicalPayload{
		ProviderID: "provider-a",
		ExternalID: "transaction-123",
		PlayerID:   "player-1",
		WalletID:   "wal-1",
		RoundID:    "round-987",
		GameID:     "fortune-chimp",
		Kind:       KindBet,
		Money:      money,
	}
	h1, err := HashCanonicalPayload(base)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := HashCanonicalPayload(base)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("hash not stable: %s vs %s", h1, h2)
	}
	if len(h1) != 64 || strings.ToLower(h1) != h1 {
		t.Fatalf("want 64 lowercase hex, got %q", h1)
	}

	otherAmount := base
	otherAmount.Money = mustMoney(t, "26.00", "BRL")
	h3, err := HashCanonicalPayload(otherAmount)
	if err != nil {
		t.Fatal(err)
	}
	if h3 == h1 {
		t.Fatal("different amount must change hash")
	}
}

func TestHashCanonicalPayloadOmitsEmptyReference(t *testing.T) {
	t.Parallel()
	money := mustMoney(t, "25.00", "BRL")
	without := CanonicalPayload{
		ProviderID: "provider-a",
		ExternalID: "tx-1",
		PlayerID:   "p",
		WalletID:   "w",
		RoundID:    "r",
		GameID:     "g",
		Kind:       KindWin,
		Money:      money,
	}
	withEmpty := without
	withEmpty.ReferenceExternalID = ""
	h1, err := HashCanonicalPayload(without)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := HashCanonicalPayload(withEmpty)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatal("empty reference must match omitted reference")
	}

	withRef := without
	withRef.ReferenceExternalID = "bet-1"
	h3, err := HashCanonicalPayload(withRef)
	if err != nil {
		t.Fatal(err)
	}
	if h3 == h1 {
		t.Fatal("present reference must change hash")
	}
}

func TestHashCanonicalPayloadRejectsUninitializedMoney(t *testing.T) {
	t.Parallel()
	_, err := HashCanonicalPayload(CanonicalPayload{Kind: KindBet})
	if err == nil {
		t.Fatal("expected error")
	}
	if _, class, ok := Classify(err); !ok || class != FailureClassValidation {
		t.Fatalf("want validation, got %v", err)
	}
}
