package app

import (
	"encoding/json"
	"time"
)

// EnvelopeFromRecord builds the published wire envelope from a persisted row.
func EnvelopeFromRecord(rec OutboxRecord) Envelope {
	data := json.RawMessage(rec.Payload)
	if len(data) == 0 {
		data = json.RawMessage("{}")
	}
	return Envelope{
		EventID:       rec.EventID,
		EventType:     rec.EventType,
		AggregateID:   rec.AggregateID,
		CorrelationID: rec.CorrelationID,
		CausationID:   rec.CausationID,
		OccurredAt:    rec.OccurredAt.UTC(),
		Version:       rec.EventVersion,
		Data:          data,
	}
}

// Backoff returns exponential delay min(max, 1s << (attempts-1)).
func Backoff(attempts int, max time.Duration) time.Duration {
	if max <= 0 {
		max = time.Minute
	}
	if attempts < 1 {
		attempts = 1
	}
	exp := attempts - 1
	const maxShift = 20
	if exp > maxShift {
		return max
	}
	d := time.Second << uint(exp)
	if d > max || d <= 0 {
		return max
	}
	return d
}
