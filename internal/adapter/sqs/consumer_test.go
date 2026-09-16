package sqsadapter

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/app"
)

type nopUoW struct{}

func (nopUoW) Within(context.Context, func(context.Context, app.Repositories) error) error {
	return nil
}

func TestConsumerRunStopsOnCancel(t *testing.T) {
	t.Parallel()
	api := &fakeQueueAPI{urls: map[string]error{"wager.fifo": nil, "dlq.fifo": nil}}
	c := &Consumer{
		client:     &Client{api: api, wager: "wager.fifo", dlq: "dlq.fifo"},
		svc:        app.NewService(nopUoW{}, app.SystemClock{}, app.UUIDGenerator{}, app.NopMetrics{}),
		log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		wait:       time.Millisecond,
		visibility: time.Second,
		backoffMax: time.Second,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Run(ctx)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
