package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

const walletColumns = `id, player_id, currency, balance_minor, version, created_at, updated_at`

type walletRepo struct {
	q querier
}

var _ app.WalletRepository = (*walletRepo)(nil)

func (r *walletRepo) GetByID(ctx context.Context, id string) (domain.Wallet, error) {
	const q = `SELECT ` + walletColumns + ` FROM wagering.wallets WHERE id = $1`
	return scanWallet(r.q.QueryRow(ctx, q, id))
}

func (r *walletRepo) GetByIDForUpdate(ctx context.Context, id string) (domain.Wallet, error) {
	const q = `SELECT ` + walletColumns + ` FROM wagering.wallets WHERE id = $1 FOR UPDATE`
	return scanWallet(r.q.QueryRow(ctx, q, id))
}

func (r *walletRepo) GetByPlayerAndCurrency(ctx context.Context, playerID, currency string) (domain.Wallet, error) {
	const q = `SELECT ` + walletColumns + ` FROM wagering.wallets WHERE player_id = $1 AND currency = $2`
	return scanWallet(r.q.QueryRow(ctx, q, playerID, currency))
}

func (r *walletRepo) Insert(ctx context.Context, w domain.Wallet) error {
	const q = `
		INSERT INTO wagering.wallets (id, player_id, currency, balance_minor, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := r.q.Exec(ctx, q,
		w.ID(),
		w.PlayerID(),
		w.Currency(),
		w.Balance().Minor(),
		w.Version(),
		w.CreatedAt(),
		w.UpdatedAt(),
	)
	if err != nil {
		return mapError(err)
	}
	return nil
}

func (r *walletRepo) Update(ctx context.Context, w domain.Wallet) error {
	// $2 is the in-memory version after Debit/Credit; $5 is the persisted
	// version before this mutation. FOR UPDATE is the caller's job, not ours.
	const q = `
		UPDATE wagering.wallets
		SET balance_minor = $1, version = $2, updated_at = $3
		WHERE id = $4 AND version = $5`
	tag, err := r.q.Exec(ctx, q,
		w.Balance().Minor(),
		w.Version(),
		w.UpdatedAt(),
		w.ID(),
		w.Version()-1,
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("postgres: %w", app.ErrOptimisticLock)
	}
	return nil
}

func scanWallet(row scanner) (domain.Wallet, error) {
	var (
		id, playerID, currency string
		balanceMinor, version  int64
		createdAt, updatedAt   time.Time
	)
	if err := row.Scan(&id, &playerID, &currency, &balanceMinor, &version, &createdAt, &updatedAt); err != nil {
		return domain.Wallet{}, mapError(err)
	}
	bal, err := moneyFromMinorScan(balanceMinor, currency)
	if err != nil {
		return domain.Wallet{}, err
	}
	w, err := domain.RehydrateWallet(id, playerID, bal, version, createdAt, updatedAt)
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("postgres: rehydrate wallet: %w", err)
	}
	return w, nil
}
