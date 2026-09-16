package postgres

import (
	"context"

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
