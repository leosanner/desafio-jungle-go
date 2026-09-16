package postgres

import (
	"context"

	"github.com/leosanner/desafio-jungle-go/internal/app"
)

type inboxRepo struct {
	q querier
}

var _ app.InboxRepository = (*inboxRepo)(nil)

func (r *inboxRepo) Get(ctx context.Context, consumerName, messageID string) (app.InboxRecord, error) {
	const q = `
		SELECT consumer_name, message_id, payload_hash, received_at, completed_at
		FROM wagering.inbox_messages
		WHERE consumer_name = $1 AND message_id = $2`
	var rec app.InboxRecord
	err := r.q.QueryRow(ctx, q, consumerName, messageID).Scan(
		&rec.ConsumerName,
		&rec.MessageID,
		&rec.PayloadHash,
		&rec.ReceivedAt,
		&rec.CompletedAt,
	)
	if err != nil {
		return app.InboxRecord{}, mapError(err)
	}
	return rec, nil
}

func (r *inboxRepo) Insert(ctx context.Context, rec app.InboxRecord) error {
	const q = `
		INSERT INTO wagering.inbox_messages (
			consumer_name, message_id, payload_hash, received_at, completed_at
		) VALUES ($1, $2, $3, $4, $5)`
	_, err := r.q.Exec(ctx, q,
		rec.ConsumerName,
		rec.MessageID,
		rec.PayloadHash,
		rec.ReceivedAt,
		rec.CompletedAt,
	)
	if err != nil {
		return mapError(err)
	}
	return nil
}
