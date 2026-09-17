//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/adapter/sqs"
	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/config"
	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

func insertUnpublished(t *testing.T, db *testDB, rec app.OutboxRecord) {
	t.Helper()
	err := db.uow.Within(t.Context(), func(ctx context.Context, repos app.Repositories) error {
		return repos.Outbox.Insert(ctx, rec)
	})
	if err != nil {
		t.Fatalf("insert outbox: %v", err)
	}
}

func sampleRecord(t *testing.T, id string) app.OutboxRecord {
	t.Helper()
	return app.OutboxRecord{
		EventID:       uniqueID(t, id+"-"),
		EventType:     domain.EventTypeWagerTransactionProcessed,
		EventVersion:  1,
		AggregateID:   uniqueID(t, "agg-"),
		CorrelationID: uniqueID(t, "corr-"),
		CausationID:   uniqueID(t, "cause-"),
		OccurredAt:    integrationNow,
		Payload:       []byte(`{"transactionId":"tx-1","kind":"BET"}`),
	}
}

func TestOutboxClaimSkipLocked(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)
	claimer := NewOutboxClaimer(db.pool)

	const n = 8
	ids := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		rec := sampleRecord(t, "evt")
		ids[rec.EventID] = struct{}{}
		insertUnpublished(t, db, rec)
	}

	now := integrationNow.Add(time.Second)
	lease := 30 * time.Second
	got := make(chan []app.OutboxRecord, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			recs, err := claimer.Claim(t.Context(), n, now, lease)
			if err != nil {
				t.Errorf("Claim: %v", err)
				got <- nil
				return
			}
			got <- recs
		}()
	}
	close(start)
	wg.Wait()
	close(got)

	seen := map[string]int{}
	total := 0
	for recs := range got {
		for _, rec := range recs {
			seen[rec.EventID]++
			total++
			if rec.Attempts != 1 {
				t.Errorf("attempts = %d, want 1", rec.Attempts)
			}
		}
	}
	if total != n {
		t.Fatalf("claimed %d, want %d", total, n)
	}
	for id := range ids {
		if seen[id] != 1 {
			t.Errorf("event %s claimed %d times", id, seen[id])
		}
	}
}

func TestOutboxMarkPublishedAndRetry(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)
	claimer := NewOutboxClaimer(db.pool)
	rec := sampleRecord(t, "ack")
	insertUnpublished(t, db, rec)

	now := integrationNow.Add(time.Second)
	claimed, err := claimer.Claim(t.Context(), 1, now, time.Minute)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v n=%d", err, len(claimed))
	}

	// Still unpublished and leased into the future: a second claim now should miss it.
	again, err := claimer.Claim(t.Context(), 1, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("claimed leased row: %+v", again)
	}

	next := now.Add(5 * time.Second)
	if err := claimer.ScheduleRetry(t.Context(), rec.EventID, next); err != nil {
		t.Fatal(err)
	}
	due, err := claimer.Claim(t.Context(), 1, next, time.Minute)
	if err != nil || len(due) != 1 {
		t.Fatalf("claim after retry: %v n=%d", err, len(due))
	}
	if due[0].EventID != rec.EventID {
		t.Fatalf("id = %s", due[0].EventID)
	}
	if due[0].Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", due[0].Attempts)
	}

	if err := claimer.MarkPublished(t.Context(), rec.EventID, now); err != nil {
		t.Fatal(err)
	}
	afterAck, err := claimer.Claim(t.Context(), 1, next.Add(time.Hour), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterAck) != 0 {
		t.Fatalf("claimed published row: %+v", afterAck)
	}
}

func TestOutboxLagUnpublishedAndCleared(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)
	claimer := NewOutboxClaimer(db.pool)
	rec := sampleRecord(t, "lag")
	insertUnpublished(t, db, rec)

	now := integrationNow.Add(time.Minute)
	lag, err := claimer.Lag(t.Context(), now)
	if err != nil {
		t.Fatal(err)
	}
	if lag.Unpublished != 1 {
		t.Fatalf("unpublished = %d", lag.Unpublished)
	}
	if lag.OldestAge != time.Minute {
		t.Fatalf("oldest = %s", lag.OldestAge)
	}

	if err := claimer.MarkPublished(t.Context(), rec.EventID, now); err != nil {
		t.Fatal(err)
	}
	cleared, err := claimer.Lag(t.Context(), now)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Unpublished != 0 || cleared.OldestAge != 0 {
		t.Fatalf("cleared = %+v", cleared)
	}
}

