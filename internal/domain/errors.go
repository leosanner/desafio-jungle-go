package domain

import (
	"errors"
	"fmt"
)

// FailureClass distinguishes how adapters should treat a domain error.
// Mapping to HTTP / SQS lives in adapters, not here.
type FailureClass int

const (
	// FailureClassValidation is malformed or disallowed input. No financial effect.
	FailureClassValidation FailureClass = iota
	// FailureClassRejection is a business rule. Persist REJECTED with FailureCode.
	FailureClassRejection
	// FailureClassWait means a reference is not usable yet. Persist PENDING_REFERENCE.
	FailureClassWait
	// FailureClassTransient is retryable infrastructure (not raised by Apply).
	FailureClassTransient
	// FailureClassPermanent is unrecoverable infrastructure (MarkFailed).
	FailureClassPermanent
)

// FailureCode is a stable, documented code persisted on rejected transactions.
type FailureCode string

const (
	FailureInvalidAmount                FailureCode = "INVALID_AMOUNT"
	FailureInvalidCurrency              FailureCode = "INVALID_CURRENCY"
	FailureIncompatibleCurrency         FailureCode = "INCOMPATIBLE_CURRENCY"
	FailureUninitialized                FailureCode = "UNINITIALIZED"
	FailureOverflow                     FailureCode = "OVERFLOW"
	FailureInsufficientFunds            FailureCode = "INSUFFICIENT_FUNDS"
	FailureInsufficientFundsReversal    FailureCode = "INSUFFICIENT_FUNDS_REVERSAL"
	FailurePositiveAmountRequired       FailureCode = "POSITIVE_AMOUNT_REQUIRED"
	FailureZeroAmountRequired           FailureCode = "ZERO_AMOUNT_REQUIRED"
	FailureOpeningNotAllowed            FailureCode = "OPENING_NOT_ALLOWED"
	FailureMissingReference             FailureCode = "MISSING_REFERENCE"
	FailureReferenceMismatch            FailureCode = "REFERENCE_MISMATCH"
	FailureReferenceKindInvalid         FailureCode = "REFERENCE_KIND_INVALID"
	FailureReferenceUnsuccessful        FailureCode = "REFERENCE_UNSUCCESSFUL"
	FailureDuplicateReversal            FailureCode = "DUPLICATE_REVERSAL"
	FailureInvalidTransition            FailureCode = "INVALID_TRANSITION"
	FailureInvalidKind                  FailureCode = "INVALID_KIND"
	FailureInvalidStatus                FailureCode = "INVALID_STATUS"
	FailureInvalidDirection             FailureCode = "INVALID_DIRECTION"
	FailureInvalidOrigin                FailureCode = "INVALID_ORIGIN"
	FailureLedgerInvariant              FailureCode = "LEDGER_INVARIANT"
	FailureIdempotencyPayloadConflict   FailureCode = "IDEMPOTENCY_PAYLOAD_CONFLICT"
	FailureDuplicateExternalTransaction FailureCode = "DUPLICATE_EXTERNAL_TRANSACTION"
	FailureMissingIdentity              FailureCode = "MISSING_IDENTITY"
	FailureJSONAmountNotString          FailureCode = "JSON_AMOUNT_NOT_STRING"
)

// IsCorrectable reports whether a client can fix the input and retry with a new operation.
// Definitive codes mean the recorded outcome should not be treated as a typo to correct in place.
func (c FailureCode) IsCorrectable() bool {
	switch c {
	case FailureInvalidAmount,
		FailureInvalidCurrency,
		FailureIncompatibleCurrency,
		FailureUninitialized,
		FailurePositiveAmountRequired,
		FailureZeroAmountRequired,
		FailureOpeningNotAllowed,
		FailureMissingReference,
		FailureInvalidKind,
		FailureInvalidStatus,
		FailureInvalidDirection,
		FailureInvalidOrigin,
		FailureJSONAmountNotString,
		FailureMissingIdentity:
		return true
	default:
		return false
	}
}

// Sentinel errors. Wrap with Classified or fmt.Errorf("%w", ...).
var (
	ErrUninitializedMoney           = errors.New("money is uninitialized")
	ErrInvalidAmount                = errors.New("invalid monetary amount")
	ErrInvalidCurrency              = errors.New("invalid currency code")
	ErrIncompatibleCurrency         = errors.New("incompatible currencies")
	ErrOverflow                     = errors.New("monetary overflow")
	ErrInsufficientFunds            = errors.New("insufficient funds")
	ErrInsufficientFundsReversal    = errors.New("insufficient funds for reversal")
	ErrPositiveAmountRequired       = errors.New("positive amount required")
	ErrZeroAmountRequired           = errors.New("zero amount required")
	ErrOpeningNotAllowed            = errors.New("OPENING is reserved for internal wallet opening")
	ErrMissingReference             = errors.New("reference is required")
	ErrReferenceMismatch            = errors.New("operation does not match its reference")
	ErrReferenceKindInvalid         = errors.New("reference kind is not valid for this operation")
	ErrReferenceUnsuccessful        = errors.New("reference did not complete successfully")
	ErrReferencePending             = errors.New("reference is not yet available")
	ErrDuplicateReversal            = errors.New("reference already has a successful reversal of this kind")
	ErrInvalidTransition            = errors.New("invalid status transition")
	ErrTerminalState                = errors.New("terminal transaction cannot change status")
	ErrInvalidKind                  = errors.New("invalid operation kind")
	ErrInvalidStatus                = errors.New("invalid transaction status")
	ErrInvalidDirection             = errors.New("invalid ledger direction")
	ErrInvalidOrigin                = errors.New("invalid transaction origin")
	ErrLedgerInvariant              = errors.New("ledger balance invariant violated")
	ErrIdempotencyPayloadConflict   = errors.New("idempotency key reused with a different payload")
	ErrDuplicateExternalTransaction = errors.New("external transaction already recorded under a different idempotency key")
	ErrMissingIdentity              = errors.New("required identifier is missing")
	ErrJSONAmountNotString          = errors.New("money amount must be a JSON string")
)

// ClassifiedError is a domain error with a stable failure code and class.
type ClassifiedError struct {
	Code  FailureCode
	Class FailureClass
	Err   error
}

func (e *ClassifiedError) Error() string {
	if e == nil || e.Err == nil {
		return "domain error"
	}
	if e.Code == "" {
		return e.Err.Error()
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Err.Error())
}

func (e *ClassifiedError) Unwrap() error { return e.Err }

func classified(code FailureCode, class FailureClass, err error) error {
	return &ClassifiedError{Code: code, Class: class, Err: err}
}

func validation(code FailureCode, err error) error {
	return classified(code, FailureClassValidation, err)
}

func rejection(code FailureCode, err error) error {
	return classified(code, FailureClassRejection, err)
}

func wait(err error) error {
	return classified("", FailureClassWait, err)
}

// Classify extracts FailureCode and FailureClass from err.
func Classify(err error) (FailureCode, FailureClass, bool) {
	var ce *ClassifiedError
	if !errors.As(err, &ce) {
		return "", 0, false
	}
	return ce.Code, ce.Class, true
}
