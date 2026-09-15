package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

const (
	ledgerColumns      = `id, wallet_id, transaction_id, direction, amount_minor, currency, balance_before_minor, balance_after_minor, created_at`
	maxLedgerListLimit = 100
)

type ledgerRepo struct {
	q querier
}

var _ app.LedgerRepository = (*ledgerRepo)(nil)

func (r *ledgerRepo) Insert(ctx context.Context, e domain.WalletLedgerEntry) error {
	const q = `
		INSERT INTO wagering.wallet_ledger_entries (
			id, wallet_id, transaction_id, direction, amount_minor, currency,
			balance_before_minor, balance_after_minor, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := r.q.Exec(ctx, q,
		e.ID(),
		e.WalletID(),
		e.TransactionID(),
		string(e.Direction()),
		e.Money().Minor(),
		e.Money().Currency(),
		e.BalanceBefore().Minor(),
		e.BalanceAfter().Minor(),
		e.CreatedAt(),
	)
	if err != nil {
		return mapError(err)
	}
	return nil
}

func (r *ledgerRepo) ListByWallet(ctx context.Context, walletID string, createdAt time.Time, id string, limit int) ([]domain.WalletLedgerEntry, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("postgres: ledger list: limit must be greater than 0")
	}
	if limit > maxLedgerListLimit {
		limit = maxLedgerListLimit
	}

	query := `SELECT ` + ledgerColumns + `
		FROM wagering.wallet_ledger_entries
		WHERE wallet_id = $1`
	var args []any
	args = append(args, walletID)
	if id != "" {
		query += ` AND (created_at, id) > ($2, $3)`
		args = append(args, createdAt, id)
		query += ` ORDER BY created_at ASC, id ASC LIMIT $4`
		args = append(args, limit)
	} else {
		query += ` ORDER BY created_at ASC, id ASC LIMIT $2`
		args = append(args, limit)
	}

	rs, err := r.q.Query(ctx, query, args...)
	if err != nil {
		return nil, mapError(err)
	}
	defer rs.Close()

	out := make([]domain.WalletLedgerEntry, 0, limit)
	for rs.Next() {
		e, err := scanLedger(rs)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rs.Err(); err != nil {
		return nil, mapError(err)
	}
	return out, nil
}

func scanLedger(row scanner) (domain.WalletLedgerEntry, error) {
	var (
		id, walletID, transactionID, direction, currency string
		amountMinor, beforeMinor, afterMinor             int64
		createdAt                                        time.Time
	)
	if err := row.Scan(
		&id,
		&walletID,
		&transactionID,
		&direction,
		&amountMinor,
		&currency,
		&beforeMinor,
		&afterMinor,
		&createdAt,
	); err != nil {
		return domain.WalletLedgerEntry{}, mapError(err)
	}
	money, err := moneyFromMinorScan(amountMinor, currency)
	if err != nil {
		return domain.WalletLedgerEntry{}, err
	}
	before, err := moneyFromMinorScan(beforeMinor, currency)
	if err != nil {
		return domain.WalletLedgerEntry{}, err
	}
	after, err := moneyFromMinorScan(afterMinor, currency)
	if err != nil {
		return domain.WalletLedgerEntry{}, err
	}
	e, err := domain.RehydrateLedgerEntry(
		id, walletID, transactionID,
		domain.Direction(direction),
		money, before, after,
		createdAt,
	)
	if err != nil {
		return domain.WalletLedgerEntry{}, fmt.Errorf("postgres: rehydrate ledger: %w", err)
	}
	return e, nil
}
