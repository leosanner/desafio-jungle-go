package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/leosanner/desafio-jungle-go/internal/config"
)

// Pool wraps a pgx connection pool. The domain never imports this package.
type Pool struct {
	pool *pgxpool.Pool
}

// NewPool opens a pgxpool. There is no request context at construction;
// ping and migrations run on Fx OnStart.
func NewPool(cfg config.Config) (*Pool, error) {
	pool, err := pgxpool.New(context.Background(), cfg.PostgresDSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: open pool: %w", err)
	}
	return &Pool{pool: pool}, nil
}

// Ping checks that the database is reachable.
func (p *Pool) Ping(ctx context.Context) error {
	if err := p.pool.Ping(ctx); err != nil {
		return fmt.Errorf("postgres: ping: %w", err)
	}
	return nil
}

// Close releases pool resources.
func (p *Pool) Close() {
	p.pool.Close()
}

// Name identifies this dependency in readiness checks.
func (p *Pool) Name() string {
	return "postgres"
}

// Check implements readiness by pinging the pool.
func (p *Pool) Check(ctx context.Context) error {
	return p.Ping(ctx)
}
