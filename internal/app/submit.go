package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

// SubmitCommand is a provider-originated operation (HTTP or SQS).
type SubmitCommand struct {
	Actor               Actor
	IdempotencyKey      string
	ProviderID          string
	ExternalID          string
	PlayerID            string
	WalletID            string
	RoundID             string
	GameID              string
	Kind                domain.Kind
	Money               domain.Money
	ReferenceExternalID string
	CorrelationID       string
}

// SubmitResult is the persisted operation outcome.
type SubmitResult struct {
	Transaction      domain.WagerTransaction
	IdempotentReplay bool
}

// Submit applies an external operation with persistent idempotency.
func (s *Service) Submit(ctx context.Context, cmd SubmitCommand) (out SubmitResult, err error) {
	start := time.Now()
	defer func() { s.observeSubmit(cmd.Kind, MetricChannelHTTP, time.Since(start), out, err) }()
	if err = authorizeSubmit(cmd.Actor, cmd.ProviderID); err != nil {
		return SubmitResult{}, err
	}
	hash, err := domain.HashCanonicalPayload(domain.CanonicalPayload{
		ProviderID:          cmd.ProviderID,
		ExternalID:          cmd.ExternalID,
		PlayerID:            cmd.PlayerID,
		WalletID:            cmd.WalletID,
		RoundID:             cmd.RoundID,
		GameID:              cmd.GameID,
		Kind:                cmd.Kind,
		Money:               cmd.Money,
		ReferenceExternalID: cmd.ReferenceExternalID,
	})
	if err != nil {
		return SubmitResult{}, err
	}

	err = s.uow.Within(ctx, func(ctx context.Context, repos Repositories) error {
		res, replay, err := s.submitInTx(ctx, repos, cmd, hash)
		if err != nil {
			return err
		}
		out = SubmitResult{Transaction: res, IdempotentReplay: replay}
		return nil
	})
	if errors.Is(err, ErrConflict) {
		// Unique violation aborts the SQL transaction; replay in a fresh one.
		return s.loadReplay(ctx, cmd, hash)
	}
	if err != nil {
		return SubmitResult{}, err
	}
	return out, nil
}

func (s *Service) submitInTx(ctx context.Context, repos Repositories, cmd SubmitCommand, hash string) (domain.WagerTransaction, bool, error) {
	existing, err := repos.Transactions.GetByProviderIdempotencyKey(ctx, cmd.ProviderID, cmd.IdempotencyKey)
	if err == nil {
		replay, err := domain.CheckIdempotencyReplay(existing.IdempotencyKey(), existing.PayloadHash(), cmd.IdempotencyKey, hash)
		if err != nil {
			return domain.WagerTransaction{}, false, err
		}
		if replay {
			return existing, true, nil
		}
	} else if !errors.Is(err, ErrNotFound) {
		return domain.WagerTransaction{}, false, err
	}

	byExt, err := repos.Transactions.GetByProviderExternalID(ctx, cmd.ProviderID, cmd.ExternalID)
	if err == nil {
		if err := domain.CheckExternalIdentity(byExt.IdempotencyKey(), cmd.IdempotencyKey); err != nil {
			return domain.WagerTransaction{}, false, err
		}
		return byExt, true, nil
	} else if !errors.Is(err, ErrNotFound) {
		return domain.WagerTransaction{}, false, err
	}

	wallet, err := repos.Wallets.GetByIDForUpdate(ctx, cmd.WalletID)
	if err != nil {
		return domain.WagerTransaction{}, false, err
	}

	// Re-check identity after the wallet lock so a concurrent first writer is visible
	// (READ COMMITTED). Avoids inserting into an already-committed unique key.
	existing, err = repos.Transactions.GetByProviderIdempotencyKey(ctx, cmd.ProviderID, cmd.IdempotencyKey)
	if err == nil {
		replay, err := domain.CheckIdempotencyReplay(existing.IdempotencyKey(), existing.PayloadHash(), cmd.IdempotencyKey, hash)
		if err != nil {
			return domain.WagerTransaction{}, false, err
		}
		if replay {
			return existing, true, nil
		}
	} else if !errors.Is(err, ErrNotFound) {
		return domain.WagerTransaction{}, false, err
	}
	byExt, err = repos.Transactions.GetByProviderExternalID(ctx, cmd.ProviderID, cmd.ExternalID)
	if err == nil {
		if err := domain.CheckExternalIdentity(byExt.IdempotencyKey(), cmd.IdempotencyKey); err != nil {
			return domain.WagerTransaction{}, false, err
		}
		return byExt, true, nil
	} else if !errors.Is(err, ErrNotFound) {
		return domain.WagerTransaction{}, false, err
	}
	if wallet.PlayerID() != cmd.PlayerID {
		return domain.WagerTransaction{}, false, domain.NewValidation(
			domain.FailureMissingIdentity,
			fmt.Errorf("%w: playerId does not match wallet", domain.ErrMissingIdentity),
		)
	}

	now := s.clock.Now()
	op, err := domain.NewExternalTransaction(domain.ExternalTxParams{
		ID:                  s.ids.NewID(),
		ProviderID:          cmd.ProviderID,
		ExternalID:          cmd.ExternalID,
		IdempotencyKey:      cmd.IdempotencyKey,
		PayloadHash:         hash,
		WalletID:            cmd.WalletID,
		PlayerID:            cmd.PlayerID,
		RoundID:             cmd.RoundID,
		GameID:              cmd.GameID,
		Kind:                cmd.Kind,
		Money:               cmd.Money,
		ReferenceExternalID: cmd.ReferenceExternalID,
		Now:                 now,
	})
	if err != nil {
		return domain.WagerTransaction{}, false, err
	}

	applied, err := s.applyInTx(ctx, repos, wallet, op)
	if err != nil {
		return domain.WagerTransaction{}, false, err
	}

	recs, err := RecordsFromEvents(applied.Events, s.ids, cmd.CorrelationID, applied.Operation.ID())
	if err != nil {
		return domain.WagerTransaction{}, false, err
	}

	if err := persistApply(ctx, repos, wallet, applied, recs, false); err != nil {
		return domain.WagerTransaction{}, false, err
	}
	return applied.Operation, false, nil
}

