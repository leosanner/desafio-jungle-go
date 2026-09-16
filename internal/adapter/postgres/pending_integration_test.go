//go:build integration

package postgres

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

func pendingActor() app.Actor {
	return app.Actor{ClientID: "provider-a", ProviderID: "provider-a"}
}

func testPendingResumer(svc *app.Service, db *testDB, maxAttempts int, ttl time.Duration) *app.PendingResumer {
	return app.NewPendingResumer(svc, NewPendingClaimer(db.pool), app.ResumeParams{
		Batch:       10,
		Lease:       30 * time.Second,
		BackoffMax:  time.Minute,
		MaxAttempts: maxAttempts,
		TTL:         ttl,
	})
}

func txStatus(t *testing.T, db *testDB, id string) (domain.Status, domain.FailureCode) {
	t.Helper()
	tx, err := db.transactions().GetByID(t.Context(), id)
	if err != nil {
		t.Fatalf("get tx: %v", err)
	}
	return tx.Status(), tx.FailureCode()
}

func walletBalance(t *testing.T, db *testDB, id string) string {
	t.Helper()
	w, err := db.wallets().GetByID(t.Context(), id)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	return w.Balance().AmountString()
}

func TestPendingClaimSkipLocked(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)
	svc := app.NewService(db.uow, app.SystemClock{}, app.UUIDGenerator{}, app.NopMetrics{})
	opened, err := svc.OpenWallet(t.Context(), app.OpenWalletCommand{
		PlayerID:       uniqueID(t, "player-"),
		InitialBalance: mustParseMoney(t, "1000.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}

	const n = 8
	ids := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		ext := uniqueID(t, "refund-")
		out, err := svc.Submit(t.Context(), app.SubmitCommand{
			Actor:               pendingActor(),
			IdempotencyKey:      "provider-a:" + ext,
			ProviderID:          "provider-a",
			ExternalID:          ext,
			PlayerID:            opened.Wallet.PlayerID(),
			WalletID:            opened.Wallet.ID(),
			RoundID:             "round-1",
			GameID:              "game-1",
			Kind:                domain.KindRefund,
			Money:               mustParseMoney(t, "10.00", "BRL"),
			ReferenceExternalID: uniqueID(t, "bet-"),
		})
		if err != nil {
			t.Fatal(err)
		}
		ids[out.Transaction.ID()] = struct{}{}
	}

	claimer := NewPendingClaimer(db.pool)
	now := time.Now().UTC()
	got := make(chan []app.PendingWork, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			items, err := claimer.Claim(t.Context(), n, now, 30*time.Second)
			if err != nil {
				t.Errorf("Claim: %v", err)
				got <- nil
				return
			}
			got <- items
		}()
	}
	close(start)
	wg.Wait()
	close(got)

	seen := map[string]int{}
	total := 0
	for items := range got {
		for _, item := range items {
			seen[item.TransactionID]++
			total++
			if item.Attempts != 1 {
				t.Errorf("attempts = %d, want 1", item.Attempts)
			}
		}
	}
	if total != n {
		t.Fatalf("claimed %d, want %d", total, n)
	}
	for id := range ids {
		if seen[id] != 1 {
			t.Errorf("tx %s claimed %d times", id, seen[id])
		}
	}
}

