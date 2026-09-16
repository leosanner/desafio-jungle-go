//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	sqsadapter "github.com/leosanner/desafio-jungle-go/internal/adapter/sqs"
	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/config"
	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

func inboundEnvelope(messageID, provider, walletID, playerID, extID, key, amount string) []byte {
	body := map[string]any{
		"messageId":  messageID,
		"type":       sqsadapter.EnvelopeTypeWagerTransactionRequested,
		"occurredAt": "2026-09-08T12:00:00.000Z",
		"data": map[string]any{
			"providerId":            provider,
			"externalTransactionId": extID,
			"idempotencyKey":        key,
			"playerId":              playerID,
			"walletId":              walletID,
			"roundId":               "round-1",
			"gameId":                "fortune-chimp",
			"kind":                  "BET",
			"money":                 map[string]string{"amount": amount, "currency": "BRL"},
		},
	}
	raw, _ := json.Marshal(body)
	return raw
}

func countInbox(t *testing.T, db *testDB) int {
	t.Helper()
	var n int
	if err := db.pool.pool.QueryRow(t.Context(), `SELECT count(*) FROM wagering.inbox_messages`).Scan(&n); err != nil {
		t.Fatalf("count inbox: %v", err)
	}
	return n
}

func countLedger(t *testing.T, db *testDB, walletID string) int {
	t.Helper()
	var n int
	if err := db.pool.pool.QueryRow(t.Context(), `SELECT count(*) FROM wagering.wallet_ledger_entries WHERE wallet_id = $1`, walletID).Scan(&n); err != nil {
		t.Fatalf("count ledger: %v", err)
	}
	return n
}

