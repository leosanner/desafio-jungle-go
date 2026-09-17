package app

import (
	"errors"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

func (s *Service) observeSubmit(kind domain.Kind, channel string, d time.Duration, out SubmitResult, err error) {
	if err != nil {
		s.observeSubmitError(err)
		return
	}
	if out.IdempotentReplay {
		s.metrics.IncDuplicate(string(kind), channel)
	}
	status := string(out.Transaction.Status())
	if status == "" {
		status = "unknown"
	}
	s.metrics.ObserveOperation(string(kind), status, channel, d)
}

func (s *Service) observeSubmitError(err error) {
	if errors.Is(err, ErrOptimisticLock) {
		s.metrics.IncConflict(ConflictOptimisticLock)
		return
	}
	if errors.Is(err, ErrUnavailable) {
		s.metrics.IncConflict(ConflictUnavailable)
		return
	}
	code, _, ok := domain.Classify(err)
	if !ok {
		return
	}
	switch code {
	case domain.FailureIdempotencyPayloadConflict:
		s.metrics.IncConflict(ConflictIdempotencyPayload)
	case domain.FailureDuplicateExternalTransaction:
		s.metrics.IncConflict(ConflictDuplicateExternal)
	}
}
