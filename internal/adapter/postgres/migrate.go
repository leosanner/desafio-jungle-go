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
	abs, err := filepath.Abs(migrationsPath)
	if err != nil {
		return fmt.Errorf("postgres: migrations path: %w", err)
	}
	sourceURL := (&url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(abs),
	}).String()

	databaseURL, err := pgx5DatabaseURL(dsn)
	if err != nil {
		return err
	}

	m, err := migrate.New(sourceURL, databaseURL)
	if err != nil {
		return fmt.Errorf("postgres: migrate open: %w", err)
	}
	defer func() {
		_, _ = m.Close()
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("postgres: migrate up: %w", err)
	}
	return nil
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
