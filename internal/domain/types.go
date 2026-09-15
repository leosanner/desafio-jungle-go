package domain

// Kind is a wagering operation type.
type Kind string

const (
	KindOpening  Kind = "OPENING"
	KindBet      Kind = "BET"
	KindWin      Kind = "WIN"
	KindLoss     Kind = "LOSS"
	KindRefund   Kind = "REFUND"
	KindRollback Kind = "ROLLBACK"
)

func ParseKind(s string) (Kind, error) {
	k := Kind(s)
	switch k {
	case KindOpening, KindBet, KindWin, KindLoss, KindRefund, KindRollback:
		return k, nil
	default:
		return "", validation(FailureInvalidKind, ErrInvalidKind)
	}
}

func (k Kind) RequiresPositiveAmount() bool {
	switch k {
	case KindOpening, KindBet, KindWin, KindRefund, KindRollback:
		return true
	default:
		return false
	}
}

func (k Kind) RequiresZeroAmount() bool {
	return k == KindLoss
}

func (k Kind) RequiresReference() bool {
	return k == KindRefund || k == KindRollback
}

func (k Kind) MovesBalance() bool {
	switch k {
	case KindOpening, KindBet, KindWin, KindRefund, KindRollback:
		return true
	default:
		return false
	}
}

func (k Kind) IsExternal() bool {
	switch k {
	case KindBet, KindWin, KindLoss, KindRefund, KindRollback:
		return true
	default:
		return false
	}
}

// Status is the WagerTransaction state-machine status.
type Status string

const (
	StatusPending          Status = "PENDING"
	StatusPendingReference Status = "PENDING_REFERENCE"
	StatusProcessed        Status = "PROCESSED"
	StatusRejected         Status = "REJECTED"
	StatusFailed           Status = "FAILED"
)

func ParseStatus(s string) (Status, error) {
	st := Status(s)
	switch st {
	case StatusPending, StatusPendingReference, StatusProcessed, StatusRejected, StatusFailed:
		return st, nil
	default:
		return "", validation(FailureInvalidStatus, ErrInvalidStatus)
	}
}

func (s Status) Terminal() bool {
	return s == StatusProcessed || s == StatusRejected || s == StatusFailed
}

// Direction is a ledger movement direction.
type Direction string

const (
	DirectionDebit  Direction = "DEBIT"
	DirectionCredit Direction = "CREDIT"
)

func ParseDirection(s string) (Direction, error) {
	d := Direction(s)
	switch d {
	case DirectionDebit, DirectionCredit:
		return d, nil
	default:
		return "", validation(FailureInvalidDirection, ErrInvalidDirection)
	}
}

// Origin distinguishes internal opening from provider operations.
type Origin string

const (
	OriginInternal Origin = "INTERNAL"
	OriginExternal Origin = "EXTERNAL"
)

func ParseOrigin(s string) (Origin, error) {
	o := Origin(s)
	switch o {
	case OriginInternal, OriginExternal:
		return o, nil
	default:
		return "", validation(FailureInvalidOrigin, ErrInvalidOrigin)
	}
}
