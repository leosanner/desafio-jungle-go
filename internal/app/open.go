package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

// OpenWalletCommand is the internal opening request.
type OpenWalletCommand struct {
	PlayerID       string
	InitialBalance domain.Money
	CorrelationID  string
}

// OpenWalletResult is the created wallet.
type OpenWalletResult struct {
	Wallet domain.Wallet
}

// OpenWallet creates a wallet. Duplicate (playerId, currency) is ErrConflict.
func (s *Service) OpenWallet(ctx context.Context, cmd OpenWalletCommand) (OpenWalletResult, error) {
	opening, err := domain.OpenWallet(domain.OpenWalletParams{
		WalletID:    s.ids.NewID(),
		PlayerID:    cmd.PlayerID,
		OpeningTxID: s.ids.NewID(),
		LedgerID:    s.ids.NewID(),
		Initial:     cmd.InitialBalance,
		Now:         s.clock.Now(),
	})
	if err != nil {
		return OpenWalletResult{}, err
	}

	causation := ""
	if opening.Transaction != nil {
		causation = opening.Transaction.ID()
	}
	recs, err := RecordsFromEvents(opening.Events, s.ids, cmd.CorrelationID, causation)
	if err != nil {
		return OpenWalletResult{}, err
	}

	err = s.uow.Within(ctx, func(ctx context.Context, repos Repositories) error {
		if err := repos.Wallets.Insert(ctx, opening.Wallet); err != nil {
			return err
		}
		if opening.Transaction != nil {
			if err := repos.Transactions.Insert(ctx, *opening.Transaction); err != nil {
				return err
			}
		}
		if opening.Ledger != nil {
			if err := repos.Ledger.Insert(ctx, *opening.Ledger); err != nil {
				return err
			}
		}
		return persistOutbox(ctx, repos, recs)
	})
	if err != nil {
		if errors.Is(err, ErrConflict) {
			return OpenWalletResult{}, fmt.Errorf("open wallet: %w", ErrConflict)
		}
		return OpenWalletResult{}, err
	}
	return OpenWalletResult{Wallet: opening.Wallet}, nil
}
