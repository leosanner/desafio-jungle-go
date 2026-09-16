package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

type fakePendingClaimer struct {
	mu       sync.Mutex
	due      []PendingWork
	retries  []retryCall
	claimErr error
}

func (f *fakePendingClaimer) Claim(_ context.Context, limit int, _ time.Time, _ time.Duration) ([]PendingWork, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	if limit > len(f.due) {
		limit = len(f.due)
	}
	out := append([]PendingWork(nil), f.due[:limit]...)
	f.due = f.due[limit:]
	for i := range out {
		out[i].Attempts++
	}
	return out, nil
}

func (f *fakePendingClaimer) ScheduleRetry(_ context.Context, transactionID string, next time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retries = append(f.retries, retryCall{eventID: transactionID, next: next})
	return nil
}

func (f *fakePendingClaimer) seed(work PendingWork) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.due = append(f.due, work)
}

type mutableClock struct{ t time.Time }

func (c *mutableClock) Now() time.Time { return c.t }

func pendingActor() Actor {
	return Actor{ClientID: "provider-a", ProviderID: "provider-a"}
}

func testResumer(svc *Service, claimer PendingClaimer, maxAttempts int, ttl time.Duration) *PendingResumer {
	return NewPendingResumer(svc, claimer, ResumeParams{
		Batch:       10,
		Lease:       30 * time.Second,
		BackoffMax:  time.Minute,
		MaxAttempts: maxAttempts,
		TTL:         ttl,
	})
}

func workFrom(tx domain.WagerTransaction) PendingWork {
	return PendingWork{
		TransactionID: tx.ID(),
		WalletID:      tx.WalletID(),
		CreatedAt:     tx.CreatedAt(),
		Status:        tx.Status(),
	}
}

