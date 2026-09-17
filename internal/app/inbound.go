package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

// InboundCommand is one SQS WagerTransactionRequested envelope after parse (ADR 0017).
type InboundCommand struct {
	ConsumerName string
	MessageID    string
	PayloadHash  string
	Submit       SubmitCommand
}

// HashMessageBody is the inbox payload hash: lowercase SHA-256 hex of the raw SQS body.
func HashMessageBody(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// HandleInbound applies an inbound SQS message with inbox + domain in one commit.
func (s *Service) HandleInbound(ctx context.Context, cmd InboundCommand) (out SubmitResult, err error) {
	start := time.Now()
	defer func() { s.observeSubmit(cmd.Submit.Kind, MetricChannelSQS, time.Since(start), out, err) }()
	if err = validateInbound(cmd); err != nil {
		return SubmitResult{}, err
	}
	err = s.uow.Within(ctx, func(ctx context.Context, repos Repositories) error {
		res, err := s.inboundInTx(ctx, repos, cmd)
		if err != nil {
			return err
		}
		out = res
		return nil
	})
	if errors.Is(err, ErrConflict) {
		return s.recoverInbound(ctx, cmd)
	}
	if err != nil {
		return SubmitResult{}, err
	}
	return out, nil
}

func validateInbound(cmd InboundCommand) error {
	if strings.TrimSpace(cmd.ConsumerName) == "" || strings.TrimSpace(cmd.MessageID) == "" {
		return domain.NewValidation(domain.FailureMissingIdentity, fmt.Errorf("%w: inbox identity", domain.ErrMissingIdentity))
	}
	if len(cmd.PayloadHash) != 64 {
		return domain.NewValidation(domain.FailureMissingIdentity, fmt.Errorf("%w: payload hash", domain.ErrMissingIdentity))
	}
	return nil
}

func (s *Service) inboundInTx(ctx context.Context, repos Repositories, cmd InboundCommand) (SubmitResult, error) {
	hash, err := domain.HashCanonicalPayload(canonicalFromSubmit(cmd.Submit))
	if err != nil {
		return SubmitResult{}, err
	}

	existing, err := repos.Inbox.Get(ctx, cmd.ConsumerName, cmd.MessageID)
	if err == nil {
		if existing.PayloadHash != cmd.PayloadHash {
			return SubmitResult{}, fmt.Errorf("%w", ErrInboxHashMismatch)
		}
		tx, _, err := s.submitInTx(ctx, repos, cmd.Submit, hash)
		if err != nil {
			return SubmitResult{}, err
		}
		return SubmitResult{Transaction: tx, IdempotentReplay: true}, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return SubmitResult{}, err
	}

	tx, replay, err := s.submitInTx(ctx, repos, cmd.Submit, hash)
	if err != nil {
		return SubmitResult{}, err
	}
	now := s.clock.Now().UTC()
	if err := repos.Inbox.Insert(ctx, InboxRecord{
		ConsumerName: cmd.ConsumerName,
		MessageID:    cmd.MessageID,
		PayloadHash:  cmd.PayloadHash,
		ReceivedAt:   now,
		CompletedAt:  now,
	}); err != nil {
		return SubmitResult{}, err
	}
	return SubmitResult{Transaction: tx, IdempotentReplay: replay}, nil
}

func (s *Service) recoverInbound(ctx context.Context, cmd InboundCommand) (SubmitResult, error) {
	var out SubmitResult
	err := s.uow.Within(ctx, func(ctx context.Context, repos Repositories) error {
		existing, err := repos.Inbox.Get(ctx, cmd.ConsumerName, cmd.MessageID)
		found := err == nil
		if found {
			if existing.PayloadHash != cmd.PayloadHash {
				return fmt.Errorf("%w", ErrInboxHashMismatch)
			}
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}

		hash, err := domain.HashCanonicalPayload(canonicalFromSubmit(cmd.Submit))
		if err != nil {
			return err
		}
		tx, replay, err := recoverIdempotentInsert(ctx, repos, cmd.Submit, hash)
		if err != nil {
			return err
		}
		if !found {
			now := s.clock.Now().UTC()
			if err := repos.Inbox.Insert(ctx, InboxRecord{
				ConsumerName: cmd.ConsumerName,
				MessageID:    cmd.MessageID,
				PayloadHash:  cmd.PayloadHash,
				ReceivedAt:   now,
				CompletedAt:  now,
			}); err != nil && !errors.Is(err, ErrConflict) {
				return err
			}
		}
		out = SubmitResult{Transaction: tx, IdempotentReplay: replay || found}
		return nil
	})
	return out, err
}

func canonicalFromSubmit(cmd SubmitCommand) domain.CanonicalPayload {
	return domain.CanonicalPayload{
		ProviderID:          cmd.ProviderID,
		ExternalID:          cmd.ExternalID,
		PlayerID:            cmd.PlayerID,
		WalletID:            cmd.WalletID,
		RoundID:             cmd.RoundID,
		GameID:              cmd.GameID,
		Kind:                cmd.Kind,
		Money:               cmd.Money,
		ReferenceExternalID: cmd.ReferenceExternalID,
	}
}
