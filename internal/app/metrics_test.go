package app

import (
	"context"
	"testing"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

func testServiceMetrics(t *testing.T, m Metrics) (*Service, *memStore) {
	t.Helper()
	store := newMemStore()
	svc := NewService(store, fixedClock{t: time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)}, &seqIDs{}, m)
	return svc, store
}

func TestSubmitRecordsDuplicatesAndConflicts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rec := &RecordingMetrics{}
	svc, _ := testServiceMetrics(t, rec)
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
	if _, err := svc.Submit(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	if rec.OperationCount("BET", string(domain.StatusProcessed), MetricChannelHTTP) != 1 {
		t.Fatalf("first processed = %d", rec.OperationCount("BET", string(domain.StatusProcessed), MetricChannelHTTP))
	}
	if _, err := svc.Submit(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	if rec.DuplicateCount("BET", MetricChannelHTTP) != 1 {
		t.Fatalf("duplicates = %d", rec.DuplicateCount("BET", MetricChannelHTTP))
	}

	conflict := cmd
	conflict.Money = mustParse(t, "26.00", "BRL")
	if _, err := svc.Submit(ctx, conflict); err == nil {
		t.Fatal("expected payload conflict")
	}
	if rec.ConflictCount(ConflictIdempotencyPayload) != 1 {
		t.Fatalf("payload conflicts = %d", rec.ConflictCount(ConflictIdempotencyPayload))
	}

	dup := cmd
	dup.IdempotencyKey = "other-key"
	if _, err := svc.Submit(ctx, dup); err == nil {
		t.Fatal("expected duplicate external")
	}
	if rec.ConflictCount(ConflictDuplicateExternal) != 1 {
		t.Fatalf("dup external = %d", rec.ConflictCount(ConflictDuplicateExternal))
	}
}

func TestReconcileDivergenceIncrementsMetric(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rec := &RecordingMetrics{}
	svc, store := testServiceMetrics(t, rec)
	opened, err := svc.OpenWallet(ctx, OpenWalletCommand{
		PlayerID:       "player-1",
		InitialBalance: mustParse(t, "50.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	w := store.wallets[opened.Wallet.ID()]
	wrong, err := domain.RehydrateWallet(w.ID(), w.PlayerID(), mustParse(t, "99.00", "BRL"), w.Version(), w.CreatedAt(), w.UpdatedAt())
	if err != nil {
		t.Fatal(err)
	}
	store.wallets[w.ID()] = wrong
	out, err := svc.ReconcileWallet(ctx, w.ID())
	if err != nil {
		t.Fatal(err)
	}
	if out.Consistent {
		t.Fatal("expected divergence")
	}
	if rec.ReconciliationDivergences != 1 {
		t.Fatalf("divergences = %d", rec.ReconciliationDivergences)
	}
}
