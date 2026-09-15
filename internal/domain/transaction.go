package domain

import (
	"fmt"
	"time"
)

// WagerTransaction is a financial operation with a validated status machine.
type WagerTransaction struct {
	id                  string
	origin              Origin
	providerID          string
	externalID          string
	idempotencyKey      string
	payloadHash         string
	walletID            string
	playerID            string
	roundID             string
	gameID              string
	kind                Kind
	money               Money
	referenceExternalID string
	resolvedReferenceID string
	status              Status
	failureCode         FailureCode
	resultBalance       Money
	hasResult           bool
	createdAt           time.Time
	updatedAt           time.Time
}

// ExternalTxParams constructs a provider-originated operation in PENDING.
type ExternalTxParams struct {
	ID                  string
	ProviderID          string
	ExternalID          string
	IdempotencyKey      string
	PayloadHash         string
	WalletID            string
	PlayerID            string
	RoundID             string
	GameID              string
	Kind                Kind
	Money               Money
	ReferenceExternalID string
	Now                 time.Time
}

// OpeningTxParams constructs an internal OPENING still PENDING until MarkProcessed.
type OpeningTxParams struct {
	ID       string
	WalletID string
	PlayerID string
	Money    Money
	Now      time.Time
}

// NewExternalTransaction rejects OPENING and validates per-kind amount and reference rules.
func NewExternalTransaction(p ExternalTxParams) (WagerTransaction, error) {
	kind, err := ParseKind(string(p.Kind))
	if err != nil {
		return WagerTransaction{}, err
	}
	if kind == KindOpening || !kind.IsExternal() {
		return WagerTransaction{}, validation(FailureOpeningNotAllowed, ErrOpeningNotAllowed)
	}
	if err := requireID("id", p.ID); err != nil {
		return WagerTransaction{}, err
	}
	if err := requireID("providerId", p.ProviderID); err != nil {
		return WagerTransaction{}, err
	}
	if err := requireID("externalTransactionId", p.ExternalID); err != nil {
		return WagerTransaction{}, err
	}
	if err := requireID("idempotencyKey", p.IdempotencyKey); err != nil {
		return WagerTransaction{}, err
	}
	if err := requireID("payloadHash", p.PayloadHash); err != nil {
		return WagerTransaction{}, err
	}
	if err := requireID("walletId", p.WalletID); err != nil {
		return WagerTransaction{}, err
	}
	if err := requireID("playerId", p.PlayerID); err != nil {
		return WagerTransaction{}, err
	}
	if err := requireID("roundId", p.RoundID); err != nil {
		return WagerTransaction{}, err
	}
	if err := requireID("gameId", p.GameID); err != nil {
		return WagerTransaction{}, err
	}
	if err := p.Money.RequireInitialized(); err != nil {
		return WagerTransaction{}, err
	}
	if err := validateKindAmount(kind, p.Money); err != nil {
		return WagerTransaction{}, err
	}
	if kind.RequiresReference() && p.ReferenceExternalID == "" {
		return WagerTransaction{}, validation(FailureMissingReference, ErrMissingReference)
	}

	now := p.Now.UTC()
	return WagerTransaction{
		id:                  p.ID,
		origin:              OriginExternal,
		providerID:          p.ProviderID,
		externalID:          p.ExternalID,
		idempotencyKey:      p.IdempotencyKey,
		payloadHash:         p.PayloadHash,
		walletID:            p.WalletID,
		playerID:            p.PlayerID,
		roundID:             p.RoundID,
		gameID:              p.GameID,
		kind:                kind,
		money:               p.Money,
		referenceExternalID: p.ReferenceExternalID,
		status:              StatusPending,
		createdAt:           now,
		updatedAt:           now,
	}, nil
}

