package sqsadapter

import (
	"errors"
	"strings"
	"testing"

	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

func sampleBody(messageID, walletID, amount string) string {
	return `{
  "messageId": "` + messageID + `",
  "type": "WagerTransactionRequested",
  "occurredAt": "2026-09-08T12:00:00.000Z",
  "data": {
    "providerId": "provider-a",
    "externalTransactionId": "` + messageID + `",
    "idempotencyKey": "provider-a:` + messageID + `",
    "playerId": "player-1",
    "walletId": "` + walletID + `",
    "roundId": "round-1",
    "gameId": "fortune-chimp",
    "kind": "BET",
    "money": { "amount": "` + amount + `", "currency": "BRL" }
  }
}`
}

func TestParseInboundHappyPath(t *testing.T) {
	t.Parallel()
	body := sampleBody("msg-123", "wal-1", "25.00")
	cmd, err := ParseInbound([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if cmd.MessageID != "msg-123" || cmd.ConsumerName != app.InboxConsumerWagerTransactions {
		t.Fatalf("%+v", cmd)
	}
	if cmd.PayloadHash != app.HashMessageBody([]byte(body)) {
		t.Fatal("hash is not of raw body")
	}
	if cmd.Submit.IdempotencyKey != "provider-a:msg-123" || cmd.Submit.Kind != domain.KindBet {
		t.Fatalf("submit %+v", cmd.Submit)
	}
	if cmd.Submit.Actor.ProviderID != "provider-a" || cmd.Submit.Actor.Internal {
		t.Fatal("actor must be queue-gated provider, not internal")
	}
	if cmd.Submit.Money.AmountString() != "25.00" {
		t.Fatalf("money %s", cmd.Submit.Money.AmountString())
	}
}

func TestParseInboundRejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want error
	}{
		{name: "empty", body: "", want: errInvalidMessage},
		{name: "json", body: "{", want: errInvalidMessage},
		{name: "missing id", body: `{"type":"WagerTransactionRequested","data":{}}`, want: errInvalidMessage},
		{name: "wrong type", body: `{"messageId":"m","type":"Other","data":{}}`, want: errInvalidMessage},
		{name: "opening", body: `{
			"messageId":"m","type":"WagerTransactionRequested",
			"data":{"providerId":"p","externalTransactionId":"e","idempotencyKey":"k",
			"playerId":"pl","walletId":"w","roundId":"r","gameId":"g","kind":"OPENING",
			"money":{"amount":"1.00","currency":"BRL"}}
		}`, want: domain.ErrOpeningNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseInbound([]byte(tc.body))
			if err == nil || !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestParseInboundRejectsNumericMoney(t *testing.T) {
	t.Parallel()
	body := strings.Replace(sampleBody("msg-n", "wal-1", "25.00"), `"25.00"`, `25.00`, 1)
	_, err := ParseInbound([]byte(body))
	if err == nil {
		t.Fatal("expected error")
	}
	if _, class, ok := domain.Classify(err); !ok || class != domain.FailureClassValidation {
		t.Fatalf("err = %v", err)
	}
}

func TestDispositionOf(t *testing.T) {
	t.Parallel()
	if DispositionOf(nil) != DispositionAck {
		t.Fatal("nil")
	}
	if DispositionOf(errInvalidMessage) != DispositionDLQ {
		t.Fatal("invalid")
	}
	if DispositionOf(app.ErrInboxHashMismatch) != DispositionDLQ {
		t.Fatal("hash")
	}
	if DispositionOf(app.ErrNotFound) != DispositionRetry {
		t.Fatal("not found")
	}
	if DispositionOf(app.ErrUnavailable) != DispositionRetry {
		t.Fatal("unavailable")
	}
	if DispositionOf(domain.NewValidation(domain.FailureInvalidKind, domain.ErrInvalidKind)) != DispositionDLQ {
		t.Fatal("validation")
	}
}
