package postgres

import (
	"context"

	"github.com/leosanner/desafio-jungle-go/internal/app"
)

type unitOfWork struct {
	pool *Pool
}

// NewUnitOfWork returns a UnitOfWork that opens one pgx.Tx per Within call.
func NewUnitOfWork(p *Pool) app.UnitOfWork {
	return &unitOfWork{pool: p}
}

var _ app.UnitOfWork = (*unitOfWork)(nil)

// Within opens one SQL transaction, injects tx-scoped repositories, commits
// if fn returns nil, and rolls back on error or panic (then re-raises).
func (u *unitOfWork) Within(ctx context.Context, fn func(ctx context.Context, repos app.Repositories) error) error {
	tx, err := u.pool.pool.Begin(ctx)
	if err != nil {
		return mapError(err)
	}

	committed := false
	defer func() {
		if rec := recover(); rec != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			panic(rec)
		}
		if !committed {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	repos := app.Repositories{
		Wallets:      &walletRepo{q: tx},
		Transactions: &transactionRepo{q: tx},
		Ledger:       &ledgerRepo{q: tx},
	}

	if err := fn(ctx, repos); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return mapError(err)
	}
	committed = true
	return nil
}
