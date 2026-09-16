package domain

import (
	"errors"
	"fmt"
	"time"
)

// ApplyInput is an external operation plus the wallet and optional resolved reference.
type ApplyInput struct {
	Wallet             *Wallet
	Operation          WagerTransaction
	Reference          *WagerTransaction
	ReferenceLookedUp  bool
	ProcessedReversals []WagerTransaction
	LedgerID           string
	Now                time.Time
}

// ApplyResult is the in-memory outcome. Persistence (same SQL commit as ledger/outbox) is the use case's job.
type ApplyResult struct {
	Wallet    Wallet
	Operation WagerTransaction
	Ledger    *WalletLedgerEntry
	Events    []Event
}

// Apply executes BET/WIN/LOSS/REFUND/ROLLBACK against a wallet copy.
// Validation errors (class Validation) mean the in-memory state was not applied.
// Wait/Rejection are represented on Operation.Status; Apply itself returns nil error then.
func Apply(in ApplyInput) (ApplyResult, error) {
	if in.Wallet == nil {
		return ApplyResult{}, validation(FailureMissingIdentity, fmt.Errorf("%w: wallet", ErrMissingIdentity))
	}
	op := in.Operation
	if op.origin != OriginExternal {
		return ApplyResult{}, validation(FailureOpeningNotAllowed, ErrOpeningNotAllowed)
	}
	if op.kind == KindOpening {
		return ApplyResult{}, validation(FailureOpeningNotAllowed, ErrOpeningNotAllowed)
	}
	if op.status != StatusPending && op.status != StatusPendingReference {
		return ApplyResult{}, rejection(FailureInvalidTransition, ErrTerminalState)
	}
	if err := op.money.RequireInitialized(); err != nil {
		return ApplyResult{}, err
	}
	if op.money.Currency() != in.Wallet.currency {
		return rejectOp(in, FailureIncompatibleCurrency, ErrIncompatibleCurrency)
	}
	if err := validateKindAmount(op.kind, op.money); err != nil {
		return ApplyResult{}, err
	}

	switch op.kind {
	case KindBet:
		return applyBet(in)
	case KindWin:
		return applyWin(in)
	case KindLoss:
		return applyLoss(in)
	case KindRefund:
		return applyRefund(in)
	case KindRollback:
		return applyRollback(in)
	default:
		return ApplyResult{}, validation(FailureInvalidKind, ErrInvalidKind)
	}
}

func applyBet(in ApplyInput) (ApplyResult, error) {
	w := cloneWallet(in.Wallet)
	now := in.Now
	entry, err := w.Debit(in.LedgerID, in.Operation.id, in.Operation.money, now)
	if err != nil {
		if isCoded(err, FailureInsufficientFunds) {
			return rejectOp(in, FailureInsufficientFunds, ErrInsufficientFunds)
		}
		return ApplyResult{}, err
	}
	return processWithLedger(in, w, entry, now)
}

func applyWin(in ApplyInput) (ApplyResult, error) {
	var resolvedID string
	if in.Operation.referenceExternalID != "" {
		if res, waitOrReject, err := resolveOptionalReference(in, KindBet); err != nil {
			return ApplyResult{}, err
		} else if waitOrReject {
			return res, nil
		}
		resolvedID = in.Reference.id
	}
	w := cloneWallet(in.Wallet)
	now := in.Now
	entry, err := w.Credit(in.LedgerID, in.Operation.id, in.Operation.money, now)
	if err != nil {
		return ApplyResult{}, err
	}
	out, err := processWithLedger(in, w, entry, now)
	if err != nil {
		return ApplyResult{}, err
	}
	out.Operation.resolvedReferenceID = resolvedID
	return out, nil
}

func applyLoss(in ApplyInput) (ApplyResult, error) {
	op := in.Operation
	now := in.Now.UTC()
	if err := op.MarkProcessed(in.Wallet.balance, now); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{
		Wallet:    *in.Wallet,
		Operation: op,
		Events:    []Event{NewWagerTransactionProcessed(op, now)},
	}, nil
}

