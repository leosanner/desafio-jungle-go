package sqsadapter

import (
	"errors"

	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

// Disposition is the SQS action after HandleInbound / parse (ADR 0018).
type Disposition int

const (
	DispositionAck Disposition = iota
	DispositionRetry
	DispositionDLQ
)

// DispositionOf maps a processing error to ack, visibility backoff, or DLQ.
func DispositionOf(err error) Disposition {
	if err == nil {
		return DispositionAck
	}
	if errors.Is(err, errInvalidMessage) || errors.Is(err, app.ErrInboxHashMismatch) {
		return DispositionDLQ
	}
	if errors.Is(err, app.ErrUnavailable) || errors.Is(err, app.ErrNotFound) {
		return DispositionRetry
	}
	if errors.Is(err, app.ErrForbidden) {
		return DispositionDLQ
	}
	_, class, ok := domain.Classify(err)
	if ok {
		switch class {
		case domain.FailureClassValidation, domain.FailureClassRejection, domain.FailureClassPermanent:
			return DispositionDLQ
		case domain.FailureClassTransient:
			return DispositionRetry
		}
	}
	return DispositionRetry
}
