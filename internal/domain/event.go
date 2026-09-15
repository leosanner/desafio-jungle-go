package domain

import "time"

const eventVersion = 1

const (
	EventTypeWagerTransactionProcessed        = "WagerTransactionProcessed"
	EventTypeWagerTransactionRejected         = "WagerTransactionRejected"
	EventTypeWalletBalanceChanged             = "WalletBalanceChanged"
	EventTypeWagerTransactionPendingReference = "WagerTransactionPendingReference"
)

// Event is a domain event. Type and version are set by constructors. Envelope
// fields (eventId, correlationId, causationId) are assigned when writing the outbox.
type Event interface {
	EventType() string
	EventVersion() int
	AggregateID() string
	OccurredAt() time.Time
}

// WagerTransactionProcessed is emitted when an operation completes successfully, including LOSS.
type WagerTransactionProcessed struct {
	TransactionID         string
	WalletID              string
	PlayerID              string
	Origin                Origin
	ProviderID            string
	ExternalTransactionID string
	Kind                  Kind
	Money                 Money
	OccurredAtUTC         time.Time
}

func NewWagerTransactionProcessed(tx WagerTransaction, occurredAt time.Time) WagerTransactionProcessed {
	return WagerTransactionProcessed{
		TransactionID:         tx.id,
		WalletID:              tx.walletID,
		PlayerID:              tx.playerID,
		Origin:                tx.origin,
		ProviderID:            tx.providerID,
		ExternalTransactionID: tx.externalID,
		Kind:                  tx.kind,
		Money:                 tx.money,
		OccurredAtUTC:         occurredAt.UTC(),
	}
}

func (e WagerTransactionProcessed) EventType() string     { return EventTypeWagerTransactionProcessed }
func (e WagerTransactionProcessed) EventVersion() int     { return eventVersion }
func (e WagerTransactionProcessed) AggregateID() string   { return e.TransactionID }
func (e WagerTransactionProcessed) OccurredAt() time.Time { return e.OccurredAtUTC }

// WagerTransactionRejected is a definitive business rejection.
type WagerTransactionRejected struct {
	TransactionID         string
	WalletID              string
	PlayerID              string
	Origin                Origin
	ProviderID            string
	ExternalTransactionID string
	Kind                  Kind
	Money                 Money
	FailureCode           FailureCode
	OccurredAtUTC         time.Time
}

func NewWagerTransactionRejected(tx WagerTransaction, occurredAt time.Time) WagerTransactionRejected {
	return WagerTransactionRejected{
		TransactionID:         tx.id,
		WalletID:              tx.walletID,
		PlayerID:              tx.playerID,
		Origin:                tx.origin,
		ProviderID:            tx.providerID,
		ExternalTransactionID: tx.externalID,
		Kind:                  tx.kind,
		Money:                 tx.money,
		FailureCode:           tx.failureCode,
		OccurredAtUTC:         occurredAt.UTC(),
	}
}

func (e WagerTransactionRejected) EventType() string     { return EventTypeWagerTransactionRejected }
func (e WagerTransactionRejected) EventVersion() int     { return eventVersion }
func (e WagerTransactionRejected) AggregateID() string   { return e.TransactionID }
func (e WagerTransactionRejected) OccurredAt() time.Time { return e.OccurredAtUTC }

// WalletBalanceChanged is emitted only when the stored balance actually changes.
type WalletBalanceChanged struct {
	WalletID      string
	TransactionID string
	Direction     Direction
	Money         Money
	BalanceBefore Money
	BalanceAfter  Money
	WalletVersion int64
	OccurredAtUTC time.Time
}

// WalletBalanceChangedParams is the constructor input for WalletBalanceChanged.
type WalletBalanceChangedParams struct {
	WalletID      string
	TransactionID string
	Direction     Direction
	Money         Money
	BalanceBefore Money
	BalanceAfter  Money
	WalletVersion int64
	OccurredAt    time.Time
}

func NewWalletBalanceChanged(p WalletBalanceChangedParams) WalletBalanceChanged {
	return WalletBalanceChanged{
		WalletID:      p.WalletID,
		TransactionID: p.TransactionID,
		Direction:     p.Direction,
		Money:         p.Money,
		BalanceBefore: p.BalanceBefore,
		BalanceAfter:  p.BalanceAfter,
		WalletVersion: p.WalletVersion,
		OccurredAtUTC: p.OccurredAt.UTC(),
	}
}

func (e WalletBalanceChanged) EventType() string     { return EventTypeWalletBalanceChanged }
func (e WalletBalanceChanged) EventVersion() int     { return eventVersion }
func (e WalletBalanceChanged) AggregateID() string   { return e.WalletID }
func (e WalletBalanceChanged) OccurredAt() time.Time { return e.OccurredAtUTC }

// WagerTransactionPendingReference records wait for a missing or still-pending reference.
type WagerTransactionPendingReference struct {
	TransactionID       string
	WalletID            string
	ProviderID          string
	ReferenceExternalID string
	OccurredAtUTC       time.Time
}

func NewWagerTransactionPendingReference(tx WagerTransaction, occurredAt time.Time) WagerTransactionPendingReference {
	return WagerTransactionPendingReference{
		TransactionID:       tx.id,
		WalletID:            tx.walletID,
		ProviderID:          tx.providerID,
		ReferenceExternalID: tx.referenceExternalID,
		OccurredAtUTC:       occurredAt.UTC(),
	}
}

func (e WagerTransactionPendingReference) EventType() string {
	return EventTypeWagerTransactionPendingReference
}
func (e WagerTransactionPendingReference) EventVersion() int     { return eventVersion }
func (e WagerTransactionPendingReference) AggregateID() string   { return e.TransactionID }
func (e WagerTransactionPendingReference) OccurredAt() time.Time { return e.OccurredAtUTC }
