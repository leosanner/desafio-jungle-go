package domain

import (
	"errors"
	"testing"
)

func TestApplyBetWinLoss(t *testing.T) {
	t.Parallel()
	opened := openWallet(t, "100.00")
	w := opened.Wallet

	bet := applyOn(t, &w, externalTx(t, KindBet, "25.00", "tx-bet"), nil, nil)
	if bet.Operation.Status() != StatusProcessed {
		t.Fatalf("bet status = %s", bet.Operation.Status())
	}
	if bet.Ledger == nil || bet.Ledger.Direction() != DirectionDebit {
		t.Fatal("bet must debit")
	}
	if bet.Wallet.Balance().AmountString() != "75.00" || bet.Wallet.Version() != 2 {
		t.Fatalf("after bet bal=%s v=%d", bet.Wallet.Balance().AmountString(), bet.Wallet.Version())
	}
	assertEventTypes(t, bet.Events, EventTypeWagerTransactionProcessed, EventTypeWalletBalanceChanged)

	w = bet.Wallet
	win := applyOn(t, &w, externalTx(t, KindWin, "10.00", "tx-win"), nil, nil)
	if win.Wallet.Balance().AmountString() != "85.00" || win.Wallet.Version() != 3 {
		t.Fatalf("after win bal=%s v=%d", win.Wallet.Balance().AmountString(), win.Wallet.Version())
	}

	w = win.Wallet
	loss := applyOn(t, &w, externalTx(t, KindLoss, "0.00", "tx-loss"), nil, nil)
	if loss.Ledger != nil {
		t.Fatal("LOSS must not create a ledger entry")
	}
	if loss.Wallet.Version() != w.Version() || loss.Wallet.Balance().AmountString() != "85.00" {
		t.Fatal("LOSS must not change balance or version")
	}
	assertEventTypes(t, loss.Events, EventTypeWagerTransactionProcessed)
}

func TestApplyBetInsufficientFunds(t *testing.T) {
	t.Parallel()
	opened := openWallet(t, "50.00")
	w := opened.Wallet
	res := applyOn(t, &w, externalTx(t, KindBet, "80.00", "tx-bet"), nil, nil)
	if res.Operation.Status() != StatusRejected {
		t.Fatalf("status = %s", res.Operation.Status())
	}
	if res.Operation.FailureCode() != FailureInsufficientFunds {
		t.Fatalf("code = %s", res.Operation.FailureCode())
	}
	if res.Ledger != nil {
		t.Fatal("rejected bet must not post ledger")
	}
	if res.Wallet.Balance().AmountString() != "50.00" || res.Wallet.Version() != 1 {
		t.Fatal("wallet unchanged")
	}
	assertEventTypes(t, res.Events, EventTypeWagerTransactionRejected)
}

func TestApplyRefundOfBet(t *testing.T) {
	t.Parallel()
	opened := openWallet(t, "100.00")
	w := opened.Wallet
	bet := applyOn(t, &w, externalTx(t, KindBet, "25.00", "tx-bet"), nil, nil)
	w = bet.Wallet
	ref := bet.Operation
	refund := applyOn(t, &w, externalTxRef(t, KindRefund, "25.00", "tx-ref", ref.ExternalID()), &ref, nil)
	if refund.Operation.Status() != StatusProcessed {
		t.Fatalf("status = %s", refund.Operation.Status())
	}
	if refund.Ledger.Direction() != DirectionCredit {
		t.Fatal("refund credits")
	}
	if refund.Wallet.Balance().AmountString() != "100.00" {
		t.Fatalf("balance = %s", refund.Wallet.Balance().AmountString())
	}
}

func TestApplyDuplicateRefundRejected(t *testing.T) {
	t.Parallel()
	opened := openWallet(t, "100.00")
	w := opened.Wallet
	bet := applyOn(t, &w, externalTx(t, KindBet, "25.00", "tx-bet"), nil, nil)
	w = bet.Wallet
	ref := bet.Operation
	first := applyOn(t, &w, externalTxRef(t, KindRefund, "25.00", "tx-ref1", ref.ExternalID()), &ref, nil)
	w = first.Wallet
	second := applyOn(t, &w, externalTxRef(t, KindRefund, "25.00", "tx-ref2", ref.ExternalID()), &ref, []WagerTransaction{first.Operation})
	if second.Operation.Status() != StatusRejected || second.Operation.FailureCode() != FailureDuplicateReversal {
		t.Fatalf("status=%s code=%s", second.Operation.Status(), second.Operation.FailureCode())
	}
	if second.Wallet.Balance().AmountString() != "100.00" {
		t.Fatalf("balance = %s", second.Wallet.Balance().AmountString())
	}
}