func requireSQSEnv(t *testing.T) config.Config {
	t.Helper()
	if os.Getenv("AWS_ENDPOINT_URL") == "" || os.Getenv("AWS_REGION") == "" {
		t.Skip("AWS_ENDPOINT_URL or AWS_REGION is not set")
	}
	if os.Getenv("AWS_ACCESS_KEY_ID") == "" {
		t.Skip("AWS_ACCESS_KEY_ID is not set")
	}
	cfg, err := config.Load()
	if err != nil {
		// Integration may run with only POSTGRES_DSN+AWS_*; build a minimal SQS config.
		cfg = config.Config{
			AWSRegion:          os.Getenv("AWS_REGION"),
			AWSAccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
			AWSSecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
			AWSEndpointURL:     os.Getenv("AWS_ENDPOINT_URL"),
			SQSWagerQueueName:  getenvDefault("SQS_WAGER_QUEUE_NAME", "wager-transactions.fifo"),
			SQSWagerDLQName:    getenvDefault("SQS_WAGER_DLQ_NAME", "wager-transactions-dlq.fifo"),
			SQSEventsQueueName: getenvDefault("SQS_EVENTS_QUEUE_NAME", "wager-events.fifo"),
		}
	}
	return cfg
}

func getenvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func newTestEventsClient(t *testing.T) *sqsadapter.Client {
	t.Helper()
	cfg := requireSQSEnv(t)
	name := "wager-events-it-" + uniqueSuffix(t) + ".fifo"
	cfg.SQSEventsQueueName = name
	c, err := sqsadapter.NewClient(cfg)
	if err != nil {
		t.Fatalf("sqs client: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	if err := c.CreateFIFOQueue(ctx, name); err != nil {
		t.Fatalf("create queue: %v", err)
	}
	t.Cleanup(func() {
		_ = c.DeleteQueue(context.Background(), name)
	})
	return c
}

func waitEnvelope(t *testing.T, c *sqsadapter.Client, eventID string) app.Envelope {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		bodies, err := c.ReceiveBodies(ctx, 10, 1)
		cancel()
		if err != nil {
			t.Fatalf("receive: %v", err)
		}
		for _, body := range bodies {
			var env app.Envelope
			if err := json.Unmarshal([]byte(body), &env); err != nil {
				continue
			}
			if env.EventID == eventID {
				return env
			}
		}
	}
	t.Fatalf("timed out waiting for event %s", eventID)
	return app.Envelope{}
}

func waitUniqueEventIDs(t *testing.T, c *sqsadapter.Client, want int) map[string]int {
	t.Helper()
	counts := map[string]int{}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		bodies, err := c.ReceiveBodies(ctx, 10, 1)
		cancel()
		if err != nil {
			t.Fatalf("receive: %v", err)
		}
		for _, body := range bodies {
			var env app.Envelope
			if err := json.Unmarshal([]byte(body), &env); err != nil {
				continue
			}
			counts[env.EventID]++
		}
		if len(counts) >= want {
			return counts
		}
	}
	t.Fatalf("timed out waiting for %d events, got %d", want, len(counts))
	return counts
}

