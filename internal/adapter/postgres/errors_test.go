package postgres

import (
	"errors"
	"testing"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/leosanner/desafio-jungle-go/internal/app"
)

func TestMapError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want error
	}{
		{
			name: "no rows",
			err:  pgx.ErrNoRows,
			want: app.ErrNotFound,
		},
		{
			name: "unique violation",
			err:  &pgconn.PgError{Code: pgerrcode.UniqueViolation},
			want: app.ErrConflict,
		},
		{
			name: "check violation",
			err:  &pgconn.PgError{Code: pgerrcode.CheckViolation},
			want: ErrCheckViolation,
		},
		{
			name: "deadlock",
			err:  &pgconn.PgError{Code: pgerrcode.DeadlockDetected},
			want: app.ErrUnavailable,
		},
		{
			name: "serialization failure",
			err:  &pgconn.PgError{Code: pgerrcode.SerializationFailure},
			want: app.ErrUnavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := mapError(tc.err)
			if !errors.Is(got, tc.want) {
				t.Fatalf("errors.Is(%v, %v) = false", got, tc.want)
			}
			if !errors.Is(got, tc.err) {
				t.Fatalf("original error must remain unwrap-able: got %v, orig %v", got, tc.err)
			}
		})
	}
}

func TestMapErrorCheckViolationWrapsOriginalAndSentinel(t *testing.T) {
	t.Parallel()
	orig := &pgconn.PgError{Code: pgerrcode.CheckViolation}
	got := mapError(orig)
	if !errors.Is(got, ErrCheckViolation) {
		t.Fatalf("errors.Is(got, ErrCheckViolation) = false; got %v", got)
	}
	var pgErr *pgconn.PgError
	if !errors.As(got, &pgErr) {
		t.Fatal("original *pgconn.PgError must be unwrap-able")
	}
	if pgErr.Code != pgerrcode.CheckViolation {
		t.Fatalf("code = %q, want %q", pgErr.Code, pgerrcode.CheckViolation)
	}
}

func TestMapErrorDoesNotMatchMessageStrings(t *testing.T) {
	t.Parallel()
	// Same words as SQLSTATE names, but not a *pgconn.PgError — must not classify.
	messages := []error{
		errors.New("unique_violation"),
		errors.New("check constraint violation"),
		errors.New("deadlock detected"),
		errors.New("could not serialize access"),
		errors.New("no rows in result set"),
	}
	for _, err := range messages {
		t.Run(err.Error(), func(t *testing.T) {
			t.Parallel()
			got := mapError(err)
			if errors.Is(got, app.ErrNotFound) ||
				errors.Is(got, app.ErrConflict) ||
				errors.Is(got, ErrCheckViolation) ||
				errors.Is(got, ErrRetryable) {
				t.Fatalf("mapped %v by message string to a sentinel: %v", err, got)
			}
		})
	}
}

func TestMapErrorUnmappedSQLSTATE(t *testing.T) {
	t.Parallel()
	orig := &pgconn.PgError{Code: pgerrcode.ForeignKeyViolation}
	got := mapError(orig)
	if errors.Is(got, app.ErrNotFound) ||
		errors.Is(got, app.ErrConflict) ||
		errors.Is(got, ErrCheckViolation) ||
		errors.Is(got, ErrRetryable) {
		t.Fatalf("unmapped SQLSTATE classified as sentinel: %v", got)
	}
	var pgErr *pgconn.PgError
	if !errors.As(got, &pgErr) {
		t.Fatal("original *pgconn.PgError must still unwrap")
	}
}

func TestMapErrorNil(t *testing.T) {
	t.Parallel()
	if got := mapError(nil); got != nil {
		t.Fatalf("mapError(nil) = %v", got)
	}
}