func TestApplyRefundAndRollbackOfSameBetMutuallyExclusive(t *testing.T) {
	t.Parallel()
	opened := openWallet(t, "100.00")
	w := opened.Wallet
	bet := applyOn(t, &w, externalTx(t, KindBet, "25.00", "tx-bet"), nil, nil)
	w = bet.Wallet
	ref := bet.Operation
	refund := applyOn(t, &w, externalTxRef(t, KindRefund, "25.00", "tx-ref", ref.ExternalID()), &ref, nil)
	w = refund.Wallet
	rb := applyOn(t, &w, externalTxRef(t, KindRollback, "25.00", "tx-rb", ref.ExternalID()), &ref, []WagerTransaction{refund.Operation})
	if rb.Operation.Status() != StatusRejected || rb.Operation.FailureCode() != FailureDuplicateReversal {
		t.Fatalf("status=%s code=%s", rb.Operation.Status(), rb.Operation.FailureCode())
	}
}

func TestApplyRollbackOfWinWithoutFunds(t *testing.T) {
	t.Parallel()
	opened := openWallet(t, "10.00")
	w := opened.Wallet
	win := applyOn(t, &w, externalTx(t, KindWin, "90.00", "tx-win"), nil, nil)
	w = win.Wallet
	// spend the win so rollback cannot debit
	spent := applyOn(t, &w, externalTx(t, KindBet, "100.00", "tx-spend"), nil, nil)
	w = spent.Wallet
	ref := win.Operation
	rb := applyOn(t, &w, externalTxRef(t, KindRollback, "90.00", "tx-rb", ref.ExternalID()), &ref, nil)
	if rb.Operation.Status() != StatusRejected || rb.Operation.FailureCode() != FailureInsufficientFundsReversal {
		t.Fatalf("status=%s code=%s", rb.Operation.Status(), rb.Operation.FailureCode())
	}
	if rb.Operation.FailureCode() == FailureInsufficientFunds {
		t.Fatal("reversal shortage must not reuse INSUFFICIENT_FUNDS")
	}
	if rb.Ledger != nil {
		t.Fatal("no ledger on rejected reversal")
	}
}

func TestApplyRollbackOfRefund(t *testing.T) {
	t.Parallel()
	opened := openWallet(t, "100.00")
	w := opened.Wallet
	bet := applyOn(t, &w, externalTx(t, KindBet, "25.00", "tx-bet"), nil, nil)
	w = bet.Wallet
	refund := applyOn(t, &w, externalTxRef(t, KindRefund, "25.00", "tx-ref", bet.Operation.ExternalID()), &bet.Operation, nil)
	w = refund.Wallet
	rb := applyOn(t, &w, externalTxRef(t, KindRollback, "25.00", "tx-rb", refund.Operation.ExternalID()), &refund.Operation, nil)
	if rb.Operation.Status() != StatusProcessed {
		t.Fatalf("status = %s", rb.Operation.Status())
	}
	if rb.Ledger.Direction() != DirectionDebit {
		t.Fatal("rollback of refund debits")
	}
	if rb.Wallet.Balance().AmountString() != "75.00" {
		t.Fatalf("balance = %s", rb.Wallet.Balance().AmountString())
	}
}

