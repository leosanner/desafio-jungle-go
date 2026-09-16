package sqsadapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

// EnvelopeTypeWagerTransactionRequested is the inbound SQS envelope type (init.md §10).
const EnvelopeTypeWagerTransactionRequested = "WagerTransactionRequested"

var errInvalidMessage = errors.New("invalid inbound message")

type wireEnvelope struct {
	MessageID string          `json:"messageId"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
}

type wireData struct {
	ProviderID          string       `json:"providerId"`
	ExternalID          string       `json:"externalTransactionId"`
	IdempotencyKey      string       `json:"idempotencyKey"`
	PlayerID            string       `json:"playerId"`
	WalletID            string       `json:"walletId"`
	RoundID             string       `json:"roundId"`
	GameID              string       `json:"gameId"`
	Kind                string       `json:"kind"`
	Money               domain.Money `json:"money"`
	ReferenceExternalID string       `json:"referenceExternalTransactionId"`
}

// ParseInbound maps a raw SQS body to HandleInbound. The inbox hash is of body as received.
func ParseInbound(body []byte) (app.InboundCommand, error) {
	if len(body) == 0 {
		return app.InboundCommand{}, fmt.Errorf("%w: empty body", errInvalidMessage)
	}
	var env wireEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return app.InboundCommand{}, fmt.Errorf("%w: json", errInvalidMessage)
	}
	if strings.TrimSpace(env.MessageID) == "" {
		return app.InboundCommand{}, fmt.Errorf("%w: messageId", errInvalidMessage)
	}
	if env.Type != EnvelopeTypeWagerTransactionRequested {
		return app.InboundCommand{}, fmt.Errorf("%w: type", errInvalidMessage)
	}
	if len(env.Data) == 0 {
		return app.InboundCommand{}, fmt.Errorf("%w: data", errInvalidMessage)
	}
	var data wireData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		var ce *domain.ClassifiedError
		if errors.As(err, &ce) {
			return app.InboundCommand{}, err
		}
		return app.InboundCommand{}, fmt.Errorf("%w: data json", errInvalidMessage)
	}
	if strings.TrimSpace(data.ProviderID) == "" ||
		strings.TrimSpace(data.ExternalID) == "" ||
		strings.TrimSpace(data.IdempotencyKey) == "" ||
		strings.TrimSpace(data.PlayerID) == "" ||
		strings.TrimSpace(data.WalletID) == "" ||
		strings.TrimSpace(data.RoundID) == "" ||
		strings.TrimSpace(data.GameID) == "" {
		return app.InboundCommand{}, fmt.Errorf("%w: required field", errInvalidMessage)
	}
	kind, err := domain.ParseKind(data.Kind)
	if err != nil {
		return app.InboundCommand{}, err
	}
	if kind == domain.KindOpening {
		return app.InboundCommand{}, domain.NewValidation(domain.FailureOpeningNotAllowed, domain.ErrOpeningNotAllowed)
	}
	if err := data.Money.RequireInitialized(); err != nil {
		return app.InboundCommand{}, err
	}
	return app.InboundCommand{
		ConsumerName: app.InboxConsumerWagerTransactions,
		MessageID:    env.MessageID,
		PayloadHash:  app.HashMessageBody(body),
		Submit: app.SubmitCommand{
			Actor:               app.Actor{ClientID: "sqs", ProviderID: data.ProviderID},
			IdempotencyKey:      data.IdempotencyKey,
			ProviderID:          data.ProviderID,
			ExternalID:          data.ExternalID,
			PlayerID:            data.PlayerID,
			WalletID:            data.WalletID,
			RoundID:             data.RoundID,
			GameID:              data.GameID,
			Kind:                kind,
			Money:               data.Money,
			ReferenceExternalID: data.ReferenceExternalID,
			CorrelationID:       env.MessageID,
		},
	}, nil
}
