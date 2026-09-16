package app

import (
	"context"
	"errors"
	"testing"

	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

func inboundCmd(t *testing.T, walletID, messageID, body string, money domain.Money) InboundCommand {
	t.Helper()
	return InboundCommand{
		ConsumerName: InboxConsumerWagerTransactions,
		MessageID:    messageID,
		PayloadHash:  HashMessageBody([]byte(body)),
		Submit: SubmitCommand{
			Actor:          Actor{ClientID: "sqs", ProviderID: "provider-a"},
			IdempotencyKey: "provider-a:" + messageID,
			ProviderID:     "provider-a",
			ExternalID:     messageID,
			PlayerID:       "player-1",
			WalletID:       walletID,
			RoundID:        "round-1",
			GameID:         "game-1",
			Kind:           domain.KindBet,
			Money:          money,
		},
	}
}

func TestHandleInboundWritesInboxWithDomain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, store := testService(t)
	opened, err := svc.OpenWallet(ctx, OpenWalletCommand{
		PlayerID:       "player-1",
		InitialBalance: mustParse(t, "100.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := inboundCmd(t, opened.Wallet.ID(), "msg-1", `{"messageId":"msg-1"}`, mustParse(t, "10.00", "BRL"))
	out, err := svc.HandleInbound(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if out.IdempotentReplay || out.Transaction.Status() != domain.StatusProcessed {
		t.Fatalf("first = replay=%v status=%s", out.IdempotentReplay, out.Transaction.Status())
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.inbox) != 1 {
		t.Fatalf("inbox = %d, want 1", len(store.inbox))
	}
	if len(store.ledger) != 2 { // opening + bet
		t.Fatalf("ledger = %d, want 2", len(store.ledger))
	}
}

func TestHandleInboundReplaySameHash(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, store := testService(t)
	opened, err := svc.OpenWallet(ctx, OpenWalletCommand{
		PlayerID:       "player-1",
		InitialBalance: mustParse(t, "100.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := inboundCmd(t, opened.Wallet.ID(), "msg-2", `{"messageId":"msg-2"}`, mustParse(t, "10.00", "BRL"))
	if _, err := svc.HandleInbound(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	replay, err := svc.HandleInbound(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !replay.IdempotentReplay {
		t.Fatal("expected inbox replay")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.ledger) != 2 {
		t.Fatalf("ledger = %d after replay, want 2", len(store.ledger))
	}
}

func TestHandleInboundHashMismatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := testService(t)
	opened, err := svc.OpenWallet(ctx, OpenWalletCommand{
		PlayerID:       "player-1",
		InitialBalance: mustParse(t, "100.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := inboundCmd(t, opened.Wallet.ID(), "msg-3", `{"messageId":"msg-3"}`, mustParse(t, "10.00", "BRL"))
	if _, err := svc.HandleInbound(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	cmd.PayloadHash = HashMessageBody([]byte(`{"messageId":"msg-3","tampered":true}`))
	_, err = svc.HandleInbound(ctx, cmd)
	if !errors.Is(err, ErrInboxHashMismatch) {
		t.Fatalf("err = %v, want hash mismatch", err)
	}
}

func TestHandleInboundDoesNotWriteOnValidationError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, store := testService(t)
	cmd := inboundCmd(t, "missing-wallet", "msg-4", `{"messageId":"msg-4"}`, mustParse(t, "10.00", "BRL"))
	_, err := svc.HandleInbound(ctx, cmd)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want not found", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.inbox) != 0 {
		t.Fatalf("inbox = %d, want 0", len(store.inbox))
	}
}

func TestHandleInboundHTTPThenSQSSameKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, store := testService(t)
	opened, err := svc.OpenWallet(ctx, OpenWalletCommand{
		PlayerID:       "player-1",
		InitialBalance: mustParse(t, "100.00", "BRL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	httpCmd := SubmitCommand{
		Actor:          Actor{ProviderID: "provider-a"},
		IdempotencyKey: "provider-a:shared-1",
		ProviderID:     "provider-a",
		ExternalID:     "shared-1",
		PlayerID:       "player-1",
		WalletID:       opened.Wallet.ID(),
		RoundID:        "round-1",
		GameID:         "game-1",
		Kind:           domain.KindBet,
		Money:          mustParse(t, "15.00", "BRL"),
	}
	if _, err := svc.Submit(ctx, httpCmd); err != nil {
		t.Fatal(err)
	}
	in := inboundCmd(t, opened.Wallet.ID(), "sqs-shared-1", `{"messageId":"sqs-shared-1"}`, mustParse(t, "15.00", "BRL"))
	in.Submit.IdempotencyKey = httpCmd.IdempotencyKey
	in.Submit.ExternalID = httpCmd.ExternalID
	out, err := svc.HandleInbound(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if !out.IdempotentReplay {
		t.Fatal("SQS after HTTP should replay")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.ledger) != 2 {
		t.Fatalf("ledger = %d, want 2 (opening+one debit)", len(store.ledger))
	}
	if len(store.inbox) != 1 {
		t.Fatalf("inbox = %d, want 1", len(store.inbox))
	}
}

func TestHashMessageBodyStable(t *testing.T) {
	t.Parallel()
	a := HashMessageBody([]byte(`{"messageId":"x"}`))
	b := HashMessageBody([]byte(`{"messageId":"x"}`))
	c := HashMessageBody([]byte(`{"messageId":"y"}`))
	if len(a) != 64 || a != b {
		t.Fatalf("hash %s / %s", a, b)
	}
	if a == c {
		t.Fatal("different bodies hashed equal")
	}
}