func TestApplyPendingAndUnsuccessfulReference(t *testing.T) {
	t.Parallel()
	opened := openWallet(t, "100.00")
	w := opened.Wallet

	missing, err := Apply(ApplyInput{
		Wallet:            &w,
		Operation:         externalTxRef(t, KindRefund, "25.00", "tx-ref", "ext-missing"),
		ReferenceLookedUp: true,
		LedgerID:          "led-x",
		Now:               testNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if missing.Operation.Status() != StatusPendingReference {
		t.Fatalf("missing ref status = %s", missing.Operation.Status())
	}
	assertEventTypes(t, missing.Events, EventTypeWagerTransactionPendingReference)

	pendingBet := externalTx(t, KindBet, "25.00", "tx-bet-pending")
	wait, err := Apply(ApplyInput{
		Wallet:            &w,
		Operation:         externalTxRef(t, KindRefund, "25.00", "tx-ref2", pendingBet.ExternalID()),
		Reference:         &pendingBet,
		ReferenceLookedUp: true,
		LedgerID:          "led-y",
		Now:               testNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if wait.Operation.Status() != StatusPendingReference {
		t.Fatalf("pending ref status = %s", wait.Operation.Status())
	}

	rejectedBet := externalTx(t, KindBet, "25.00", "tx-bet-rej")
	if err := rejectedBet.MarkRejected(FailureInsufficientFunds, testNow); err != nil {
		t.Fatal(err)
	}
	uns, err := Apply(ApplyInput{
		Wallet:            &w,
		Operation:         externalTxRef(t, KindRefund, "25.00", "tx-ref3", rejectedBet.ExternalID()),
		Reference:         &rejectedBet,
		ReferenceLookedUp: true,
		LedgerID:          "led-z",
		Now:               testNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if uns.Operation.Status() != StatusRejected || uns.Operation.FailureCode() != FailureReferenceUnsuccessful {
		t.Fatalf("status=%s code=%s", uns.Operation.Status(), uns.Operation.FailureCode())
	}
}

func TestApplyReferenceMismatch(t *testing.T) {
	t.Parallel()
	opened := openWallet(t, "100.00")
	w := opened.Wallet
	bet := applyOn(t, &w, externalTx(t, KindBet, "25.00", "tx-bet"), nil, nil)
	w = bet.Wallet
	ref := bet.Operation
	wrong := externalTxRef(t, KindRefund, "10.00", "tx-ref", ref.ExternalID())
	res := applyOn(t, &w, wrong, &ref, nil)
	if res.Operation.Status() != StatusRejected || res.Operation.FailureCode() != FailureReferenceMismatch {
		t.Fatalf("status=%s code=%s", res.Operation.Status(), res.Operation.FailureCode())
	}
}

func TestApplyWinWithBetReference(t *testing.T) {
	t.Parallel()
	opened := openWallet(t, "100.00")
	w := opened.Wallet
	bet := applyOn(t, &w, externalTx(t, KindBet, "25.00", "tx-bet"), nil, nil)
	w = bet.Wallet
	win := applyOn(t, &w, externalTxRef(t, KindWin, "40.00", "tx-win", bet.Operation.ExternalID()), &bet.Operation, nil)
	if win.Operation.Status() != StatusProcessed {
		t.Fatalf("status = %s", win.Operation.Status())
	}
	if win.Wallet.Balance().AmountString() != "115.00" {
		t.Fatalf("balance = %s", win.Wallet.Balance().AmountString())
	}
}

func TestApplyOpeningViaApplyRejected(t *testing.T) {
	t.Parallel()
	opened := openWallet(t, "10.00")
	w := opened.Wallet
	opening, err := NewOpeningTransaction(OpeningTxParams{
		ID: "tx-o", WalletID: "wal-1", PlayerID: "player-1", Money: mustMoney(t, "10.00", "BRL"), Now: testNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Apply(ApplyInput{Wallet: &w, Operation: opening, Now: testNow})
	if !errors.Is(err, ErrOpeningNotAllowed) {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyTwoEightyBetsOnOneHundred(t *testing.T) {
	t.Parallel()
	opened := openWallet(t, "100.00")
	w := opened.Wallet
	first := applyOn(t, &w, externalTx(t, KindBet, "80.00", "tx-a"), nil, nil)
	if first.Operation.Status() != StatusProcessed {
		t.Fatal("first bet should process")
	}
	w = first.Wallet
	second := applyOn(t, &w, externalTx(t, KindBet, "80.00", "tx-b"), nil, nil)
	if second.Operation.Status() != StatusRejected {
		t.Fatal("second bet should reject")
	}
	if second.Wallet.Balance().AmountString() != "20.00" {
		t.Fatalf("balance = %s", second.Wallet.Balance().AmountString())
	}
	if first.Ledger == nil || second.Ledger != nil {
		t.Fatal("exactly one debit")
	}
}

func TestIdempotencyPayloadConflict(t *testing.T) {
	t.Parallel()
	replay, err := CheckIdempotencyReplay("k", "hash-a", "k", "hash-a")
	if err != nil || !replay {
		t.Fatalf("replay: %v %v", replay, err)
	}
	_, err = CheckIdempotencyReplay("k", "hash-a", "k", "hash-b")
	if !errors.Is(err, ErrIdempotencyPayloadConflict) {
		t.Fatalf("conflict: %v", err)
	}
	if err := CheckExternalIdentity("k1", "k2"); !errors.Is(err, ErrDuplicateExternalTransaction) {
		t.Fatalf("dup ext: %v", err)
	}
}

func TestFailureCodeCorrectableVsDefinitive(t *testing.T) {
	t.Parallel()
	if !FailureInvalidAmount.IsCorrectable() {
		t.Fatal("INVALID_AMOUNT should be correctable")
	}
	if FailureInsufficientFunds.IsCorrectable() {
		t.Fatal("INSUFFICIENT_FUNDS is definitive")
	}
	if FailureDuplicateReversal.IsCorrectable() {
		t.Fatal("DUPLICATE_REVERSAL is definitive")
	}
}

func assertEventTypes(t *testing.T, events []Event, types ...string) {
	t.Helper()
	if len(events) != len(types) {
		t.Fatalf("events len = %d, want %d (%v)", len(events), len(types), eventTypes(events))
	}
	for i, typ := range types {
		if events[i].EventType() != typ {
			t.Fatalf("event[%d] = %s, want %s", i, events[i].EventType(), typ)
		}
	}
}

func eventTypes(events []Event) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.EventType()
	}
	return out
}