func applyRefund(in ApplyInput) (ApplyResult, error) {
	ref, res, waiting, err := requireReference(in)
	if err != nil {
		return ApplyResult{}, err
	}
	if waiting {
		return res, nil
	}
	if ref.kind != KindBet {
		return rejectOp(in, FailureReferenceKindInvalid, ErrReferenceKindInvalid)
	}
	if err := matchReference(in.Operation, *ref, true); err != nil {
		return rejectOp(in, FailureReferenceMismatch, err)
	}
	if hasProcessed(in.ProcessedReversals, KindRefund, ref.id) || hasProcessed(in.ProcessedReversals, KindRollback, ref.id) {
		return rejectOp(in, FailureDuplicateReversal, ErrDuplicateReversal)
	}
	w := cloneWallet(in.Wallet)
	now := in.Now
	entry, err := w.Credit(in.LedgerID, in.Operation.id, in.Operation.money, now)
	if err != nil {
		return ApplyResult{}, err
	}
	out, err := processWithLedger(in, w, entry, now)
	if err != nil {
		return ApplyResult{}, err
	}
	out.Operation.resolvedReferenceID = ref.id
	return out, nil
}

func applyRollback(in ApplyInput) (ApplyResult, error) {
	ref, res, waiting, err := requireReference(in)
	if err != nil {
		return ApplyResult{}, err
	}
	if waiting {
		return res, nil
	}
	switch ref.kind {
	case KindBet, KindWin, KindRefund:
	default:
		return rejectOp(in, FailureReferenceKindInvalid, ErrReferenceKindInvalid)
	}
	if err := matchReference(in.Operation, *ref, true); err != nil {
		return rejectOp(in, FailureReferenceMismatch, err)
	}
	if hasProcessed(in.ProcessedReversals, KindRollback, ref.id) {
		return rejectOp(in, FailureDuplicateReversal, ErrDuplicateReversal)
	}
	if ref.kind == KindBet && hasProcessed(in.ProcessedReversals, KindRefund, ref.id) {
		return rejectOp(in, FailureDuplicateReversal, ErrDuplicateReversal)
	}

	w := cloneWallet(in.Wallet)
	now := in.Now
	var entry WalletLedgerEntry
	switch ref.kind {
	case KindBet:
		entry, err = w.Credit(in.LedgerID, in.Operation.id, in.Operation.money, now)
	case KindWin, KindRefund:
		entry, err = w.Debit(in.LedgerID, in.Operation.id, in.Operation.money, now)
		if err != nil && isCoded(err, FailureInsufficientFunds) {
			return rejectOp(in, FailureInsufficientFundsReversal, ErrInsufficientFundsReversal)
		}
	}
	if err != nil {
		return ApplyResult{}, err
	}
	out, err := processWithLedger(in, w, entry, now)
	if err != nil {
		return ApplyResult{}, err
	}
	out.Operation.resolvedReferenceID = ref.id
	return out, nil
}

func requireReference(in ApplyInput) (ref *WagerTransaction, res ApplyResult, waiting bool, err error) {
	if in.Operation.referenceExternalID == "" {
		err = validation(FailureMissingReference, ErrMissingReference)
		return
	}
	if !in.ReferenceLookedUp || in.Reference == nil {
		res, err = waitOp(in)
		waiting = err == nil
		return
	}
	ref = in.Reference
	switch ref.status {
	case StatusPending, StatusPendingReference:
		res, err = waitOp(in)
		waiting = err == nil
		return
	case StatusRejected, StatusFailed:
		res, err = rejectOp(in, FailureReferenceUnsuccessful, ErrReferenceUnsuccessful)
		waiting = err == nil
		return
	case StatusProcessed:
		return ref, ApplyResult{}, false, nil
	default:
		err = validation(FailureInvalidStatus, ErrInvalidStatus)
		return
	}
}