func TestResumeDueResolvesWhenReferenceArrives(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, store := testService(t)
	opened, err := svc.OpenWallet(ctx, OpenWalletCommand{
		PlayerID:       "player-1",
		InitialBalance: mustParse(t, "100.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	refund, err := svc.Submit(ctx, SubmitCommand{
		Actor:               pendingActor(),
		IdempotencyKey:      "provider-a:refund-1",
		ProviderID:          "provider-a",
		ExternalID:          "refund-1",
		PlayerID:            "player-1",
		WalletID:            opened.Wallet.ID(),
		RoundID:             "round-1",
		GameID:              "game-1",
		Kind:                domain.KindRefund,
		Money:               mustParse(t, "25.00", "BRL"),
		ReferenceExternalID: "bet-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if refund.Transaction.Status() != domain.StatusPendingReference {
		t.Fatalf("status = %s", refund.Transaction.Status())
	}

	claimer := &fakePendingClaimer{}
	resumer := testResumer(svc, claimer, 8, time.Hour)
	claimer.seed(workFrom(refund.Transaction))
	first, err := resumer.ResumeDue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first.Waiting != 1 || first.Processed != 0 {
		t.Fatalf("before bet: %+v", first)
	}
	if len(claimer.retries) != 1 {
		t.Fatalf("retries = %d", len(claimer.retries))
	}

	if _, err := svc.Submit(ctx, SubmitCommand{
		Actor:          pendingActor(),
		IdempotencyKey: "provider-a:bet-1",
		ProviderID:     "provider-a",
		ExternalID:     "bet-1",
		PlayerID:       "player-1",
		WalletID:       opened.Wallet.ID(),
		RoundID:        "round-1",
		GameID:         "game-1",
		Kind:           domain.KindBet,
		Money:          mustParse(t, "25.00", "BRL"),
	}); err != nil {
		t.Fatal(err)
	}

	claimer.seed(workFrom(refund.Transaction))
	second, err := resumer.ResumeDue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.Processed != 1 {
		t.Fatalf("after bet: %+v", second)
	}

	got, err := svc.GetTransaction(ctx, pendingActor(), refund.Transaction.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status() != domain.StatusProcessed {
		t.Fatalf("refund status = %s", got.Status())
	}
	store.mu.Lock()
	bal := store.wallets[opened.Wallet.ID()].Balance().AmountString()
	ledgerN := 0
	for _, e := range store.ledger {
		if e.WalletID() == opened.Wallet.ID() {
			ledgerN++
		}
	}
	store.mu.Unlock()
	if bal != "100.00" {
		t.Fatalf("balance = %s, want 100.00", bal)
	}
	if ledgerN != 3 {
		t.Fatalf("ledger = %d, want 3 (opening, bet, refund)", ledgerN)
	}

	replay, err := svc.Submit(ctx, SubmitCommand{
		Actor:               pendingActor(),
		IdempotencyKey:      "provider-a:refund-1",
		ProviderID:          "provider-a",
		ExternalID:          "refund-1",
		PlayerID:            "player-1",
		WalletID:            opened.Wallet.ID(),
		RoundID:             "round-1",
		GameID:              "game-1",
		Kind:                domain.KindRefund,
		Money:               mustParse(t, "25.00", "BRL"),
		ReferenceExternalID: "bet-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !replay.IdempotentReplay || replay.Transaction.Status() != domain.StatusProcessed {
		t.Fatalf("replay = %+v", replay)
	}
}

func TestResumeDueExpiresReferenceNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, store := testService(t)
	opened, err := svc.OpenWallet(ctx, OpenWalletCommand{
		PlayerID:       "player-1",
		InitialBalance: mustParse(t, "100.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	refund, err := svc.Submit(ctx, SubmitCommand{
		Actor:               pendingActor(),
		IdempotencyKey:      "provider-a:refund-exp",
		ProviderID:          "provider-a",
		ExternalID:          "refund-exp",
		PlayerID:            "player-1",
		WalletID:            opened.Wallet.ID(),
		RoundID:             "round-1",
		GameID:              "game-1",
		Kind:                domain.KindRefund,
		Money:               mustParse(t, "25.00", "BRL"),
		ReferenceExternalID: "bet-missing",
	})
	if err != nil {
		t.Fatal(err)
	}

	claimer := &fakePendingClaimer{}
	resumer := testResumer(svc, claimer, 1, time.Hour)
	claimer.seed(workFrom(refund.Transaction))
	res, err := resumer.ResumeDue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Rejected != 1 {
		t.Fatalf("res=%+v", res)
	}

	store.mu.Lock()
	got := store.txs[refund.Transaction.ID()]
	store.mu.Unlock()
	if got.Status() != domain.StatusRejected || got.FailureCode() != domain.FailureReferenceNotFound {
		t.Fatalf("status=%s code=%s", got.Status(), got.FailureCode())
	}
	store.mu.Lock()
	foundRejected := false
	for _, rec := range store.outbox {
		if rec.EventType == domain.EventTypeWagerTransactionRejected && rec.AggregateID == refund.Transaction.ID() {
			foundRejected = true
		}
	}
	store.mu.Unlock()
	if !foundRejected {
		t.Fatal("missing WagerTransactionRejected outbox row")
	}
}

func TestResumeDueTTLExpires(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clk := &mutableClock{t: time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)}
	store := newMemStore()
	svc := NewService(store, clk, &seqIDs{}, NopMetrics{})
	opened, err := svc.OpenWallet(ctx, OpenWalletCommand{
		PlayerID:       "player-1",
		InitialBalance: mustParse(t, "100.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	refund, err := svc.Submit(ctx, SubmitCommand{
		Actor:               pendingActor(),
		IdempotencyKey:      "provider-a:refund-ttl",
		ProviderID:          "provider-a",
		ExternalID:          "refund-ttl",
		PlayerID:            "player-1",
		WalletID:            opened.Wallet.ID(),
		RoundID:             "round-1",
		GameID:              "game-1",
		Kind:                domain.KindRefund,
		Money:               mustParse(t, "25.00", "BRL"),
		ReferenceExternalID: "bet-ttl",
	})
	if err != nil {
		t.Fatal(err)
	}
	clk.t = clk.t.Add(2 * time.Hour)
	claimer := &fakePendingClaimer{}
	resumer := testResumer(svc, claimer, 100, time.Hour)
	claimer.seed(workFrom(refund.Transaction))
	res, err := resumer.ResumeDue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Rejected != 1 {
		t.Fatalf("res=%+v", res)
	}
	store.mu.Lock()
	got := store.txs[refund.Transaction.ID()]
	store.mu.Unlock()
	if got.FailureCode() != domain.FailureReferenceNotFound {
		t.Fatalf("code=%s", got.FailureCode())
	}
}

func TestResumeDueCommittedPending(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, store := testService(t)
	opened, err := svc.OpenWallet(ctx, OpenWalletCommand{
		PlayerID:       "player-1",
		InitialBalance: mustParse(t, "100.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	op, err := domain.NewExternalTransaction(domain.ExternalTxParams{
		ID:             "tx-pending-bet",
		ProviderID:     "provider-a",
		ExternalID:     "bet-pending",
		IdempotencyKey: "provider-a:bet-pending",
		PayloadHash:    "hash-pending",
		WalletID:       opened.Wallet.ID(),
		PlayerID:       "player-1",
		RoundID:        "round-1",
		GameID:         "game-1",
		Kind:           domain.KindBet,
		Money:          mustParse(t, "25.00", "BRL"),
		Now:            now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Within(ctx, func(ctx context.Context, repos Repositories) error {
		return repos.Transactions.Insert(ctx, op)
	}); err != nil {
		t.Fatal(err)
	}

	claimer := &fakePendingClaimer{}
	resumer := testResumer(svc, claimer, 8, time.Hour)
	claimer.seed(workFrom(op))
	res, err := resumer.ResumeDue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Processed != 1 {
		t.Fatalf("res=%+v", res)
	}
	store.mu.Lock()
	got := store.txs[op.ID()]
	bal := store.wallets[opened.Wallet.ID()].Balance().AmountString()
	store.mu.Unlock()
	if got.Status() != domain.StatusProcessed {
		t.Fatalf("status=%s", got.Status())
	}
	if bal != "75.00" {
		t.Fatalf("balance=%s", bal)
	}
}

func TestResumeDueClaimError(t *testing.T) {
	t.Parallel()
	svc, _ := testService(t)
	claimer := &fakePendingClaimer{claimErr: errors.New("db down")}
	_, err := testResumer(svc, claimer, 8, time.Hour).ResumeDue(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResumeDueSkipsTerminal(t *testing.T) {
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
	bet, err := svc.Submit(ctx, SubmitCommand{
		Actor:          pendingActor(),
		IdempotencyKey: "provider-a:bet-done",
		ProviderID:     "provider-a",
		ExternalID:     "bet-done",
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
	claimer := &fakePendingClaimer{}
	resumer := testResumer(svc, claimer, 8, time.Hour)
	claimer.seed(workFrom(bet.Transaction))
	res, err := resumer.ResumeDue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 1 {
		t.Fatalf("res=%+v", res)
	}
}
