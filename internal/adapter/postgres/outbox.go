package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/leosanner/desafio-jungle-go/internal/app"
)

type outboxRepo struct {
	q querier
}

var _ app.OutboxRepository = (*outboxRepo)(nil)

func (r *outboxRepo) Insert(ctx context.Context, rec app.OutboxRecord) error {
	const q = `
		INSERT INTO wagering.outbox_events (
			event_id, event_type, event_version, aggregate_id,
			correlation_id, causation_id, occurred_at, payload,
			attempts, next_attempt_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 0, $7)`
	_, err := r.q.Exec(ctx, q,
		rec.EventID,
		rec.EventType,
		rec.EventVersion,
		rec.AggregateID,
		rec.CorrelationID,
		nullIfEmpty(rec.CausationID),
		rec.OccurredAt,
		rec.Payload,
	)
	if err != nil {
		return mapError(err)
	}
	return nil
}

// OutboxClaimer claims unpublished rows in its own short transaction (ADR 0016).
type OutboxClaimer struct {
	pool *pgxpool.Pool
}

var _ app.OutboxClaimer = (*OutboxClaimer)(nil)

// NewOutboxClaimer uses the process pool, not the financial unit of work.
func NewOutboxClaimer(p *Pool) *OutboxClaimer {
	return &OutboxClaimer{pool: p.pool}
}

func (c *OutboxClaimer) Claim(ctx context.Context, limit int, now time.Time, lease time.Duration) ([]app.OutboxRecord, error) {
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
			SELECT event_id
			FROM wagering.outbox_events
			WHERE published_at IS NULL
			  AND next_attempt_at <= $1
			ORDER BY next_attempt_at ASC, event_id ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		)
		UPDATE wagering.outbox_events o
		SET attempts = o.attempts + 1,
		    next_attempt_at = $3
		FROM picked
		WHERE o.event_id = picked.event_id
		RETURNING o.event_id, o.event_type, o.event_version, o.aggregate_id,
		          o.correlation_id, o.causation_id, o.occurred_at, o.payload, o.attempts`

	rows, err := tx.Query(ctx, q, now, limit, now.Add(lease))
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var recs []app.OutboxRecord
	for rows.Next() {
		var rec app.OutboxRecord
		var causation *string
		if err := rows.Scan(
			&rec.EventID,
			&rec.EventType,
			&rec.EventVersion,
			&rec.AggregateID,
			&rec.CorrelationID,
			&causation,
			&rec.OccurredAt,
			&rec.Payload,
			&rec.Attempts,
		); err != nil {
			return nil, mapError(err)
		}
		rec.CausationID = derefString(causation)
		recs = append(recs, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, mapError(err)
	}
	return recs, nil
}

func (c *OutboxClaimer) MarkPublished(ctx context.Context, eventID string, at time.Time) error {
	const q = `
		UPDATE wagering.outbox_events
		SET published_at = $2
		WHERE event_id = $1 AND published_at IS NULL`
	_, err := c.pool.Exec(ctx, q, eventID, at)
	if err != nil {
		return mapError(err)
	}
	return nil
}

func (c *OutboxClaimer) ScheduleRetry(ctx context.Context, eventID string, nextAttempt time.Time) error {
	const q = `
		UPDATE wagering.outbox_events
		SET next_attempt_at = $2
		WHERE event_id = $1 AND published_at IS NULL`
	_, err := c.pool.Exec(ctx, q, eventID, nextAttempt)
	if err != nil {
		return mapError(err)
	}
	return nil
}