func (s *Service) applyInTx(ctx context.Context, repos Repositories, wallet domain.Wallet, op domain.WagerTransaction) (domain.ApplyResult, error) {
	var ref *domain.WagerTransaction
	lookedUp := op.ReferenceExternalID() != ""
	if lookedUp {
		r, err := repos.Transactions.GetByProviderExternalID(ctx, op.ProviderID(), op.ReferenceExternalID())
		if err == nil {
			ref = &r
		} else if !errors.Is(err, ErrNotFound) {
			return domain.ApplyResult{}, err
		}
	}

	var reversals []domain.WagerTransaction
	if ref != nil {
		list, err := repos.Transactions.ListProcessedReversals(ctx, ref.ID())
		if err != nil {
			return domain.ApplyResult{}, err
		}
		reversals = list
	}

	return domain.Apply(domain.ApplyInput{
		Wallet:             &wallet,
		Operation:          op,
		Reference:          ref,
		ReferenceLookedUp:  lookedUp,
		ProcessedReversals: reversals,
		LedgerID:           s.ids.NewID(),
		Now:                s.clock.Now(),
	})
}

func (s *Service) loadReplay(ctx context.Context, cmd SubmitCommand, hash string) (SubmitResult, error) {
	var out SubmitResult
	err := s.uow.Within(ctx, func(ctx context.Context, repos Repositories) error {
		tx, replay, err := recoverIdempotentInsert(ctx, repos, cmd, hash)
		if err != nil {
			return err
		}
		out = SubmitResult{Transaction: tx, IdempotentReplay: replay}
		return nil
	})
	return out, err
}

func recoverIdempotentInsert(ctx context.Context, repos Repositories, cmd SubmitCommand, hash string) (domain.WagerTransaction, bool, error) {
	existing, err := repos.Transactions.GetByProviderIdempotencyKey(ctx, cmd.ProviderID, cmd.IdempotencyKey)
	if err == nil {
		replay, err := domain.CheckIdempotencyReplay(existing.IdempotencyKey(), existing.PayloadHash(), cmd.IdempotencyKey, hash)
		if err != nil {
			return domain.WagerTransaction{}, false, err
		}
		return existing, replay, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return domain.WagerTransaction{}, false, err
	}
	byExt, err := repos.Transactions.GetByProviderExternalID(ctx, cmd.ProviderID, cmd.ExternalID)
	if err != nil {
		return domain.WagerTransaction{}, false, fmt.Errorf("submit conflict: %w", ErrConflict)
	}
	if err := domain.CheckExternalIdentity(byExt.IdempotencyKey(), cmd.IdempotencyKey); err != nil {
		return domain.WagerTransaction{}, false, err
	}
	return byExt, true, nil
}

func authorizeSubmit(actor Actor, providerID string) error {
	if actor.Internal || actor.ProviderID == "" {
		return ErrForbidden
	}
	if providerID != "" && !actor.CanAccessProvider(providerID) {
		return ErrForbidden
	}
	return nil
}

// ErrForbidden is an authorization failure mapped to HTTP 403.
var ErrForbidden = errors.New("forbidden")