func TestPendingRefundResolvesWhenBetArrives(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)
	svc := app.NewService(db.uow, app.SystemClock{}, app.UUIDGenerator{}, app.NopMetrics{})
	opened, err := svc.OpenWallet(t.Context(), app.OpenWalletCommand{
		PlayerID:       uniqueID(t, "player-"),
		InitialBalance: mustParseMoney(t, "100.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	betExt := uniqueID(t, "bet-")
	refund, err := svc.Submit(t.Context(), app.SubmitCommand{
		Actor:               pendingActor(),
		IdempotencyKey:      "provider-a:refund-" + betExt,
		ProviderID:          "provider-a",
		ExternalID:          "refund-" + betExt,
		PlayerID:            opened.Wallet.PlayerID(),
		WalletID:            opened.Wallet.ID(),
		RoundID:             "round-1",
		GameID:              "game-1",
		Kind:                domain.KindRefund,
		Money:               mustParseMoney(t, "25.00", "BRL"),
		ReferenceExternalID: betExt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if refund.Transaction.Status() != domain.StatusPendingReference {
		t.Fatalf("status = %s", refund.Transaction.Status())
	}

	if _, err := svc.Submit(t.Context(), app.SubmitCommand{
		Actor:          pendingActor(),
		IdempotencyKey: "provider-a:" + betExt,
		ProviderID:     "provider-a",
		ExternalID:     betExt,
		PlayerID:       opened.Wallet.PlayerID(),
		WalletID:       opened.Wallet.ID(),
		RoundID:        "round-1",
		GameID:         "game-1",
		Kind:           domain.KindBet,
		Money:          mustParseMoney(t, "25.00", "BRL"),
	}); err != nil {
		t.Fatal(err)
	}

	resumer := testPendingResumer(svc, db, 8, time.Hour)
	res, err := resumer.ResumeDue(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if res.Processed != 1 {
		t.Fatalf("resume=%+v", res)
	}
	st, _ := txStatus(t, db, refund.Transaction.ID())
	if st != domain.StatusProcessed {
		t.Fatalf("refund status = %s", st)
	}
	if walletBalance(t, db, opened.Wallet.ID()) != "100.00" {
		t.Fatalf("balance = %s", walletBalance(t, db, opened.Wallet.ID()))
	}
	if countLedger(t, db, opened.Wallet.ID()) != 3 {
		t.Fatalf("ledger = %d, want 3", countLedger(t, db, opened.Wallet.ID()))
	}

	replay, err := svc.Submit(t.Context(), app.SubmitCommand{
		Actor:               pendingActor(),
		IdempotencyKey:      "provider-a:refund-" + betExt,
		ProviderID:          "provider-a",
		ExternalID:          "refund-" + betExt,
		PlayerID:            opened.Wallet.PlayerID(),
		WalletID:            opened.Wallet.ID(),
		RoundID:             "round-1",
		GameID:              "game-1",
		Kind:                domain.KindRefund,
		Money:               mustParseMoney(t, "25.00", "BRL"),
		ReferenceExternalID: betExt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !replay.IdempotentReplay {
		t.Fatal("expected replay after resolution")
	}
}

func TestPendingRefundExpiresReferenceNotFound(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)
	svc := app.NewService(db.uow, app.SystemClock{}, app.UUIDGenerator{}, app.NopMetrics{})
	opened, err := svc.OpenWallet(t.Context(), app.OpenWalletCommand{
		PlayerID:       uniqueID(t, "player-"),
		InitialBalance: mustParseMoney(t, "100.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	refund, err := svc.Submit(t.Context(), app.SubmitCommand{
		Actor:               pendingActor(),
		IdempotencyKey:      uniqueID(t, "key-"),
		ProviderID:          "provider-a",
		ExternalID:          uniqueID(t, "refund-"),
		PlayerID:            opened.Wallet.PlayerID(),
		WalletID:            opened.Wallet.ID(),
		RoundID:             "round-1",
		GameID:              "game-1",
		Kind:                domain.KindRefund,
		Money:               mustParseMoney(t, "25.00", "BRL"),
		ReferenceExternalID: uniqueID(t, "bet-"),
	})
	if err != nil {
		t.Fatal(err)
	}

	resumer := testPendingResumer(svc, db, 1, time.Hour)
	res, err := resumer.ResumeDue(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if res.Rejected != 1 {
		t.Fatalf("resume=%+v", res)
	}
	st, code := txStatus(t, db, refund.Transaction.ID())
	if st != domain.StatusRejected || code != domain.FailureReferenceNotFound {
		t.Fatalf("status=%s code=%s", st, code)
	}
	if walletBalance(t, db, opened.Wallet.ID()) != "100.00" {
		t.Fatal("expiry must not move the balance")
	}
}

func TestPendingCommittedPendingResumed(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)
	svc := app.NewService(db.uow, app.SystemClock{}, app.UUIDGenerator{}, app.NopMetrics{})
	opened, err := svc.OpenWallet(t.Context(), app.OpenWalletCommand{
		PlayerID:       uniqueID(t, "player-"),
		InitialBalance: mustParseMoney(t, "100.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}

	extID := uniqueID(t, "bet-")
	key := "provider-a:" + extID
	money := mustParseMoney(t, "25.00", "BRL")
	hash, err := domain.HashCanonicalPayload(domain.CanonicalPayload{
		ProviderID: "provider-a",
		ExternalID: extID,
		PlayerID:   opened.Wallet.PlayerID(),
		WalletID:   opened.Wallet.ID(),
		RoundID:    "round-1",
		GameID:     "game-1",
		Kind:       domain.KindBet,
		Money:      money,
	})
	if err != nil {
		t.Fatal(err)
	}
	op, err := domain.NewExternalTransaction(domain.ExternalTxParams{
		ID:             uniqueID(t, "tx-"),
		ProviderID:     "provider-a",
		ExternalID:     extID,
		IdempotencyKey: key,
		PayloadHash:    hash,
		WalletID:       opened.Wallet.ID(),
		PlayerID:       opened.Wallet.PlayerID(),
		RoundID:        "round-1",
		GameID:         "game-1",
		Kind:           domain.KindBet,
		Money:          money,
		Now:            time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.uow.Within(t.Context(), func(ctx context.Context, repos app.Repositories) error {
		return repos.Transactions.Insert(ctx, op)
	}); err != nil {
		t.Fatal(err)
	}

	// New Service stands in for another instance after restart (TST-15).
	other := app.NewService(db.uow, app.SystemClock{}, app.UUIDGenerator{}, app.NopMetrics{})
	resumer := testPendingResumer(other, db, 8, time.Hour)
	res, err := resumer.ResumeDue(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if res.Processed != 1 {
		t.Fatalf("resume=%+v", res)
	}
	st, _ := txStatus(t, db, op.ID())
	if st != domain.StatusProcessed {
		t.Fatalf("status = %s", st)
	}
	if walletBalance(t, db, opened.Wallet.ID()) != "75.00" {
		t.Fatalf("balance = %s", walletBalance(t, db, opened.Wallet.ID()))
	}

	replay, err := other.Submit(t.Context(), app.SubmitCommand{
		Actor:          pendingActor(),
		IdempotencyKey: key,
		ProviderID:     "provider-a",
		ExternalID:     extID,
		PlayerID:       opened.Wallet.PlayerID(),
		WalletID:       opened.Wallet.ID(),
		RoundID:        "round-1",
		GameID:         "game-1",
		Kind:           domain.KindBet,
		Money:          money,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !replay.IdempotentReplay {
		t.Fatal("idempotency lost after PENDING resume")
	}
	if countLedger(t, db, opened.Wallet.ID()) != 2 {
		t.Fatalf("ledger = %d, want 2", countLedger(t, db, opened.Wallet.ID()))
	}
}

func TestPendingTwoResumersOneRefund(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)
	svc := app.NewService(db.uow, app.SystemClock{}, app.UUIDGenerator{}, app.NopMetrics{})
	opened, err := svc.OpenWallet(t.Context(), app.OpenWalletCommand{
		PlayerID:       uniqueID(t, "player-"),
		InitialBalance: mustParseMoney(t, "100.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	betExt := uniqueID(t, "bet-")
	refund, err := svc.Submit(t.Context(), app.SubmitCommand{
		Actor:               pendingActor(),
		IdempotencyKey:      "provider-a:refund-" + betExt,
		ProviderID:          "provider-a",
		ExternalID:          "refund-" + betExt,
		PlayerID:            opened.Wallet.PlayerID(),
		WalletID:            opened.Wallet.ID(),
		RoundID:             "round-1",
		GameID:              "game-1",
		Kind:                domain.KindRefund,
		Money:               mustParseMoney(t, "25.00", "BRL"),
		ReferenceExternalID: betExt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Submit(t.Context(), app.SubmitCommand{
		Actor:          pendingActor(),
		IdempotencyKey: "provider-a:" + betExt,
		ProviderID:     "provider-a",
		ExternalID:     betExt,
		PlayerID:       opened.Wallet.PlayerID(),
		WalletID:       opened.Wallet.ID(),
		RoundID:        "round-1",
		GameID:         "game-1",
		Kind:           domain.KindBet,
		Money:          mustParseMoney(t, "25.00", "BRL"),
	}); err != nil {
		t.Fatal(err)
	}

	a := testPendingResumer(svc, db, 8, time.Hour)
	b := testPendingResumer(svc, db, 8, time.Hour)
	start := make(chan struct{})
	results := make(chan app.ResumeResult, 2)
	var wg sync.WaitGroup
	for _, r := range []*app.PendingResumer{a, b} {
		wg.Add(1)
		go func(resumer *app.PendingResumer) {
			defer wg.Done()
			<-start
			res, err := resumer.ResumeDue(t.Context())
			if err != nil {
				t.Errorf("ResumeDue: %v", err)
			}
			results <- res
		}(r)
	}
	close(start)
	wg.Wait()
	close(results)

	var processed, skipped int
	for res := range results {
		processed += res.Processed
		skipped += res.Skipped
	}
	if processed != 1 {
		t.Fatalf("processed=%d skipped=%d", processed, skipped)
	}
	st, _ := txStatus(t, db, refund.Transaction.ID())
	if st != domain.StatusProcessed {
		t.Fatalf("status = %s", st)
	}
	if walletBalance(t, db, opened.Wallet.ID()) != "100.00" {
		t.Fatalf("balance = %s", walletBalance(t, db, opened.Wallet.ID()))
	}
	if countLedger(t, db, opened.Wallet.ID()) != 3 {
		t.Fatalf("ledger = %d", countLedger(t, db, opened.Wallet.ID()))
	}
}

func TestPendingLeaseHidesRow(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)
	svc := app.NewService(db.uow, app.SystemClock{}, app.UUIDGenerator{}, app.NopMetrics{})
	opened, err := svc.OpenWallet(t.Context(), app.OpenWalletCommand{
		PlayerID:       uniqueID(t, "player-"),
		InitialBalance: mustParseMoney(t, "100.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Submit(t.Context(), app.SubmitCommand{
		Actor:               pendingActor(),
		IdempotencyKey:      uniqueID(t, "key-"),
		ProviderID:          "provider-a",
		ExternalID:          uniqueID(t, "refund-"),
		PlayerID:            opened.Wallet.PlayerID(),
		WalletID:            opened.Wallet.ID(),
		RoundID:             "round-1",
		GameID:              "game-1",
		Kind:                domain.KindRefund,
		Money:               mustParseMoney(t, "25.00", "BRL"),
		ReferenceExternalID: uniqueID(t, "bet-"),
	}); err != nil {
		t.Fatal(err)
	}

	claimer := NewPendingClaimer(db.pool)
	now := time.Now().UTC()
	first, err := claimer.Claim(t.Context(), 1, now, time.Minute)
	if err != nil || len(first) != 1 {
		t.Fatalf("claim: %v n=%d", err, len(first))
	}
	again, err := claimer.Claim(t.Context(), 1, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("claimed leased row: %+v", again)
	}
}
