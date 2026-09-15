//go:build integration

package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

func TestUnitOfWorkAtomicity(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)

	t.Run("success persists wallet transaction and ledger", func(t *testing.T) {
		t.Parallel()
		opening, err := domain.OpenWallet(domain.OpenWalletParams{
			WalletID:    uniqueID(t, "wal-"),
			PlayerID:    uniqueID(t, "player-"),
			OpeningTxID: uniqueID(t, "tx-open-"),
			LedgerID:    uniqueID(t, "led-open-"),
			Initial:     mustParseMoney(t, "100.00", "BRL"),
			Now:         integrationNow,
		})
		if err != nil {
			t.Fatalf("OpenWallet: %v", err)
		}
		if err := db.uow.Within(t.Context(), persistOpeningFn(opening)); err != nil {
			t.Fatalf("Within: %v", err)
		}

		wallet, err := db.wallets().GetByID(t.Context(), opening.Wallet.ID())
		if err != nil {
			t.Fatalf("GetByID wallet: %v", err)
		}
		assertMoney(t, wallet.Balance(), "100.00", "BRL")
		if wallet.Version() != 1 {
			t.Fatalf("version = %d, want 1", wallet.Version())
		}

		tx, err := db.transactions().GetByID(t.Context(), opening.Transaction.ID())
		if err != nil {
			t.Fatalf("GetByID tx: %v", err)
		}
		if tx.Kind() != domain.KindOpening {
			t.Fatalf("kind = %s, want OPENING", tx.Kind())
		}
		if tx.Status() != domain.StatusProcessed {
			t.Fatalf("status = %s, want PROCESSED", tx.Status())
		}

		entries, err := db.ledger().ListByWallet(t.Context(), wallet.ID(), time.Time{}, "", 100)
		if err != nil {
			t.Fatalf("ListByWallet: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("ledger entries = %d, want 1", len(entries))
		}
		if entries[0].Direction() != domain.DirectionCredit {
			t.Fatalf("direction = %s, want CREDIT", entries[0].Direction())
		}
		assertMoney(t, entries[0].BalanceAfter(), "100.00", "BRL")
		assertLedgerReconcilesBalance(t, db, wallet)
	})

	t.Run("failure rolls back wallet transaction and ledger", func(t *testing.T) {
		t.Parallel()
		forced := errors.New("forced rollback before ledger")
		opening, err := domain.OpenWallet(domain.OpenWalletParams{
			WalletID:    uniqueID(t, "wal-"),
			PlayerID:    uniqueID(t, "player-"),
			OpeningTxID: uniqueID(t, "tx-open-"),
			LedgerID:    uniqueID(t, "led-open-"),
			Initial:     mustParseMoney(t, "100.00", "BRL"),
			Now:         integrationNow,
		})
		if err != nil {
			t.Fatalf("OpenWallet: %v", err)
		}

		err = db.uow.Within(t.Context(), func(ctx context.Context, repos app.Repositories) error {
			if err := repos.Wallets.Insert(ctx, opening.Wallet); err != nil {
				return err
			}
			if err := repos.Transactions.Insert(ctx, *opening.Transaction); err != nil {
				return err
			}
			return forced
		})
		if !errors.Is(err, forced) {
			t.Fatalf("Within err = %v, want %v", err, forced)
		}

		_, err = db.wallets().GetByID(t.Context(), opening.Wallet.ID())
		if !errors.Is(err, app.ErrNotFound) {
			t.Fatalf("wallet GetByID err = %v, want ErrNotFound", err)
		}
		_, err = db.transactions().GetByID(t.Context(), opening.Transaction.ID())
		if !errors.Is(err, app.ErrNotFound) {
			t.Fatalf("tx GetByID err = %v, want ErrNotFound", err)
		}
		entries, err := db.ledger().ListByWallet(t.Context(), opening.Wallet.ID(), time.Time{}, "", 100)
		if err != nil {
			t.Fatalf("ListByWallet: %v", err)
		}
		if len(entries) != 0 {
			t.Fatalf("ledger leftover = %d, want 0", len(entries))
		}
	})

	t.Run("zero opening persists wallet only", func(t *testing.T) {
		t.Parallel()
		opening, err := domain.OpenWallet(domain.OpenWalletParams{
			WalletID: uniqueID(t, "wal-"),
			PlayerID: uniqueID(t, "player-"),
			Initial:  mustParseMoney(t, "0.00", "BRL"),
			Now:      integrationNow,
		})
		if err != nil {
			t.Fatalf("OpenWallet: %v", err)
		}
		if opening.Transaction != nil || opening.Ledger != nil {
			t.Fatal("zero opening must not produce tx or ledger")
		}
		if err := db.uow.Within(t.Context(), persistOpeningFn(opening)); err != nil {
			t.Fatalf("Within: %v", err)
		}

		wallet, err := db.wallets().GetByID(t.Context(), opening.Wallet.ID())
		if err != nil {
			t.Fatalf("GetByID wallet: %v", err)
		}
		assertMoney(t, wallet.Balance(), "0.00", "BRL")
		if wallet.Version() != 1 {
			t.Fatalf("version = %d, want 1", wallet.Version())
		}
		entries, err := db.ledger().ListByWallet(t.Context(), wallet.ID(), time.Time{}, "", 100)
		if err != nil {
			t.Fatalf("ListByWallet: %v", err)
		}
		if len(entries) != 0 {
			t.Fatalf("ledger entries = %d, want 0", len(entries))
		}
		assertLedgerReconcilesBalance(t, db, wallet)
	})
}

