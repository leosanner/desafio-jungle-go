//go:build integration

package postgres

import (
	"testing"

	"github.com/leosanner/desafio-jungle-go/internal/config"
)

func TestMigrationsUpAndDown(t *testing.T) {
	t.Parallel()

	dsn := createIsolatedDatabase(t)
	path := migrationsDir(t)

	mdsn := migrateDSN(dsn)
	if err := RunUp(path, mdsn); err != nil {
		t.Fatalf("RunUp: %v", err)
	}

	pool, err := NewPool(config.Config{PostgresDSN: dsn})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(pool.Close)

	assertSchemaExists(t, pool)
	assertFinancialTables(t, pool, true)

	if err := RunSteps(path, mdsn, -1); err != nil {
		t.Fatalf("RunSteps(-1): %v", err)
	}

	assertSchemaExists(t, pool)
	assertFinancialTables(t, pool, false)

	if err := RunUp(path, mdsn); err != nil {
		t.Fatalf("RunUp again: %v", err)
	}

	assertSchemaExists(t, pool)
	assertFinancialTables(t, pool, true)
}
