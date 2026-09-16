package app

import (
	"context"
	"fmt"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

// ResumeParams sizes one pending-reference tick (ADR 0019).
type ResumeParams struct {
	Batch       int
	Lease       time.Duration
	BackoffMax  time.Duration
	MaxAttempts int
	TTL         time.Duration
}

// ResumeResult is the outcome of one ResumeDue tick.
type ResumeResult struct {
	Processed int
	Rejected  int
	Waiting   int
	Skipped   int
	Failed    int
}

// PendingOutcome is the durable result of resuming one claimed row.
type PendingOutcome int

const (
	PendingSkipped PendingOutcome = iota
	PendingWaiting
	PendingProcessed
	PendingRejected
)

// PendingResumer claims due PENDING / PENDING_REFERENCE rows and applies them.
type PendingResumer struct {
	svc     *Service
	claimer PendingClaimer
	batch   int
	lease   time.Duration
	backoff time.Duration
	maxTry  int
	ttl     time.Duration
}

// NewPendingResumer constructs the pending-reference use case.
func NewPendingResumer(svc *Service, claimer PendingClaimer, p ResumeParams) *PendingResumer {
	return &PendingResumer{
		svc:     svc,
		claimer: claimer,
		batch:   p.Batch,
		lease:   p.Lease,
		backoff: p.BackoffMax,
		maxTry:  p.MaxAttempts,
		ttl:     p.TTL,
	}
}

// ResumeDue claims a batch and resumes each row. Item errors increment Failed
// and do not abort the rest of the batch. Claim errors are returned.
func (r *PendingResumer) ResumeDue(ctx context.Context) (ResumeResult, error) {
	var out ResumeResult
	now := r.svc.clock.Now().UTC()
	items, err := r.claimer.Claim(ctx, r.batch, now, r.lease)
	if err != nil {
		return out, fmt.Errorf("pending claim: %w", err)
	}
	params := ResumeParams{MaxAttempts: r.maxTry, TTL: r.ttl}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		outcome, err := r.svc.resumeOne(ctx, item, params)
		if err != nil {
			out.Failed++
			continue
		}
		switch outcome {
		case PendingWaiting:
			next := now.Add(Backoff(item.Attempts, r.backoff))
			if retryErr := r.claimer.ScheduleRetry(ctx, item.TransactionID, next); retryErr != nil {
				return out, fmt.Errorf("pending retry %s: %w", item.TransactionID, retryErr)
			}
			out.Waiting++
		case PendingProcessed:
			out.Processed++
		case PendingRejected:
			out.Rejected++
		default:
			out.Skipped++
		}
	}
	return out, nil
}

func (s *Service) resumeOne(ctx context.Context, work PendingWork, p ResumeParams) (PendingOutcome, error) {
	var outcome PendingOutcome
	err := s.uow.Within(ctx, func(ctx context.Context, repos Repositories) error {
		now := s.clock.Now().UTC()
		// Wallet first, then the transaction: same order as Submit (ADR 0010 / 0019).
		wallet, err := repos.Wallets.GetByIDForUpdate(ctx, work.WalletID)
		if err != nil {
			return err
		}
		op, err := repos.Transactions.GetByIDForUpdate(ctx, work.TransactionID)
		if err != nil {
			return err
		}
		if op.Status().Terminal() ||
			(op.Status() != domain.StatusPending && op.Status() != domain.StatusPendingReference) {
			outcome = PendingSkipped
			return nil
		}
		if op.WalletID() != work.WalletID {
			return fmt.Errorf("pending: wallet mismatch for %s", work.TransactionID)
		}

		applied, err := s.applyInTx(ctx, repos, wallet, op)
		if err != nil {
			return err
		}

		if applied.Operation.Status() == domain.StatusPendingReference && pendingExhausted(work, applied.Operation, now, p) {
			rej := applied.Operation
			if err := rej.MarkRejected(domain.FailureReferenceNotFound, now); err != nil {
				return err
			}
			applied.Operation = rej
			applied.Events = []domain.Event{domain.NewWagerTransactionRejected(rej, now)}
			applied.Ledger = nil
			applied.Wallet = wallet
		}

		recs, err := RecordsFromEvents(applied.Events, s.ids, applied.Operation.ID(), applied.Operation.ID())
		if err != nil {
			return err
		}
		if err := persistApply(ctx, repos, wallet, applied, recs, true); err != nil {
			return err
		}

		switch applied.Operation.Status() {
		case domain.StatusPendingReference:
			outcome = PendingWaiting
		case domain.StatusRejected:
			outcome = PendingRejected
		case domain.StatusProcessed:
			outcome = PendingProcessed
		default:
			outcome = PendingSkipped
		}
		return nil
	})
	return outcome, err
}

func pendingExhausted(work PendingWork, op domain.WagerTransaction, now time.Time, p ResumeParams) bool {
	if p.MaxAttempts > 0 && work.Attempts >= p.MaxAttempts {
		return true
	}
	if p.TTL > 0 && !now.Before(op.CreatedAt().UTC().Add(p.TTL)) {
		return true
	}
	return false
}
