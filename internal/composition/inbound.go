package composition

import (
	"context"
	"fmt"

	sqsadapter "github.com/leosanner/desafio-jungle-go/internal/adapter/sqs"

	"go.uber.org/fx"
)

func inboundModule() fx.Option {
	return fx.Module("inbound",
		fx.Provide(sqsadapter.NewConsumer, NewInboundWorker),
		fx.Invoke(func(*InboundWorker) {}),
	)
}

// InboundWorker long-polls wager-transactions.fifo until shutdown (ADR 0004 / ADR 0018).
type InboundWorker struct {
	consumer *sqsadapter.Consumer
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewInboundWorker registers start/stop hooks on the worker that owns the goroutine.
func NewInboundWorker(lc fx.Lifecycle, consumer *sqsadapter.Consumer) *InboundWorker {
	w := &InboundWorker{
		consumer: consumer,
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
				_ = w.consumer.ReleaseInFlight(context.Background())
				return fmt.Errorf("inbound worker: shutdown deadline: %w", ctx.Err())
			}
		},
	})
	return w
}

func (w *InboundWorker) run(ctx context.Context) {
	defer close(w.done)
	w.consumer.Run(ctx)
}

// Done is closed after the poll loop exits (tests).
func (w *InboundWorker) Done() <-chan struct{} {
	return w.done
}