// NewOpeningTransaction creates an internal OPENING. Provider/external metadata do not apply.
func NewOpeningTransaction(p OpeningTxParams) (WagerTransaction, error) {
	if err := requireID("id", p.ID); err != nil {
		return WagerTransaction{}, err
	}
	if err := requireID("walletId", p.WalletID); err != nil {
		return WagerTransaction{}, err
	}
	if err := requireID("playerId", p.PlayerID); err != nil {
		return WagerTransaction{}, err
	}
	if err := p.Money.RequireInitialized(); err != nil {
		return WagerTransaction{}, err
	}
	if !p.Money.IsPositive() {
		return WagerTransaction{}, validation(FailurePositiveAmountRequired, ErrPositiveAmountRequired)
	}
	now := p.Now.UTC()
	return WagerTransaction{
		id:        p.ID,
		origin:    OriginInternal,
		walletID:  p.WalletID,
		playerID:  p.PlayerID,
		kind:      KindOpening,
		money:     p.Money,
		status:    StatusPending,
		createdAt: now,
		updatedAt: now,
	}, nil
}

// RehydrateTxParams is persistence-shaped input. No movements or events are applied.
type RehydrateTxParams struct {
	ID                  string
	Origin              Origin
	ProviderID          string
	ExternalID          string
	IdempotencyKey      string
	PayloadHash         string
	WalletID            string
	PlayerID            string
	RoundID             string
	GameID              string
	Kind                Kind
	Money               Money
	ReferenceExternalID string
	ResolvedReferenceID string
	Status              Status
	FailureCode         FailureCode
	ResultBalance       Money
	HasResult           bool
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// RehydrateTransaction restores a row without re-applying the operation.
func RehydrateTransaction(p RehydrateTxParams) (WagerTransaction, error) {
	origin, err := ParseOrigin(string(p.Origin))
	if err != nil {
		return WagerTransaction{}, err
	}
	kind, err := ParseKind(string(p.Kind))
	if err != nil {
		return WagerTransaction{}, err
	}
	status, err := ParseStatus(string(p.Status))
	if err != nil {
		return WagerTransaction{}, err
	}
	if err := requireID("id", p.ID); err != nil {
		return WagerTransaction{}, err
	}
	if err := requireID("walletId", p.WalletID); err != nil {
		return WagerTransaction{}, err
	}
	if err := requireID("playerId", p.PlayerID); err != nil {
		return WagerTransaction{}, err
	}
	if err := p.Money.RequireInitialized(); err != nil {
		return WagerTransaction{}, err
	}
	if origin == OriginInternal {
		if kind != KindOpening {
			return WagerTransaction{}, validation(FailureInvalidKind, fmt.Errorf("%w: internal origin requires OPENING", ErrInvalidKind))
		}
	} else if !kind.IsExternal() {
		return WagerTransaction{}, validation(FailureOpeningNotAllowed, ErrOpeningNotAllowed)
	}
	if p.HasResult {
		if err := p.ResultBalance.RequireInitialized(); err != nil {
			return WagerTransaction{}, err
		}
	}
	return WagerTransaction{
		id:                  p.ID,
		origin:              origin,
		providerID:          p.ProviderID,
		externalID:          p.ExternalID,
		idempotencyKey:      p.IdempotencyKey,
		payloadHash:         p.PayloadHash,
		walletID:            p.WalletID,
		playerID:            p.PlayerID,
		roundID:             p.RoundID,
		gameID:              p.GameID,
		kind:                kind,
		money:               p.Money,
		referenceExternalID: p.ReferenceExternalID,
		resolvedReferenceID: p.ResolvedReferenceID,
		status:              status,
		failureCode:         p.FailureCode,
		resultBalance:       p.ResultBalance,
		hasResult:           p.HasResult,
		createdAt:           p.CreatedAt.UTC(),
		updatedAt:           p.UpdatedAt.UTC(),
	}, nil
}

func (t WagerTransaction) ID() string                  { return t.id }
func (t WagerTransaction) Origin() Origin              { return t.origin }
func (t WagerTransaction) ProviderID() string          { return t.providerID }
func (t WagerTransaction) ExternalID() string          { return t.externalID }
func (t WagerTransaction) IdempotencyKey() string      { return t.idempotencyKey }
func (t WagerTransaction) PayloadHash() string         { return t.payloadHash }
func (t WagerTransaction) WalletID() string            { return t.walletID }
func (t WagerTransaction) PlayerID() string            { return t.playerID }
func (t WagerTransaction) RoundID() string             { return t.roundID }
func (t WagerTransaction) GameID() string              { return t.gameID }
func (t WagerTransaction) Kind() Kind                  { return t.kind }
func (t WagerTransaction) Money() Money                { return t.money }
func (t WagerTransaction) ReferenceExternalID() string { return t.referenceExternalID }
func (t WagerTransaction) ResolvedReferenceID() string { return t.resolvedReferenceID }
func (t WagerTransaction) Status() Status              { return t.status }
func (t WagerTransaction) FailureCode() FailureCode    { return t.failureCode }
func (t WagerTransaction) CreatedAt() time.Time        { return t.createdAt }
func (t WagerTransaction) UpdatedAt() time.Time        { return t.updatedAt }

// ResultBalance is the wallet balance observed at PROCESSED time (replay snapshot).
func (t WagerTransaction) ResultBalance() (Money, bool) {
	return t.resultBalance, t.hasResult
}

func (t *WagerTransaction) MarkProcessed(resultBalance Money, now time.Time) error {
	if err := resultBalance.RequireInitialized(); err != nil {
		return err
	}
	if err := t.transition(StatusProcessed, now); err != nil {
		return err
	}
	t.resultBalance = resultBalance
	t.hasResult = true
	t.failureCode = ""
	return nil
}

func (t *WagerTransaction) MarkRejected(code FailureCode, now time.Time) error {
	if code == "" {
		return validation(FailureInvalidTransition, fmt.Errorf("%w: empty failureCode", ErrInvalidTransition))
	}
	if err := t.transition(StatusRejected, now); err != nil {
		return err
	}
	t.failureCode = code
	t.hasResult = false
	return nil
}

func (t *WagerTransaction) MarkPendingReference(resolvedReferenceID string, now time.Time) error {
	if err := t.transition(StatusPendingReference, now); err != nil {
		return err
	}
	if resolvedReferenceID != "" {
		t.resolvedReferenceID = resolvedReferenceID
	}
	return nil
}

func (t *WagerTransaction) MarkFailed(code FailureCode, now time.Time) error {
	if code == "" {
		return validation(FailureInvalidTransition, fmt.Errorf("%w: empty failureCode", ErrInvalidTransition))
	}
	if err := t.transition(StatusFailed, now); err != nil {
		return err
	}
	t.failureCode = code
	t.hasResult = false
	return nil
}

func (t *WagerTransaction) ResolveReference(internalID string) error {
	if err := requireID("resolvedReferenceId", internalID); err != nil {
		return err
	}
	if t.status.Terminal() {
		return rejection(FailureInvalidTransition, ErrTerminalState)
	}
	t.resolvedReferenceID = internalID
	return nil
}

func (t *WagerTransaction) transition(to Status, now time.Time) error {
	if t.status.Terminal() {
		return rejection(FailureInvalidTransition, ErrTerminalState)
	}
	ok := false
	switch t.status {
	case StatusPending:
		ok = to == StatusProcessed || to == StatusRejected || to == StatusFailed || to == StatusPendingReference
	case StatusPendingReference:
		ok = to == StatusProcessed || to == StatusRejected || to == StatusFailed || to == StatusPendingReference
	}
	if !ok {
		return rejection(FailureInvalidTransition, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, t.status, to))
	}
	t.status = to
	t.updatedAt = now.UTC()
	return nil
}

func validateKindAmount(kind Kind, money Money) error {
	switch {
	case kind.RequiresZeroAmount():
		if !money.IsZero() {
			return validation(FailureZeroAmountRequired, ErrZeroAmountRequired)
		}
	case kind.RequiresPositiveAmount():
		if !money.IsPositive() {
			return validation(FailurePositiveAmountRequired, ErrPositiveAmountRequired)
		}
	}
	return nil
}
