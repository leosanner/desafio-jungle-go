package app

import (
	"context"
	"fmt"
	"time"
)

// AfterPublishFunc is an optional test hook invoked after a successful Publish
// and before MarkPublished. A non-nil error skips the ack (ADR 0016).
type AfterPublishFunc func(ctx context.Context, rec OutboxRecord) error

// RelayParams sizes one publisher tick.
type RelayParams struct {
	Batch      int
	Lease      time.Duration
	BackoffMax time.Duration
}

// OutboxRelay claims due unpublished rows, publishes them, and acks or retries.
type OutboxRelay struct {
	claimer    OutboxClaimer
	bus        EventBus
	clock      Clock
	batch      int
	lease      time.Duration
	backoffMax time.Duration
	afterPub   AfterPublishFunc
}

// NewOutboxRelay constructs the publisher use case.
func NewOutboxRelay(claimer OutboxClaimer, bus EventBus, clock Clock, p RelayParams) *OutboxRelay {
	return &OutboxRelay{
		claimer:    claimer,
		bus:        bus,
		clock:      clock,
		batch:      p.Batch,
		lease:      p.Lease,
		backoffMax: p.BackoffMax,
	}
}

// SetAfterPublish installs a test-only hook. Production wiring leaves it nil.
func (r *OutboxRelay) SetAfterPublish(fn AfterPublishFunc) {
	r.afterPub = fn
}

// PublishResult is the outcome of one PublishDue tick.
type PublishResult struct {
	Published int
	Failed    int
	Skipped   int
}

// PublishDue claims a batch and publishes each row. Send failures schedule retry
// and do not abort the rest of the batch. Claim errors are returned.
func (r *OutboxRelay) PublishDue(ctx context.Context) (PublishResult, error) {
	var out PublishResult
	now := r.clock.Now().UTC()
	recs, err := r.claimer.Claim(ctx, r.batch, now, r.lease)
	if err != nil {
		return out, fmt.Errorf("outbox claim: %w", err)
	}
	for _, rec := range recs {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		env := EnvelopeFromRecord(rec)
		if err := r.bus.Publish(ctx, env); err != nil {
			next := now.Add(Backoff(rec.Attempts, r.backoffMax))
			if retryErr := r.claimer.ScheduleRetry(ctx, rec.EventID, next); retryErr != nil {
				return out, fmt.Errorf("outbox retry %s: %w", rec.EventID, retryErr)
			}
			out.Failed++
			continue
		}
		if r.afterPub != nil {
			if err := r.afterPub(ctx, rec); err != nil {
				out.Skipped++
				continue
			}
		}
		if err := r.claimer.MarkPublished(ctx, rec.EventID, now); err != nil {
			return out, fmt.Errorf("outbox ack %s: %w", rec.EventID, err)
		}
		out.Published++
	}
	return out, nil
}
