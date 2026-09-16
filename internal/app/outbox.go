package app

import (
	"encoding/json"
	"fmt"

	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

// RecordsFromEvents snapshots domain events as unpublished outbox rows.
func RecordsFromEvents(events []domain.Event, ids IDGenerator, correlationID, causationID string) ([]OutboxRecord, error) {
	if correlationID == "" {
		correlationID = ids.NewID()
	}
	out := make([]OutboxRecord, 0, len(events))
	for _, e := range events {
		payload, err := json.Marshal(e)
		if err != nil {
			return nil, fmt.Errorf("outbox payload: %w", err)
		}
		out = append(out, OutboxRecord{
			EventID:       ids.NewID(),
			EventType:     e.EventType(),
			EventVersion:  e.EventVersion(),
			AggregateID:   e.AggregateID(),
			CorrelationID: correlationID,
			CausationID:   causationID,
			OccurredAt:    e.OccurredAt(),
			Payload:       payload,
		})
	}
	return out, nil
}
