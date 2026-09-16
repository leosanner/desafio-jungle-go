package app

import (
	"context"

	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

// Service orchestrates wallet and wagering use cases. It does not import net/http or Fx.
type Service struct {
	uow     UnitOfWork
	clock   Clock
	ids     IDGenerator
	metrics Metrics
}

// NewService constructs the application service.
func NewService(uow UnitOfWork, clock Clock, ids IDGenerator, metrics Metrics) *Service {
	if metrics == nil {
		metrics = NopMetrics{}
	}
	return &Service{uow: uow, clock: clock, ids: ids, metrics: metrics}
}

func persistOutbox(ctx context.Context, repos Repositories, recs []OutboxRecord) error {
	for _, rec := range recs {
		if err := repos.Outbox.Insert(ctx, rec); err != nil {
			return err
		}
	}
	return nil
}

func persistApply(ctx context.Context, repos Repositories, loaded domain.Wallet, res domain.ApplyResult, recs []OutboxRecord, existing bool) error {
	if existing {
		if err := repos.Transactions.Update(ctx, res.Operation); err != nil {
			return err
		}
	} else if err := repos.Transactions.Insert(ctx, res.Operation); err != nil {
		return err
	}
	if res.Ledger != nil {
		if err := repos.Ledger.Insert(ctx, *res.Ledger); err != nil {
			return err
		}
	}
	if res.Wallet.Version() != loaded.Version() {
		if err := repos.Wallets.Update(ctx, res.Wallet); err != nil {
			return err
		}
	}
	return persistOutbox(ctx, repos, recs)
}
