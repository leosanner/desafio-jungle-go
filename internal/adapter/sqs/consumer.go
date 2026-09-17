package sqsadapter

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/config"
)

// AfterCommitFunc is an optional test hook invoked after a successful HandleInbound
// and before DeleteMessage. A non-nil error skips the delete (ADR 0018, TST-12).
type AfterCommitFunc func(ctx context.Context, messageID string) error

// Consumer long-polls the wager queue and drives HandleInbound (ADR 0018).
type Consumer struct {
	client      *Client
	svc         *app.Service
	log         *slog.Logger
	metrics     app.Metrics
	visibility  time.Duration
	wait        time.Duration
	backoffMax  time.Duration
	afterCommit AfterCommitFunc

	mu       sync.Mutex
	inflight string
}

// NewConsumer constructs the inbound adapter. Production wiring leaves AfterCommit nil.
func NewConsumer(client *Client, svc *app.Service, cfg config.Config, log *slog.Logger, metrics app.Metrics) *Consumer {
	if metrics == nil {
		metrics = app.NopMetrics{}
	}
	return &Consumer{
		client:     client,
		svc:        svc,
		log:        log,
		metrics:    metrics,
		visibility: cfg.SQSVisibilityTimeout,
		wait:       cfg.SQSWaitTime,
		backoffMax: cfg.SQSBackoffMax,
	}
}

// SetAfterCommit installs a test-only hook. Production wiring leaves it nil.
func (c *Consumer) SetAfterCommit(fn AfterCommitFunc) {
	c.afterCommit = fn
}

// Run receives until ctx is cancelled, then returns after the in-flight message finishes.
func (c *Consumer) Run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		msgs, err := c.client.ReceiveWager(ctx, c.wait, c.visibility)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.log.Error("sqs consumer: receive", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		for _, msg := range msgs {
			itemCtx, cancel := context.WithTimeout(context.Background(), c.visibility)
			c.Handle(itemCtx, msg)
			cancel()
		}
	}
}

// ProcessOnce receives up to one message with a short wait and handles it (tests).
func (c *Consumer) ProcessOnce(ctx context.Context) error {
	msgs, err := c.client.ReceiveWager(ctx, time.Second, c.visibility)
	if err != nil {
		return err
	}
	for _, msg := range msgs {
		c.Handle(ctx, msg)
	}
	return nil
}

// Handle parses, calls HandleInbound, then acks, retries or dead-letters.
func (c *Consumer) Handle(ctx context.Context, msg InboundMessage) {
	c.setInFlight(msg.ReceiptHandle)
	defer c.clearInFlight(msg.ReceiptHandle)

	cmd, err := ParseInbound([]byte(msg.Body))
	if err != nil {
		c.log.Info("sqs inbound rejected", "messageId", cmd.MessageID, "err", err)
		c.finish(ctx, msg, cmd.MessageID, err)
		return
	}
	out, err := c.svc.HandleInbound(ctx, cmd)
	if err != nil {
		c.log.Info("sqs inbound failed",
			"messageId", cmd.MessageID,
			"correlationId", cmd.Submit.CorrelationID,
			"walletId", cmd.Submit.WalletID,
			"providerId", cmd.Submit.ProviderID,
			"err", err,
		)
		c.finish(ctx, msg, cmd.MessageID, err)
		return
	}
	c.log.Info("sqs inbound",
		"messageId", cmd.MessageID,
		"correlationId", cmd.Submit.CorrelationID,
		"transactionId", out.Transaction.ID(),
		"walletId", out.Transaction.WalletID(),
		"providerId", out.Transaction.ProviderID(),
		"kind", string(out.Transaction.Kind()),
		"status", string(out.Transaction.Status()),
		"idempotentReplay", out.IdempotentReplay,
	)
	if c.afterCommit != nil {
		if hookErr := c.afterCommit(ctx, cmd.MessageID); hookErr != nil {
			c.log.Info("sqs consumer: skip delete after commit", "messageId", cmd.MessageID)
			return
		}
	}
	if err := c.client.Delete(ctx, msg.ReceiptHandle); err != nil {
		c.log.Error("sqs consumer: delete", "messageId", cmd.MessageID, "err", err)
	}
}

func (c *Consumer) finish(ctx context.Context, msg InboundMessage, messageID string, err error) {
	switch DispositionOf(err) {
	case DispositionAck:
		if delErr := c.client.Delete(ctx, msg.ReceiptHandle); delErr != nil {
			c.log.Error("sqs consumer: delete", "messageId", messageID, "err", delErr)
		}
	case DispositionDLQ:
		dedup := messageID
		if dedup == "" {
			dedup = app.HashMessageBody([]byte(msg.Body))
		}
		if dlqErr := c.client.SendToDLQ(ctx, msg.Body, msg.GroupID, dedup); dlqErr != nil {
			c.log.Error("sqs consumer: dlq", "messageId", messageID, "err", dlqErr)
			c.retry(ctx, msg, err)
			return
		}
		c.metricsOrNop().IncSQSDLQ()
		if delErr := c.client.Delete(ctx, msg.ReceiptHandle); delErr != nil {
			c.log.Error("sqs consumer: delete after dlq", "messageId", messageID, "err", delErr)
		}
	default:
		c.retry(ctx, msg, err)
	}
}

func (c *Consumer) retry(ctx context.Context, msg InboundMessage, cause error) {
	c.metricsOrNop().IncSQSRetry()
	delay := app.Backoff(msg.ReceiveCount, c.backoffMax)
	c.log.Info("sqs consumer: retry", "receiveCount", msg.ReceiveCount, "delay", delay.String(), "err", cause)
	if visErr := c.client.ChangeVisibility(ctx, msg.ReceiptHandle, delay); visErr != nil {
		c.log.Error("sqs consumer: visibility", "err", visErr)
	}
}

func (c *Consumer) metricsOrNop() app.Metrics {
	if c.metrics == nil {
		return app.NopMetrics{}
	}
	return c.metrics
}

func (c *Consumer) setInFlight(handle string) {
	c.mu.Lock()
	c.inflight = handle
	c.mu.Unlock()
}

func (c *Consumer) clearInFlight(handle string) {
	c.mu.Lock()
	if c.inflight == handle {
		c.inflight = ""
	}
	c.mu.Unlock()
}

// ReleaseInFlight sets visibility to 0 so a stopped worker does not hold the message.
func (c *Consumer) ReleaseInFlight(ctx context.Context) error {
	c.mu.Lock()
	handle := c.inflight
	c.mu.Unlock()
	if handle == "" {
		return nil
	}
	return c.client.ChangeVisibility(ctx, handle, 0)
}
