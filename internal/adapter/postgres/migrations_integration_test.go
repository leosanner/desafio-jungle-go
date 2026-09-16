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
	assertOutboxTable(t, pool, true)
	assertInboxTable(t, pool, true)

	if err := RunSteps(path, mdsn, -1); err != nil {
		t.Fatalf("RunSteps(-1): %v", err)
	}

	assertSchemaExists(t, pool)
	assertInboxTable(t, pool, false)
	assertOutboxTable(t, pool, true)
	assertFinancialTables(t, pool, true)

	if err := RunSteps(path, mdsn, -1); err != nil {
		t.Fatalf("RunSteps(-1) outbox: %v", err)
	}

	assertSchemaExists(t, pool)
	assertOutboxTable(t, pool, false)
	assertFinancialTables(t, pool, true)

	if err := RunSteps(path, mdsn, -1); err != nil {
		t.Fatalf("RunSteps(-1) financial: %v", err)
	}

	assertSchemaExists(t, pool)
	assertFinancialTables(t, pool, false)

	if err := RunUp(path, mdsn); err != nil {
		t.Fatalf("RunUp again: %v", err)
	}

	assertSchemaExists(t, pool)
	assertFinancialTables(t, pool, true)
	assertOutboxTable(t, pool, true)
	assertInboxTable(t, pool, true)
}
