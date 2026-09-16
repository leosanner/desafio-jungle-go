package composition

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/adapter/postgres"
	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/config"

	"go.uber.org/fx"
)

func pendingModule() fx.Option {
	return fx.Module("pending",
		fx.Provide(
			fx.Annotate(postgres.NewPendingClaimer, fx.As(new(app.PendingClaimer))),
			func(svc *app.Service, claimer app.PendingClaimer, cfg config.Config) *app.PendingResumer {
				return app.NewPendingResumer(svc, claimer, app.ResumeParams{
					Batch:       cfg.PendingBatchSize,
					Lease:       cfg.PendingLease,
					BackoffMax:  cfg.PendingBackoffMax,
					MaxAttempts: cfg.PendingMaxAttempts,
					TTL:         cfg.PendingTTL,
				})
			},
			NewPendingWorker,
		),
		fx.Invoke(func(*PendingWorker) {}),
	)
}

// PendingWorker polls due PENDING / PENDING_REFERENCE rows until shutdown (ADR 0004 / ADR 0019).
type PendingWorker struct {
	resumer  *app.PendingResumer
	interval time.Duration
	lease    time.Duration
	log      *slog.Logger
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewPendingWorker registers start/stop hooks on the worker that owns the goroutine.
func NewPendingWorker(lc fx.Lifecycle, resumer *app.PendingResumer, cfg config.Config, log *slog.Logger) *PendingWorker {
	w := &PendingWorker{
		resumer:  resumer,
		interval: cfg.PendingPollInterval,
		lease:    cfg.PendingLease,
		log:      log,
		done:     make(chan struct{}),
	}
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			ctx, cancel := context.WithCancel(context.Background())
			w.cancel = cancel
			go w.run(ctx)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			w.cancel()
			select {
			case <-w.Done():
				return nil
			case <-ctx.Done():
				return fmt.Errorf("pending worker: shutdown deadline: %w", ctx.Err())
			}
		},
	})
	return w
}

func (w *PendingWorker) run(ctx context.Context) {
	defer close(w.done)
	w.tick()
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick()
		}
	}
}

func (w *PendingWorker) tick() {
	itemCtx, cancel := context.WithTimeout(context.Background(), w.lease)
	defer cancel()
	res, err := w.resumer.ResumeDue(itemCtx)
	if err != nil {
		w.log.Error("pending: resume due", "err", err)
		return
	}
	if res.Processed > 0 || res.Rejected > 0 || res.Waiting > 0 || res.Failed > 0 {
		w.log.Info("pending: tick",
			"processed", res.Processed,
			"rejected", res.Rejected,
			"waiting", res.Waiting,
			"skipped", res.Skipped,
			"failed", res.Failed,
		)
	}
}

// Done is closed after the poll loop exits (tests).
func (w *PendingWorker) Done() <-chan struct{} {
	return w.done
}