func resolveOptionalReference(in ApplyInput, want Kind) (ApplyResult, bool, error) {
	if !in.ReferenceLookedUp || in.Reference == nil {
		res, err := waitOp(in)
		return res, true, err
	}
	ref := in.Reference
	switch ref.status {
	case StatusPending, StatusPendingReference:
		res, err := waitOp(in)
		return res, true, err
	case StatusRejected, StatusFailed:
		res, err := rejectOp(in, FailureReferenceUnsuccessful, ErrReferenceUnsuccessful)
		return res, true, err
	case StatusProcessed:
		if ref.kind != want {
			res, err := rejectOp(in, FailureReferenceKindInvalid, ErrReferenceKindInvalid)
			return res, true, err
		}
		if err := matchReference(in.Operation, *ref, false); err != nil {
			res, e := rejectOp(in, FailureReferenceMismatch, err)
			return res, true, e
		}
		return ApplyResult{}, false, nil
	default:
		return ApplyResult{}, true, validation(FailureInvalidStatus, ErrInvalidStatus)
	}
}

func matchReference(op, ref WagerTransaction, requireEqualAmount bool) error {
	if op.providerID != ref.providerID ||
		op.playerID != ref.playerID ||
		op.walletID != ref.walletID ||
		op.roundID != ref.roundID {
		return ErrReferenceMismatch
	}
	if op.money.Currency() != ref.money.Currency() {
		return ErrReferenceMismatch
	}
	if requireEqualAmount {
		eq, err := op.money.Equal(ref.money)
		if err != nil {
			return err
		}
		if !eq {
			return ErrReferenceMismatch
		}
	}
	return nil
}

func hasProcessed(reversals []WagerTransaction, kind Kind, referencedID string) bool {
	for _, r := range reversals {
		if r.status == StatusProcessed && r.kind == kind && r.resolvedReferenceID == referencedID {
			return true
		}
	}
	return false
}

func processWithLedger(in ApplyInput, w Wallet, entry WalletLedgerEntry, now time.Time) (ApplyResult, error) {
	op := in.Operation
	if err := op.MarkProcessed(w.balance, now); err != nil {
		return ApplyResult{}, err
	}
	events := []Event{
		NewWagerTransactionProcessed(op, now),
		NewWalletBalanceChanged(WalletBalanceChangedParams{
			WalletID:      w.id,
			TransactionID: op.id,
			Direction:     entry.direction,
			Money:         entry.money,
			BalanceBefore: entry.balanceBefore,
			BalanceAfter:  entry.balanceAfter,
			WalletVersion: w.version,
			OccurredAt:    now,
		}),
	}
	return ApplyResult{
		Wallet:    w,
		Operation: op,
		Ledger:    &entry,
		Events:    events,
	}, nil
}

func rejectOp(in ApplyInput, code FailureCode, _ error) (ApplyResult, error) {
	op := in.Operation
	now := in.Now.UTC()
	if err := op.MarkRejected(code, now); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{
		Wallet:    *in.Wallet,
		Operation: op,
		Events:    []Event{NewWagerTransactionRejected(op, now)},
	}, nil
}

func waitOp(in ApplyInput) (ApplyResult, error) {
	op := in.Operation
	now := in.Now.UTC()
	already := op.status == StatusPendingReference
	if err := op.MarkPendingReference("", now); err != nil {
		return ApplyResult{}, err
	}
	var events []Event
	if !already {
		events = []Event{NewWagerTransactionPendingReference(op, now)}
	}
	return ApplyResult{
		Wallet:    *in.Wallet,
		Operation: op,
		Events:    events,
	}, nil
}

func cloneWallet(w *Wallet) Wallet {
	return *w
}

func isCoded(err error, code FailureCode) bool {
	var ce *ClassifiedError
	return errors.As(err, &ce) && ce.Code == code
}
