package postgres

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"

	// golang-migrate's pgx/v5 driver registers the "pgx5" URL scheme.
	// The application DSN uses postgres://; RunUp rewrites it to pgx5://.
	// A separate migrate connection is opened so migrate.Close cannot
	// shut down the process pool. Using database/postgres (lib/pq) would
	// accept postgres:// as-is but would add a second driver for Phase 1.
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// RunUp applies all pending migrations from migrationsPath.
// migrate.ErrNoChange is treated as success (already up to date, or no files).
func RunUp(migrationsPath, dsn string) error {
	m, err := openMigrate(migrationsPath, dsn)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = m.Close()
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("postgres: migrate up: %w", err)
	}
	return nil
}

// RunSteps applies n migration steps (positive = up, negative = down).
// migrate.ErrNoChange is treated as success.
func RunSteps(migrationsPath, dsn string, n int) error {
	m, err := openMigrate(migrationsPath, dsn)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = m.Close()
	}()

	if err := m.Steps(n); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("postgres: migrate steps: %w", err)
	}
	return nil
}

func openMigrate(migrationsPath, dsn string) (*migrate.Migrate, error) {
	abs, err := filepath.Abs(migrationsPath)
	if err != nil {
		return nil, fmt.Errorf("postgres: migrations path: %w", err)
	}
	sourceURL := (&url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(abs),
	}).String()

	databaseURL, err := pgx5DatabaseURL(dsn)
	if err != nil {
		return nil, err
	}
	databaseURL, err = pinMigrationsTable(databaseURL)
	if err != nil {
		return nil, err
	}

	m, err := migrate.New(sourceURL, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("postgres: migrate open: %w", err)
	}
	return m, nil
}

// pinMigrationsTable keeps schema_migrations in public. The DB role is
// "wagering"; after 000001 creates schema wagering, search_path "$user", public
// would otherwise resolve CURRENT_SCHEMA to wagering and a later migrate
// session would miss public.schema_migrations.
func pinMigrationsTable(databaseURL string) (string, error) {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return "", fmt.Errorf("postgres: parse migrate url: %w", err)
	}
	q := u.Query()
	if q.Get("x-migrations-table") == "" {
		q.Set("x-migrations-table", `"public"."schema_migrations"`)
		q.Set("x-migrations-table-quoted", "true")
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func pgx5DatabaseURL(dsn string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("postgres: parse dsn: %w", err)
	}
	switch u.Scheme {
	case "postgres", "postgresql", "pgx", "pgx5":
		u.Scheme = "pgx5"
	default:
		return "", fmt.Errorf("postgres: unsupported dsn scheme %q", u.Scheme)
	}
	return u.String(), nil
}
