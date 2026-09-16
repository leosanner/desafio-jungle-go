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
