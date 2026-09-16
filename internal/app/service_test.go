package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

func testService(t *testing.T) (*Service, *memStore) {
	t.Helper()
	store := newMemStore()
	svc := NewService(store, fixedClock{t: time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)}, &seqIDs{}, NopMetrics{})
	return svc, store
}

func mustParse(t *testing.T, amount, currency string) domain.Money {
	t.Helper()
	m, err := domain.ParseMoney(amount, currency)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestOpenWalletPositiveAndZero(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("positive writes opening ledger and outbox", func(t *testing.T) {
		t.Parallel()
		svc, store := testService(t)
		out, err := svc.OpenWallet(ctx, OpenWalletCommand{
			PlayerID:       "player-1",
			InitialBalance: mustParse(t, "100.00", "BRL"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if out.Wallet.Version() != 1 {
			t.Fatalf("version = %d", out.Wallet.Version())
		}
		store.mu.Lock()
		defer store.mu.Unlock()
		if len(store.txs) != 1 {
			t.Fatalf("txs = %d, want 1", len(store.txs))
		}
		if len(store.ledger) != 1 {
			t.Fatalf("ledger = %d, want 1", len(store.ledger))
		}
		if len(store.outbox) != 2 {
			t.Fatalf("outbox = %d, want 2", len(store.outbox))
		}
	})

	t.Run("zero writes wallet only", func(t *testing.T) {
		t.Parallel()
		svc, store := testService(t)
		_, err := svc.OpenWallet(ctx, OpenWalletCommand{
			PlayerID:       "player-1",
			InitialBalance: mustParse(t, "0.00", "BRL"),
		})
		if err != nil {
			t.Fatal(err)
		}
		store.mu.Lock()
		defer store.mu.Unlock()
		if len(store.txs) != 0 || len(store.ledger) != 0 || len(store.outbox) != 0 {
			t.Fatalf("expected no financial rows")
		}
	})

	t.Run("duplicate player currency conflicts", func(t *testing.T) {
		t.Parallel()
		svc, _ := testService(t)
		cmd := OpenWalletCommand{PlayerID: "player-1", InitialBalance: mustParse(t, "10.00", "BRL")}
		if _, err := svc.OpenWallet(ctx, cmd); err != nil {
			t.Fatal(err)
		}
		_, err := svc.OpenWallet(ctx, cmd)
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("err = %v, want conflict", err)
		}
	})
}

func TestSubmitReplayAndConflicts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := testService(t)
	opened, err := svc.OpenWallet(ctx, OpenWalletCommand{
		PlayerID:       "player-1",
		InitialBalance: mustParse(t, "100.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	actor := Actor{ClientID: "provider-a", ProviderID: "provider-a"}
	cmd := SubmitCommand{
		Actor:          actor,
		IdempotencyKey: "provider-a:bet-1",
		ProviderID:     "provider-a",
		ExternalID:     "bet-1",
		PlayerID:       "player-1",
		WalletID:       opened.Wallet.ID(),
		RoundID:        "round-1",
		GameID:         "game-1",
		Kind:           domain.KindBet,
		Money:          mustParse(t, "25.00", "BRL"),
	}
	first, err := svc.Submit(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if first.IdempotentReplay || first.Transaction.Status() != domain.StatusProcessed {
		t.Fatalf("first = %+v", first)
	}
	replay, err := svc.Submit(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !replay.IdempotentReplay {
		t.Fatal("expected replay")
	}
	if replay.Transaction.ID() != first.Transaction.ID() {
		t.Fatal("replay id changed")
	}

	conflict := cmd
	conflict.Money = mustParse(t, "26.00", "BRL")
	_, err = svc.Submit(ctx, conflict)
	if _, class, ok := domain.Classify(err); !ok || class != domain.FailureClassRejection {
		t.Fatalf("payload conflict: %v", err)
	}

	dupExt := cmd
	dupExt.IdempotencyKey = "other-key"
	_, err = svc.Submit(ctx, dupExt)
	if code, _, ok := domain.Classify(err); !ok || code != domain.FailureDuplicateExternalTransaction {
		t.Fatalf("dup external: %v", err)
	}

	second, err := svc.Submit(ctx, SubmitCommand{
		Actor:          actor,
		IdempotencyKey: "provider-a:bet-2",
		ProviderID:     "provider-a",
		ExternalID:     "bet-2",
		PlayerID:       "player-1",
		WalletID:       opened.Wallet.ID(),
		RoundID:        "round-1",
		GameID:         "game-1",
		Kind:           domain.KindBet,
		Money:          mustParse(t, "10.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	bal, ok := replay.Transaction.ResultBalance()
	if !ok {
		t.Fatal("missing snapshot")
	}
	later, ok := second.Transaction.ResultBalance()
	if !ok {
		t.Fatal("missing second snapshot")
	}
	if bal.AmountString() == later.AmountString() {
		t.Fatal("second bet should move balance")
	}
	replayAgain, err := svc.Submit(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	snap, _ := replayAgain.Transaction.ResultBalance()
	if snap.AmountString() != "75.00" {
		t.Fatalf("replay balance = %s, want original 75.00", snap.AmountString())
	}
}

func TestGetTransactionHidesOtherProvider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := testService(t)
	opened, err := svc.OpenWallet(ctx, OpenWalletCommand{
		PlayerID:       "player-1",
		InitialBalance: mustParse(t, "100.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.Submit(ctx, SubmitCommand{
		Actor:          Actor{ProviderID: "provider-a"},
		IdempotencyKey: "k",
		ProviderID:     "provider-a",
		ExternalID:     "ext-1",
		PlayerID:       "player-1",
		WalletID:       opened.Wallet.ID(),
		RoundID:        "r",
		GameID:         "g",
		Kind:           domain.KindBet,
		Money:          mustParse(t, "5.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.GetTransaction(ctx, Actor{ProviderID: "provider-b"}, out.Transaction.ID())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want not found", err)
	}
	got, err := svc.GetTransaction(ctx, Actor{ProviderID: "provider-a"}, out.Transaction.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != out.Transaction.ID() {
		t.Fatal("owner should read")
	}
}

func TestReconcileWalletConsistent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := testService(t)
	opened, err := svc.OpenWallet(ctx, OpenWalletCommand{
		PlayerID:       "player-1",
		InitialBalance: mustParse(t, "50.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := svc.ReconcileWallet(ctx, opened.Wallet.ID())
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Consistent || rec.CheckedEntries != 1 {
		t.Fatalf("%+v", rec)
	}
}
