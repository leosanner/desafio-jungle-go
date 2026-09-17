package composition

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/adapter/postgres"
	sqsadapter "github.com/leosanner/desafio-jungle-go/internal/adapter/sqs"
	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/config"

	"go.uber.org/fx"
)

func outboxModule() fx.Option {
	return fx.Module("outbox",
		fx.Provide(
			fx.Annotate(postgres.NewOutboxClaimer, fx.As(new(app.OutboxClaimer))),
			func(c *sqsadapter.Client) app.EventBus { return c },
			func(claimer app.OutboxClaimer, bus app.EventBus, clock app.Clock, cfg config.Config) *app.OutboxRelay {
				return app.NewOutboxRelay(claimer, bus, clock, app.RelayParams{
					Batch:      cfg.OutboxBatchSize,
					Lease:      cfg.OutboxLease,
					BackoffMax: cfg.OutboxBackoffMax,
				})
			},
			NewOutboxWorker,
		),
		fx.Invoke(func(*OutboxWorker) {}),
	)
}

// OutboxWorker polls unpublished outbox rows until shutdown (ADR 0004 / ADR 0016).
type OutboxWorker struct {
	relay    *app.OutboxRelay
	interval time.Duration
	lease    time.Duration
	log      *slog.Logger
	metrics  app.Metrics
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewOutboxWorker registers start/stop hooks on the worker that owns the goroutine.
func NewOutboxWorker(lc fx.Lifecycle, relay *app.OutboxRelay, cfg config.Config, log *slog.Logger, metrics app.Metrics) *OutboxWorker {
	if metrics == nil {
		metrics = app.NopMetrics{}
	}
	w := &OutboxWorker{
		relay:    relay,
		interval: cfg.OutboxPollInterval,
		lease:    cfg.OutboxLease,
		log:      log,
		metrics:  metrics,
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
			case <-w.done:
				return nil
			case <-ctx.Done():
				return fmt.Errorf("outbox worker: shutdown deadline: %w", ctx.Err())
			}
		},
	})
	return w
}

func (w *OutboxWorker) run(ctx context.Context) {
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

func (w *OutboxWorker) tick() {
	// Bounded independently of poll cancellation so SendMessage can finish or time out.
	itemCtx, cancel := context.WithTimeout(context.Background(), w.lease)
	defer cancel()
	res, err := w.relay.PublishDue(itemCtx)
	if err != nil {
		w.log.Error("outbox: publish due", "err", err)
		return
	}
	w.metrics.IncOutboxPublished(res.Published)
	w.metrics.IncOutboxPublishFailed(res.Failed)
	if lag, lagErr := w.relay.Lag(itemCtx); lagErr != nil {
		w.log.Error("outbox: lag", "err", lagErr)
	} else {
		w.metrics.SetOutboxLag(lag.Unpublished, lag.OldestAge)
	}
	if res.Published > 0 || res.Failed > 0 || res.Skipped > 0 {
		w.log.Info("outbox: tick",
			"published", res.Published,
			"failed", res.Failed,
			"skipped", res.Skipped,
		)
	}
}

// Done is closed after the poll loop exits (tests).
func (w *OutboxWorker) Done() <-chan struct{} {
	return w.done
}
