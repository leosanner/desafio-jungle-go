package app

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEnvelopeFromRecord(t *testing.T) {
	t.Parallel()
	rec := OutboxRecord{
		EventID:       "evt-1",
		EventType:     "WagerTransactionProcessed",
		EventVersion:  1,
		AggregateID:   "tx-1",
		CorrelationID: "corr-1",
		CausationID:   "tx-1",
		OccurredAt:    time.Date(2026, 9, 16, 15, 0, 0, 0, time.UTC),
		Payload:       []byte(`{"transactionId":"tx-1"}`),
	}
	env := EnvelopeFromRecord(rec)
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["eventId"] != "evt-1" || got["eventType"] != "WagerTransactionProcessed" {
		t.Fatalf("envelope = %s", b)
	}
	if got["aggregateId"] != "tx-1" || got["correlationId"] != "corr-1" {
		t.Fatalf("ids = %s", b)
	}
	if got["causationId"] != "tx-1" || got["version"] != float64(1) {
		t.Fatalf("meta = %s", b)
	}
	if got["occurredAt"] != "2026-09-16T15:00:00Z" {
		t.Fatalf("occurredAt = %v", got["occurredAt"])
	}
	data, ok := got["data"].(map[string]any)
	if !ok || data["transactionId"] != "tx-1" {
		t.Fatalf("data = %v", got["data"])
	}
}

func TestEnvelopeOmitsEmptyCausation(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(EnvelopeFromRecord(OutboxRecord{
		EventID:      "e",
		EventType:    "T",
		EventVersion: 1,
		OccurredAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Payload:      []byte(`{}`),
	}))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["causationId"]; ok {
		t.Fatalf("causationId present: %s", b)
	}
}

func TestBackoff(t *testing.T) {
	t.Parallel()
	max := time.Minute
	cases := []struct {
		attempts int
		want     time.Duration
	}{
		{1, time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{6, 32 * time.Second},
		{7, time.Minute},
		{8, time.Minute},
		{100, time.Minute},
		{0, time.Second},
	}
	for _, tc := range cases {
		if got := Backoff(tc.attempts, max); got != tc.want {
			t.Errorf("Backoff(%d) = %s, want %s", tc.attempts, got, tc.want)
		}
	}
}
