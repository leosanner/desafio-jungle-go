package app

import (
	"context"

	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

// Reconciliation compares stored balance with Σcredits − Σdebits.
type Reconciliation struct {
	WalletID          string
	StoredBalance     domain.Money
	CalculatedBalance domain.Money
	Difference        domain.Money
	Consistent        bool
	CheckedEntries    int
}

// ReconcileWallet rebuilds the balance from the ledger without changing it.
func (s *Service) ReconcileWallet(ctx context.Context, walletID string) (Reconciliation, error) {
	var rec Reconciliation
	err := s.uow.Within(ctx, func(ctx context.Context, repos Repositories) error {
		w, err := repos.Wallets.GetByIDForUpdate(ctx, walletID)
		if err != nil {
			return err
		}
		sum, n, err := repos.Ledger.SumByWallet(ctx, walletID, w.Currency())
		if err != nil {
			return err
		}
		diff, err := w.Balance().Sub(sum)
		if err != nil {
			return err
		}
		eq, err := w.Balance().Equal(sum)
		if err != nil {
			return err
		}
		rec = Reconciliation{
			WalletID:          w.ID(),
			StoredBalance:     w.Balance(),
			CalculatedBalance: sum,
			Difference:        diff,
			Consistent:        eq,
			CheckedEntries:    n,
		}
		return nil
	})
	if err != nil {
		return Reconciliation{}, err
	}
	if !rec.Consistent {
		s.metrics.IncReconciliationDivergence()
	}
	return rec, nil
}
