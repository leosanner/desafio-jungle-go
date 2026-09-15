package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

// querier is satisfied by pgx.Tx so every repository method stays on the
// unit-of-work transaction. Repositories never open their own transactions.
type querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type scanner interface {
	Scan(dest ...any) error
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// moneyFromMinorScan trims CHAR(3) padding before constructing Money.
func moneyFromMinorScan(minor int64, currency string) (domain.Money, error) {
	m, err := domain.MoneyFromMinor(minor, strings.TrimSpace(currency))
	if err != nil {
		return domain.Money{}, fmt.Errorf("postgres: money: %w", err)
	}
	return m, nil
}
