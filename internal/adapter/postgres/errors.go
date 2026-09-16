package postgres

import (
	"errors"
	"fmt"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/leosanner/desafio-jungle-go/internal/app"
)

// Sentinel errors for SQLSTATE values that are not already expressed as app ports.
var (
	// ErrCheckViolation is a CHECK constraint failure (SQLSTATE 23514), including
	// non-negative balance and ledger invariants enforced by the schema.
	ErrCheckViolation = errors.New("check constraint violation")
	// ErrRetryable is a deadlock (40P01) or serialization failure (40001).
	ErrRetryable = errors.New("retryable database error")
)

// mapError classifies pgx/pgconn failures using SQLSTATE. It never inspects
// error message strings. Mapped sentinels and the original error remain Is-able.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("postgres: %w: %w", app.ErrNotFound, err)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgerrcode.UniqueViolation:
			return fmt.Errorf("postgres: %w: %w", app.ErrConflict, err)
		case pgerrcode.CheckViolation:
			return fmt.Errorf("postgres: %w: %w", ErrCheckViolation, err)
		case pgerrcode.DeadlockDetected, pgerrcode.SerializationFailure:
			return fmt.Errorf("postgres: %w: %w: %w", app.ErrUnavailable, ErrRetryable, err)
		}
	}
	return fmt.Errorf("postgres: %w", err)
}
