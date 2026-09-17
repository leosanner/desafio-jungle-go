package composition_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/composition"
	"github.com/leosanner/desafio-jungle-go/internal/config"

	"go.uber.org/fx/fxtest"
)

type emptyClaimer struct{}

func (emptyClaimer) Claim(context.Context, int, time.Time, time.Duration) ([]app.OutboxRecord, error) {
	return nil, nil
}
func (emptyClaimer) MarkPublished(context.Context, string, time.Time) error { return nil }
func (emptyClaimer) ScheduleRetry(context.Context, string, time.Time) error { return nil }
func (emptyClaimer) Lag(context.Context, time.Time) (app.OutboxLag, error) {
	return app.OutboxLag{}, nil
}

type emptyPendingClaimer struct{}

func (emptyPendingClaimer) Claim(context.Context, int, time.Time, time.Duration) ([]app.PendingWork, error) {
	return nil, nil
}
func (emptyPendingClaimer) ScheduleRetry(context.Context, string, time.Time) error { return nil }

type nopBus struct{}

func (nopBus) Publish(context.Context, app.Envelope) error { return nil }

func TestOutboxWorkerStartStopClosesDone(t *testing.T) {
	t.Parallel()
	lc := fxtest.NewLifecycle(t)
	relay := app.NewOutboxRelay(emptyClaimer{}, nopBus{}, app.SystemClock{}, app.RelayParams{
		Batch:      10,
		Lease:      time.Second,
		BackoffMax: time.Minute,
	})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := composition.NewOutboxWorker(lc, relay, config.Config{
		OutboxPollInterval: 20 * time.Millisecond,
		OutboxLease:        time.Second,
	}, log, app.NopMetrics{})
	ctx := t.Context()
	if err := lc.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := lc.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	select {
	case <-w.Done():
	default:
		t.Fatal("worker done not closed after stop")
	}
}

type nopUoW struct{}

func (nopUoW) Within(ctx context.Context, fn func(context.Context, app.Repositories) error) error {
	return nil
}

func TestPendingWorkerStartStopClosesDone(t *testing.T) {
	t.Parallel()
	lc := fxtest.NewLifecycle(t)
	svc := app.NewService(nopUoW{}, app.SystemClock{}, app.UUIDGenerator{}, app.NopMetrics{})
	resumer := app.NewPendingResumer(svc, emptyPendingClaimer{}, app.ResumeParams{
		Batch:       10,
		Lease:       time.Second,
		BackoffMax:  time.Minute,
		MaxAttempts: 8,
		TTL:         time.Hour,
	})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := composition.NewPendingWorker(lc, resumer, config.Config{
		PendingPollInterval: 20 * time.Millisecond,
		PendingLease:        time.Second,
	}, log)
	ctx := t.Context()
	if err := lc.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := lc.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	select {
	case <-w.Done():
	default:
		t.Fatal("worker done not closed after stop")
	}
}