func TestHandleInboundInboxAtomicWithDomain(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)
	svc := app.NewService(db.uow, app.SystemClock{}, app.UUIDGenerator{}, app.NopMetrics{})
	opened, err := svc.OpenWallet(t.Context(), app.OpenWalletCommand{
		PlayerID:       uniqueID(t, "player-"),
		InitialBalance: mustParseMoney(t, "100.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	msgID := uniqueID(t, "msg-")
	body := inboundEnvelope(msgID, "provider-a", opened.Wallet.ID(), opened.Wallet.PlayerID(), msgID, "provider-a:"+msgID, "10.00")
	cmd, err := sqsadapter.ParseInbound(body)
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.HandleInbound(t.Context(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if out.Transaction.Status() != domain.StatusProcessed {
		t.Fatalf("status = %s", out.Transaction.Status())
	}
	if countInbox(t, db) != 1 {
		t.Fatalf("inbox = %d", countInbox(t, db))
	}
	if countLedger(t, db, opened.Wallet.ID()) != 2 {
		t.Fatalf("ledger = %d, want 2", countLedger(t, db, opened.Wallet.ID()))
	}

	replay, err := svc.HandleInbound(t.Context(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !replay.IdempotentReplay {
		t.Fatal("expected replay")
	}
	if countLedger(t, db, opened.Wallet.ID()) != 2 {
		t.Fatal("replay debited twice")
	}
}

func TestHandleInboundNoInboxOnMissingWallet(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)
	svc := app.NewService(db.uow, app.SystemClock{}, app.UUIDGenerator{}, app.NopMetrics{})
	msgID := uniqueID(t, "msg-")
	body := inboundEnvelope(msgID, "provider-a", uniqueID(t, "wal-"), uniqueID(t, "player-"), msgID, "provider-a:"+msgID, "10.00")
	cmd, err := sqsadapter.ParseInbound(body)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.HandleInbound(t.Context(), cmd)
	if !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if countInbox(t, db) != 0 {
		t.Fatal("inbox row on failed treatment")
	}
}

func newInboundTestClient(t *testing.T, visibility time.Duration) *sqsadapter.Client {
	t.Helper()
	cfg := requireSQSEnv(t)
	suffix := uniqueSuffix(t)
	mainName := "wager-in-it-" + suffix + ".fifo"
	dlqName := "wager-in-dlq-it-" + suffix + ".fifo"
	cfg.SQSWagerQueueName = mainName
	cfg.SQSWagerDLQName = dlqName
	c, err := sqsadapter.NewClient(cfg)
	if err != nil {
		t.Fatalf("sqs client: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	if err := c.CreateFIFOQueueWithRedrive(ctx, mainName, dlqName, visibility, 5); err != nil {
		t.Fatalf("create inbound queues: %v", err)
	}
	t.Cleanup(func() {
		_ = c.DeleteQueue(context.Background(), mainName)
		_ = c.DeleteQueue(context.Background(), dlqName)
	})
	return c
}

func newInboundConsumer(t *testing.T, db *testDB, client *sqsadapter.Client, visibility time.Duration) *sqsadapter.Consumer {
	t.Helper()
	svc := app.NewService(db.uow, app.SystemClock{}, app.UUIDGenerator{}, app.NopMetrics{})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return sqsadapter.NewConsumer(client, svc, config.Config{
		SQSVisibilityTimeout: visibility,
		SQSWaitTime:          time.Second,
		SQSBackoffMax:        2 * time.Second,
	}, log)
}

func waitProcess(t *testing.T, c *sqsadapter.Consumer) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		err := c.ProcessOnce(ctx)
		cancel()
		if err != nil {
			t.Fatalf("ProcessOnce: %v", err)
		}
		return
	}
}

func TestInboundConsumerProcessesBet(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)
	client := newInboundTestClient(t, 30*time.Second)
	svc := app.NewService(db.uow, app.SystemClock{}, app.UUIDGenerator{}, app.NopMetrics{})
	opened, err := svc.OpenWallet(t.Context(), app.OpenWalletCommand{
		PlayerID:       uniqueID(t, "player-"),
		InitialBalance: mustParseMoney(t, "100.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	msgID := uniqueID(t, "msg-")
	body := inboundEnvelope(msgID, "provider-a", opened.Wallet.ID(), opened.Wallet.PlayerID(), msgID, "provider-a:"+msgID, "25.00")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := client.SendWager(ctx, string(body), opened.Wallet.ID(), msgID); err != nil {
		t.Fatal(err)
	}
	consumer := newInboundConsumer(t, db, client, 30*time.Second)
	waitProcess(t, consumer)
	if countInbox(t, db) != 1 {
		t.Fatalf("inbox = %d", countInbox(t, db))
	}
	if countLedger(t, db, opened.Wallet.ID()) != 2 {
		t.Fatalf("ledger = %d", countLedger(t, db, opened.Wallet.ID()))
	}
	w, err := svc.GetWallet(t.Context(), opened.Wallet.ID())
	if err != nil {
		t.Fatal(err)
	}
	assertMoney(t, w.Balance(), "75.00", "BRL")
}

func TestInboundRecoverCommitBeforeDelete(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)
	client := newInboundTestClient(t, time.Second)
	svc := app.NewService(db.uow, app.SystemClock{}, app.UUIDGenerator{}, app.NopMetrics{})
	opened, err := svc.OpenWallet(t.Context(), app.OpenWalletCommand{
		PlayerID:       uniqueID(t, "player-"),
		InitialBalance: mustParseMoney(t, "100.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	msgID := uniqueID(t, "msg-")
	body := inboundEnvelope(msgID, "provider-a", opened.Wallet.ID(), opened.Wallet.PlayerID(), msgID, "provider-a:"+msgID, "25.00")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := client.SendWager(ctx, string(body), opened.Wallet.ID(), msgID); err != nil {
		t.Fatal(err)
	}
	consumer := newInboundConsumer(t, db, client, time.Second)
	consumer.SetAfterCommit(func(context.Context, string) error {
		return context.Canceled
	})
	waitProcess(t, consumer)
	if countInbox(t, db) != 1 {
		t.Fatal("expected inbox after commit-without-delete")
	}
	if countLedger(t, db, opened.Wallet.ID()) != 2 {
		t.Fatal("expected one debit")
	}

	consumer.SetAfterCommit(nil)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_ = consumer.ProcessOnce(t.Context())
		w, err := svc.GetWallet(t.Context(), opened.Wallet.ID())
		if err != nil {
			t.Fatal(err)
		}
		if w.Balance().AmountString() == "75.00" && countLedger(t, db, opened.Wallet.ID()) == 2 {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("redelivery changed the outcome")
}

func TestInboundInvalidMessageGoesToDLQ(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)
	client := newInboundTestClient(t, 30*time.Second)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := client.SendWager(ctx, `{not-json`, "invalid", uniqueID(t, "dedup-")); err != nil {
		t.Fatal(err)
	}
	consumer := newInboundConsumer(t, db, client, 30*time.Second)
	waitProcess(t, consumer)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		bodies, err := client.ReceiveDLQ(t.Context(), 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(bodies) > 0 {
			if countInbox(t, db) != 0 {
				t.Fatal("invalid message wrote inbox")
			}
			return
		}
	}
	t.Fatal("dlq empty")
}

func TestInboundHTTPxSQSSameKeyOneDebit(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)
	client := newInboundTestClient(t, 30*time.Second)
	svc := app.NewService(db.uow, app.SystemClock{}, app.UUIDGenerator{}, app.NopMetrics{})
	opened, err := svc.OpenWallet(t.Context(), app.OpenWalletCommand{
		PlayerID:       uniqueID(t, "player-"),
		InitialBalance: mustParseMoney(t, "100.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	extID := uniqueID(t, "ext-")
	key := "provider-a:" + extID
	if _, err := svc.Submit(t.Context(), app.SubmitCommand{
		Actor:          app.Actor{ProviderID: "provider-a"},
		IdempotencyKey: key,
		ProviderID:     "provider-a",
		ExternalID:     extID,
		PlayerID:       opened.Wallet.PlayerID(),
		WalletID:       opened.Wallet.ID(),
		RoundID:        "round-1",
		GameID:         "fortune-chimp",
		Kind:           domain.KindBet,
		Money:          mustParseMoney(t, "20.00", "BRL"),
	}); err != nil {
		t.Fatal(err)
	}
	msgID := uniqueID(t, "msg-")
	body := inboundEnvelope(msgID, "provider-a", opened.Wallet.ID(), opened.Wallet.PlayerID(), extID, key, "20.00")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := client.SendWager(ctx, string(body), opened.Wallet.ID(), msgID); err != nil {
		t.Fatal(err)
	}
	consumer := newInboundConsumer(t, db, client, 30*time.Second)
	waitProcess(t, consumer)
	if countLedger(t, db, opened.Wallet.ID()) != 2 {
		t.Fatalf("ledger = %d, want 2 (opening+one debit)", countLedger(t, db, opened.Wallet.ID()))
	}
	if countInbox(t, db) != 1 {
		t.Fatalf("inbox = %d", countInbox(t, db))
	}
	w, err := svc.GetWallet(t.Context(), opened.Wallet.ID())
	if err != nil {
		t.Fatal(err)
	}
	assertMoney(t, w.Balance(), "80.00", "BRL")
}

func TestInboundDuplicateSQSCopiesOneDebit(t *testing.T) {
	t.Parallel()
	db := openMigratedDB(t)
	client := newInboundTestClient(t, 30*time.Second)
	svc := app.NewService(db.uow, app.SystemClock{}, app.UUIDGenerator{}, app.NopMetrics{})
	opened, err := svc.OpenWallet(t.Context(), app.OpenWalletCommand{
		PlayerID:       uniqueID(t, "player-"),
		InitialBalance: mustParseMoney(t, "100.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	msgID := uniqueID(t, "msg-")
	body := inboundEnvelope(msgID, "provider-a", opened.Wallet.ID(), opened.Wallet.PlayerID(), msgID, "provider-a:"+msgID, "10.00")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := client.SendWager(ctx, string(body), opened.Wallet.ID(), msgID+"-a"); err != nil {
		t.Fatal(err)
	}
	if err := client.SendWager(ctx, string(body), opened.Wallet.ID(), msgID+"-b"); err != nil {
		t.Fatal(err)
	}
	consumer := newInboundConsumer(t, db, client, 30*time.Second)
	waitProcess(t, consumer)
	waitProcess(t, consumer)
	if countLedger(t, db, opened.Wallet.ID()) != 2 {
		t.Fatalf("ledger = %d, want 2", countLedger(t, db, opened.Wallet.ID()))
	}
	if countInbox(t, db) != 1 {
		t.Fatalf("inbox = %d", countInbox(t, db))
	}
}