func TestDuplicateWalletInsertConflict(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)

	playerID := uniqueID(t, "player-")
	first, err := domain.OpenWallet(domain.OpenWalletParams{
		WalletID: uniqueID(t, "wal-"),
		PlayerID: playerID,
		Initial:  mustParseMoney(t, "0.00", "BRL"),
		Now:      integrationNow,
	})
	if err != nil {
		t.Fatalf("OpenWallet first: %v", err)
	}
	second, err := domain.OpenWallet(domain.OpenWalletParams{
		WalletID: uniqueID(t, "wal-"),
		PlayerID: playerID,
		Initial:  mustParseMoney(t, "0.00", "BRL"),
		Now:      integrationNow,
	})
	if err != nil {
		t.Fatalf("OpenWallet second: %v", err)
	}

	if err := db.uow.Within(t.Context(), persistOpeningFn(first)); err != nil {
		t.Fatalf("first Insert: %v", err)
	}
	err = db.uow.Within(t.Context(), persistOpeningFn(second))
	if !errors.Is(err, app.ErrConflict) {
		t.Fatalf("second Insert err = %v, want ErrConflict", err)
	}
}

func TestMoneyRoundTripWithoutFloat(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)
	opening := persistOpening(t, db, "25.00")

	wallet, err := db.wallets().GetByID(t.Context(), opening.Wallet.ID())
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got := wallet.Balance().AmountString(); got != "25.00" {
		t.Fatalf("AmountString = %q, want %q", got, "25.00")
	}
	if got := wallet.Balance().Currency(); got != "BRL" {
		t.Fatalf("Currency = %q, want BRL", got)
	}
	assertLedgerReconcilesBalance(t, db, wallet)
}

func TestConcurrentBetsSerializePerWallet(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)
	opening := persistOpening(t, db, "100.00")
	walletID := opening.Wallet.ID()
	playerID := opening.Wallet.PlayerID()
	debit := mustParseMoney(t, "80.00", "BRL")
	provider := uniqueID(t, "prov-")

	type betIDs struct {
		txID, ledgerID, extID, key, hash string
	}
	ids := []betIDs{
		{uniqueID(t, "tx-"), uniqueID(t, "led-"), uniqueID(t, "ext-"), uniqueID(t, "key-"), uniqueID(t, "hash-")},
		{uniqueID(t, "tx-"), uniqueID(t, "led-"), uniqueID(t, "ext-"), uniqueID(t, "key-"), uniqueID(t, "hash-")},
	}

	start := make(chan struct{})
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(b betIDs) {
			defer wg.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			errCh <- db.uow.Within(ctx, func(ctx context.Context, repos app.Repositories) error {
				w, err := repos.Wallets.GetByIDForUpdate(ctx, walletID)
				if err != nil {
					return err
				}
				tx, err := domain.NewExternalTransaction(domain.ExternalTxParams{
					ID:             b.txID,
					ProviderID:     provider,
					ExternalID:     b.extID,
					IdempotencyKey: b.key,
					PayloadHash:    b.hash,
					WalletID:       walletID,
					PlayerID:       playerID,
					RoundID:        "round-1",
					GameID:         "fortune-chimp",
					Kind:           domain.KindBet,
					Money:          debit,
					Now:            integrationNow,
				})
				if err != nil {
					return err
				}

				entry, err := w.Debit(b.ledgerID, b.txID, debit, integrationNow)
				if err != nil {
					if !errors.Is(err, domain.ErrInsufficientFunds) {
						return err
					}
					if err := tx.MarkRejected(domain.FailureInsufficientFunds, integrationNow); err != nil {
						return err
					}
					return repos.Transactions.Insert(ctx, tx)
				}

				if err := tx.MarkProcessed(w.Balance(), integrationNow); err != nil {
					return err
				}
				if err := repos.Transactions.Insert(ctx, tx); err != nil {
					return err
				}
				if err := repos.Ledger.Insert(ctx, entry); err != nil {
					return err
				}
				return repos.Wallets.Update(ctx, w)
			})
		}(ids[i])
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("Within: %v", err)
		}
	}

	wallet, err := db.wallets().GetByID(t.Context(), walletID)
	if err != nil {
		t.Fatalf("GetByID wallet: %v", err)
	}
	assertMoney(t, wallet.Balance(), "20.00", "BRL")
	if wallet.Version() != 2 {
		t.Fatalf("version = %d, want 2", wallet.Version())
	}

	var processed, rejected int
	for _, b := range ids {
		tx, err := db.transactions().GetByID(t.Context(), b.txID)
		if err != nil {
			t.Fatalf("GetByID tx %s: %v", b.txID, err)
		}
		switch tx.Status() {
		case domain.StatusProcessed:
			processed++
		case domain.StatusRejected:
			rejected++
			if tx.FailureCode() != domain.FailureInsufficientFunds {
				t.Fatalf("rejected failureCode = %s, want %s", tx.FailureCode(), domain.FailureInsufficientFunds)
			}
		default:
			t.Fatalf("unexpected status %s", tx.Status())
		}
	}
	if processed != 1 || rejected != 1 {
		t.Fatalf("processed=%d rejected=%d, want 1 and 1", processed, rejected)
	}

	entries, err := db.ledger().ListByWallet(t.Context(), walletID, time.Time{}, "", 100)
	if err != nil {
		t.Fatalf("ListByWallet: %v", err)
	}
	debits := 0
	for _, e := range entries {
		if e.Direction() == domain.DirectionDebit {
			debits++
			assertMoney(t, e.Money(), "80.00", "BRL")
		}
	}
	if debits != 1 {
		t.Fatalf("ledger debits = %d, want 1", debits)
	}
	assertLedgerReconcilesBalance(t, db, wallet)
}