func TestOutboxRelayPublishesOpeningEvents(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)
	bus := newTestEventsClient(t)
	svc := app.NewService(db.uow, app.SystemClock{}, app.UUIDGenerator{}, app.NopMetrics{})
	opened, err := svc.OpenWallet(t.Context(), app.OpenWalletCommand{
		PlayerID:       uniqueID(t, "player-"),
		InitialBalance: mustParseMoney(t, "100.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}

	relay := app.NewOutboxRelay(NewOutboxClaimer(db.pool), bus, app.SystemClock{}, app.RelayParams{
		Batch:      10,
		Lease:      30 * time.Second,
		BackoffMax: time.Minute,
	})
	res, err := relay.PublishDue(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if res.Published != 2 {
		t.Fatalf("published = %d, want 2 (processed + balance changed)", res.Published)
	}
	counts := waitUniqueEventIDs(t, bus, 2)
	if len(counts) != 2 {
		t.Fatalf("unique events = %d", len(counts))
	}
	if opened.Wallet.Version() != 1 {
		t.Fatalf("wallet version = %d", opened.Wallet.Version())
	}
}

func TestOutboxRelayPublishesEnvelope(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)
	bus := newTestEventsClient(t)
	rec := sampleRecord(t, "pub")
	insertUnpublished(t, db, rec)

	relay := app.NewOutboxRelay(NewOutboxClaimer(db.pool), bus, app.SystemClock{}, app.RelayParams{
		Batch:      10,
		Lease:      30 * time.Second,
		BackoffMax: time.Minute,
	})
	res, err := relay.PublishDue(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if res.Published != 1 {
		t.Fatalf("published = %d", res.Published)
	}
	env := waitEnvelope(t, bus, rec.EventID)
	if env.EventType != rec.EventType || env.AggregateID != rec.AggregateID {
		t.Fatalf("envelope = %+v", env)
	}
	if env.CorrelationID != rec.CorrelationID || env.Version != 1 {
		t.Fatalf("envelope meta = %+v", env)
	}
	if string(env.Data) == "" {
		t.Fatal("empty data")
	}
}

func TestOutboxTwoPublishersContend(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)
	bus := newTestEventsClient(t)
	const n = 6
	want := map[string]struct{}{}
	for i := 0; i < n; i++ {
		rec := sampleRecord(t, "race")
		want[rec.EventID] = struct{}{}
		insertUnpublished(t, db, rec)
	}

	params := app.RelayParams{Batch: n, Lease: 30 * time.Second, BackoffMax: time.Minute}
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan app.PublishResult, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			relay := app.NewOutboxRelay(NewOutboxClaimer(db.pool), bus, app.SystemClock{}, params)
			<-start
			res, err := relay.PublishDue(t.Context())
			if err != nil {
				t.Errorf("PublishDue: %v", err)
			}
			results <- res
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	published := 0
	for res := range results {
		published += res.Published
	}
	if published != n {
		t.Fatalf("published = %d, want %d", published, n)
	}
	counts := waitUniqueEventIDs(t, bus, n)
	if len(counts) != n {
		t.Fatalf("unique eventIds = %d, want %d (%v)", len(counts), n, counts)
	}
	for id := range want {
		if counts[id] < 1 {
			t.Errorf("missing event %s", id)
		}
	}
}

func TestOutboxRecoverPublishBeforeAck(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)
	bus := newTestEventsClient(t)
	rec := sampleRecord(t, "crash")
	insertUnpublished(t, db, rec)

	claimer := NewOutboxClaimer(db.pool)
	params := app.RelayParams{Batch: 1, Lease: 50 * time.Millisecond, BackoffMax: time.Minute}

	first := app.NewOutboxRelay(claimer, bus, app.SystemClock{}, params)
	first.SetAfterPublish(func(context.Context, app.OutboxRecord) error {
		return context.Canceled
	})
	res, err := first.PublishDue(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 1 || res.Published != 0 {
		t.Fatalf("first tick %+v", res)
	}

	deadline := time.Now().Add(5 * time.Second)
	var second app.PublishResult
	for time.Now().Before(deadline) {
		second, err = app.NewOutboxRelay(claimer, bus, app.SystemClock{}, params).PublishDue(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if second.Published == 1 {
			break
		}
	}
	if second.Published != 1 {
		t.Fatalf("second tick did not ack: %+v", second)
	}
	env := waitEnvelope(t, bus, rec.EventID)
	if env.EventID != rec.EventID {
		t.Fatalf("eventId changed: %s", env.EventID)
	}
}
