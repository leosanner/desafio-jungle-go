package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeClaimer struct {
	mu        sync.Mutex
	due       []OutboxRecord
	published []string
	retries   []retryCall
	claimErr  error
	ackErr    error
}

type retryCall struct {
	eventID string
	next    time.Time
}

func (f *fakeClaimer) Claim(_ context.Context, limit int, _ time.Time, _ time.Duration) ([]OutboxRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	if limit > len(f.due) {
		limit = len(f.due)
	}
	out := append([]OutboxRecord(nil), f.due[:limit]...)
	f.due = f.due[limit:]
	for i := range out {
		out[i].Attempts++
	}
	return out, nil
}

func (f *fakeClaimer) MarkPublished(_ context.Context, eventID string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ackErr != nil {
		return f.ackErr
	}
	f.published = append(f.published, eventID)
	return nil
}

func (f *fakeClaimer) ScheduleRetry(_ context.Context, eventID string, next time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retries = append(f.retries, retryCall{eventID: eventID, next: next})
	return nil
}

func (f *fakeClaimer) Lag(_ context.Context, _ time.Time) (OutboxLag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return OutboxLag{Unpublished: len(f.due)}, nil
}

type fakeBus struct {
	mu      sync.Mutex
	sent    []Envelope
	failFor map[string]error
}

func (f *fakeBus) Publish(_ context.Context, env Envelope) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failFor[env.EventID]; err != nil {
		return err
	}
	f.sent = append(f.sent, env)
	return nil
}

func testRelay(claimer OutboxClaimer, bus EventBus) *OutboxRelay {
	return NewOutboxRelay(claimer, bus, fixedClock{t: time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)}, RelayParams{
		Batch:      10,
		Lease:      30 * time.Second,
		BackoffMax: time.Minute,
	})
}

func TestPublishDueEmpty(t *testing.T) {
	t.Parallel()
	claimer := &fakeClaimer{}
	bus := &fakeBus{}
	res, err := testRelay(claimer, bus).PublishDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Published != 0 || len(bus.sent) != 0 {
		t.Fatalf("res=%+v sent=%d", res, len(bus.sent))
	}
}

func TestPublishDueSuccessAcks(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	claimer := &fakeClaimer{due: []OutboxRecord{{
		EventID:       "evt-1",
		EventType:     "WagerTransactionProcessed",
		EventVersion:  1,
		AggregateID:   "tx-1",
		CorrelationID: "c1",
		OccurredAt:    now,
		Payload:       []byte(`{"transactionId":"tx-1"}`),
	}}}
	bus := &fakeBus{}
	res, err := testRelay(claimer, bus).PublishDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Published != 1 || res.Failed != 0 {
		t.Fatalf("res=%+v", res)
	}
	if len(bus.sent) != 1 || bus.sent[0].EventID != "evt-1" {
		t.Fatalf("sent=%+v", bus.sent)
	}
	if len(claimer.published) != 1 || claimer.published[0] != "evt-1" {
		t.Fatalf("published=%v", claimer.published)
	}
}

func TestPublishDueSendFailureSchedulesRetry(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	claimer := &fakeClaimer{due: []OutboxRecord{{
		EventID:      "evt-fail",
		EventType:    "T",
		EventVersion: 1,
		AggregateID:  "a",
		OccurredAt:   now,
		Payload:      []byte(`{}`),
	}}}
	bus := &fakeBus{failFor: map[string]error{"evt-fail": errors.New("broker down")}}
	res, err := testRelay(claimer, bus).PublishDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed != 1 || res.Published != 0 {
		t.Fatalf("res=%+v", res)
	}
	if len(claimer.published) != 0 {
		t.Fatalf("published=%v", claimer.published)
	}
	if len(claimer.retries) != 1 || claimer.retries[0].eventID != "evt-fail" {
		t.Fatalf("retries=%v", claimer.retries)
	}
	// attempts after claim is 1 → backoff 1s
	want := now.Add(time.Second)
	if !claimer.retries[0].next.Equal(want) {
		t.Fatalf("next=%s want %s", claimer.retries[0].next, want)
	}
}

func TestPublishDueSkipAckHook(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	claimer := &fakeClaimer{due: []OutboxRecord{{
		EventID:      "evt-hook",
		EventType:    "T",
		EventVersion: 1,
		AggregateID:  "a",
		OccurredAt:   now,
		Payload:      []byte(`{}`),
	}}}
	bus := &fakeBus{}
	relay := testRelay(claimer, bus)
	relay.SetAfterPublish(func(context.Context, OutboxRecord) error {
		return errors.New("injected crash after publish")
	})
	res, err := relay.PublishDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 1 || res.Published != 0 {
		t.Fatalf("res=%+v", res)
	}
	if len(bus.sent) != 1 {
		t.Fatalf("sent=%d", len(bus.sent))
	}
	if len(claimer.published) != 0 {
		t.Fatalf("ack should be skipped, published=%v", claimer.published)
	}
}

func TestPublishDueClaimError(t *testing.T) {
	t.Parallel()
	claimer := &fakeClaimer{claimErr: errors.New("db down")}
	_, err := testRelay(claimer, &fakeBus{}).PublishDue(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}
