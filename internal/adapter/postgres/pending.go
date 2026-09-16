package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

// PendingClaimer claims due PENDING / PENDING_REFERENCE rows in its own short
// transaction (ADR 0019).
type PendingClaimer struct {
	pool *pgxpool.Pool
}

var _ app.PendingClaimer = (*PendingClaimer)(nil)

// NewPendingClaimer uses the process pool, not the financial unit of work.
func NewPendingClaimer(p *Pool) *PendingClaimer {
	return &PendingClaimer{pool: p.pool}
}

func (c *PendingClaimer) Claim(ctx context.Context, limit int, now time.Time, lease time.Duration) ([]app.PendingWork, error) {
	if limit <= 0 {
		return nil, nil
	}
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	const q = `
		WITH picked AS (
			SELECT id
			FROM wagering.wager_transactions
			WHERE origin = 'EXTERNAL'
			  AND status IN ('PENDING', 'PENDING_REFERENCE')
			  AND (next_attempt_at IS NULL OR next_attempt_at <= $1)
			ORDER BY next_attempt_at ASC NULLS FIRST, id ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		)
		UPDATE wagering.wager_transactions t
		SET attempt_count = t.attempt_count + 1,
		    next_attempt_at = $3
		FROM picked
		WHERE t.id = picked.id
		RETURNING t.id, t.wallet_id, t.attempt_count, t.created_at, t.status`

	rows, err := tx.Query(ctx, q, now, limit, now.Add(lease))
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var items []app.PendingWork
	for rows.Next() {
		var item app.PendingWork
		var status string
		if err := rows.Scan(&item.TransactionID, &item.WalletID, &item.Attempts, &item.CreatedAt, &status); err != nil {
			return nil, mapError(err)
		}
		item.Status = domain.Status(status)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, mapError(err)
	}
	return items, nil
}

func (c *PendingClaimer) ScheduleRetry(ctx context.Context, transactionID string, nextAttempt time.Time) error {
	const q = `
		UPDATE wagering.wager_transactions
		SET next_attempt_at = $2
		WHERE id = $1
		  AND status IN ('PENDING', 'PENDING_REFERENCE')`
	_, err := c.pool.Exec(ctx, q, transactionID, nextAttempt)
	if err != nil {
		return mapError(err)
	}
	return nil
}
