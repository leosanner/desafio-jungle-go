//go:build integration

package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/config"
	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

var integrationNow = time.Date(2026, 9, 15, 20, 0, 0, 0, time.UTC)

type testDB struct {
	dsn  string
	pool *Pool
	uow  app.UnitOfWork
}

func requirePostgresDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN is not set")
	}
	return dsn
}

func migrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations"))
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("migrations path %s: %v", dir, err)
	}
	return dir
}

func uniqueSuffix(t *testing.T) string {
	t.Helper()
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b[:])
}

func uniqueID(t *testing.T, prefix string) string {
	t.Helper()
	return prefix + uniqueSuffix(t)
}

func dsnForDatabase(adminDSN, dbName string) (string, error) {
	u, err := url.Parse(adminDSN)
	if err != nil {
		return "", err
	}
	u.Path = "/" + dbName
	return u.String(), nil
}

// migrateDSN pins schema_migrations in public. After 000001 creates schema
// wagering, CURRENT_SCHEMA() becomes the role name ("wagering") and a new
// migrate session would miss public.schema_migrations (NilVersion → Steps
// returns "file does not exist").
func migrateDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	q := u.Query()
	q.Set("x-migrations-table", `"public"."schema_migrations"`)
	q.Set("x-migrations-table-quoted", "true")
	u.RawQuery = q.Encode()
	return u.String()
}

func withAdminConn(t *testing.T, adminDSN string, fn func(ctx context.Context, conn *pgx.Conn)) {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(adminDSN)
	if err != nil {
		t.Fatalf("parse admin dsn: %v", err)
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := pgx.ConnectConfig(ctx, cfg.ConnConfig)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	defer conn.Close(context.Background())
	fn(ctx, conn)
}

// createIsolatedDatabase creates wagering_it_<unique> and drops it in Cleanup.
// Callers must not migrate the shared compose database.
func createIsolatedDatabase(t *testing.T) string {
	t.Helper()
	adminDSN := requirePostgresDSN(t)
	dbName := "wagering_it_" + uniqueSuffix(t)
	ident := pgx.Identifier{dbName}.Sanitize()

	withAdminConn(t, adminDSN, func(ctx context.Context, conn *pgx.Conn) {
		if _, err := conn.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
			t.Fatalf("CREATE DATABASE %s: %v", dbName, err)
		}
	})

	t.Cleanup(func() {
		withAdminConn(t, adminDSN, func(ctx context.Context, conn *pgx.Conn) {
			_, _ = conn.Exec(ctx, `
				SELECT pg_terminate_backend(pid)
				FROM pg_stat_activity
				WHERE datname = $1 AND pid <> pg_backend_pid()`, dbName)
			if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+ident+" WITH (FORCE)"); err != nil {
				t.Errorf("DROP DATABASE %s: %v", dbName, err)
			}
		})
	})

	isolated, err := dsnForDatabase(adminDSN, dbName)
	if err != nil {
		t.Fatalf("isolated dsn: %v", err)
	}
	return isolated
}

func openMigratedDB(t *testing.T) *testDB {
	t.Helper()
	dsn := createIsolatedDatabase(t)
	if err := RunUp(migrationsDir(t), migrateDSN(dsn)); err != nil {
		t.Fatalf("RunUp: %v", err)
	}
	pool, err := NewPool(config.Config{PostgresDSN: dsn})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(pool.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return &testDB{dsn: dsn, pool: pool, uow: NewUnitOfWork(pool)}
}

func (db *testDB) exec(ctx context.Context, sql string, args ...any) error {
	_, err := db.pool.pool.Exec(ctx, sql, args...)
	return err
}

func (db *testDB) wallets() *walletRepo {
	return &walletRepo{q: db.pool.pool}
}

func (db *testDB) transactions() *transactionRepo {
	return &transactionRepo{q: db.pool.pool}
}

func (db *testDB) ledger() *ledgerRepo {
	return &ledgerRepo{q: db.pool.pool}
}

func mustParseMoney(t *testing.T, amount, currency string) domain.Money {
	t.Helper()
	m, err := domain.ParseMoney(amount, currency)
	if err != nil {
		t.Fatalf("ParseMoney(%q, %q): %v", amount, currency, err)
	}
	return m
}

func assertMoney(t *testing.T, got domain.Money, amount, currency string) {
	t.Helper()
	want := mustParseMoney(t, amount, currency)
	eq, err := got.Equal(want)
	if err != nil {
		t.Fatalf("Equal: %v", err)
	}
	if !eq {
		t.Fatalf("money = %s %s, want %s %s", got.AmountString(), got.Currency(), amount, currency)
	}
}

func persistOpening(t *testing.T, db *testDB, initial string) domain.Opening {
	t.Helper()
	opening, err := domain.OpenWallet(domain.OpenWalletParams{
		WalletID:    uniqueID(t, "wal-"),
		PlayerID:    uniqueID(t, "player-"),
		OpeningTxID: uniqueID(t, "tx-open-"),
		LedgerID:    uniqueID(t, "led-open-"),
		Initial:     mustParseMoney(t, initial, "BRL"),
		Now:         integrationNow,
	})
	if err != nil {
		t.Fatalf("OpenWallet: %v", err)
	}
	if err := db.uow.Within(t.Context(), persistOpeningFn(opening)); err != nil {
		t.Fatalf("persist opening: %v", err)
	}
	return opening
}

func persistOpeningFn(opening domain.Opening) func(context.Context, app.Repositories) error {
	return func(ctx context.Context, repos app.Repositories) error {
		if err := repos.Wallets.Insert(ctx, opening.Wallet); err != nil {
			return err
		}
		if opening.Transaction != nil {
			if err := repos.Transactions.Insert(ctx, *opening.Transaction); err != nil {
				return err
			}
		}
		if opening.Ledger != nil {
			if err := repos.Ledger.Insert(ctx, *opening.Ledger); err != nil {
				return err
			}
		}
		return nil
	}
}

func requireConstraint(t *testing.T, err error, sentinel error, constraint string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected constraint error, got nil")
	}
	mapped := mapError(err)
	if !errors.Is(mapped, sentinel) {
		t.Fatalf("errors.Is(%v, %v) = false", mapped, sentinel)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected *pgconn.PgError, got %T (%v)", err, err)
	}
	if pgErr.ConstraintName != constraint {
		t.Fatalf("constraint = %q, want %q (SQLSTATE %s: %s)", pgErr.ConstraintName, constraint, pgErr.Code, pgErr.Message)
	}
}

func seedWalletSQL(t *testing.T, db *testDB, balanceMinor int64) (walletID, playerID string) {
	t.Helper()
	walletID = uniqueID(t, "wal-")
	playerID = uniqueID(t, "player-")
	if err := db.exec(t.Context(), `
		INSERT INTO wagering.wallets (id, player_id, currency, balance_minor, version, created_at, updated_at)
		VALUES ($1, $2, 'BRL', $3, 1, $4, $4)`, walletID, playerID, balanceMinor, integrationNow); err != nil {
		t.Fatalf("seed wallet: %v", err)
	}
	return walletID, playerID
}

func seedOpeningTxSQL(t *testing.T, db *testDB, walletID, playerID string, amountMinor int64) string {
	t.Helper()
	txID := uniqueID(t, "tx-")
	if err := db.exec(t.Context(), `
		INSERT INTO wagering.wager_transactions (
			id, origin, wallet_id, player_id, kind, amount_minor, currency, status, created_at, updated_at
		) VALUES ($1, 'INTERNAL', $2, $3, 'OPENING', $4, 'BRL', 'PROCESSED', $5, $5)`,
		txID, walletID, playerID, amountMinor, integrationNow); err != nil {
		t.Fatalf("seed opening tx: %v", err)
	}
	return txID
}

func seedExternalTxSQL(t *testing.T, db *testDB, walletID, playerID, kind string, amountMinor int64, provider, extID, key string) string {
	t.Helper()
	txID := uniqueID(t, "tx-")
	hash := uniqueID(t, "hash-")
	if err := db.exec(t.Context(), `
		INSERT INTO wagering.wager_transactions (
			id, origin, provider_id, external_transaction_id, idempotency_key, payload_hash,
			wallet_id, player_id, round_id, game_id, kind, amount_minor, currency, status, created_at, updated_at
		) VALUES ($1, 'EXTERNAL', $2, $3, $4, $5, $6, $7, 'round-1', 'game-1', $8, $9, 'BRL', 'PENDING', $10, $10)`,
		txID, provider, extID, key, hash, walletID, playerID, kind, amountMinor, integrationNow); err != nil {
		t.Fatalf("seed external tx: %v", err)
	}
	return txID
}

func seedLedgerSQL(t *testing.T, db *testDB, walletID, txID, direction string, amount, before, after int64) string {
	t.Helper()
	id := uniqueID(t, "led-")
	if err := db.exec(t.Context(), `
		INSERT INTO wagering.wallet_ledger_entries (
			id, wallet_id, transaction_id, direction, amount_minor, currency,
			balance_before_minor, balance_after_minor, created_at
		) VALUES ($1, $2, $3, $4, $5, 'BRL', $6, $7, $8)`,
		id, walletID, txID, direction, amount, before, after, integrationNow); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}
	return id
}

func assertLedgerReconcilesBalance(t *testing.T, db *testDB, wallet domain.Wallet) {
	t.Helper()
	entries, err := db.ledger().ListByWallet(t.Context(), wallet.ID(), time.Time{}, "", 100)
	if err != nil {
		t.Fatalf("ListByWallet: %v", err)
	}
	sum, err := domain.Zero(wallet.Currency())
	if err != nil {
		t.Fatalf("Zero: %v", err)
	}
	for _, e := range entries {
		switch e.Direction() {
		case domain.DirectionCredit:
			sum, err = sum.Add(e.Money())
		case domain.DirectionDebit:
			sum, err = sum.Sub(e.Money())
		default:
			t.Fatalf("unexpected direction %s", e.Direction())
		}
		if err != nil {
			t.Fatalf("ledger sum: %v", err)
		}
	}
	eq, err := wallet.Balance().Equal(sum)
	if err != nil {
		t.Fatalf("Equal: %v", err)
	}
	if !eq {
		t.Fatalf("wallet balance %s %s != Σcredits − Σdebits %s %s (%d entries)",
			wallet.Balance().AmountString(), wallet.Balance().Currency(),
			sum.AmountString(), sum.Currency(), len(entries))
	}
}

func assertSchemaExists(t *testing.T, pool *Pool) {
	t.Helper()
	var exists bool
	if err := pool.pool.QueryRow(t.Context(), `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.schemata WHERE schema_name = 'wagering'
		)`).Scan(&exists); err != nil {
		t.Fatalf("schema exists: %v", err)
	}
	if !exists {
		t.Fatal("schema wagering should exist")
	}
}

func assertFinancialTables(t *testing.T, pool *Pool, want bool) {
	t.Helper()
	for _, name := range []string{"wallets", "wager_transactions", "wallet_ledger_entries"} {
		var exists bool
		if err := pool.pool.QueryRow(t.Context(), `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = 'wagering' AND table_name = $1
			)`, name).Scan(&exists); err != nil {
			t.Fatalf("table %s exists: %v", name, err)
		}
		if exists != want {
			t.Errorf("table wagering.%s exists=%v, want %v", name, exists, want)
		}
	}
}

func assertOutboxTable(t *testing.T, pool *Pool, want bool) {
	t.Helper()
	var exists bool
	if err := pool.pool.QueryRow(t.Context(), `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'wagering' AND table_name = 'outbox_events'
		)`).Scan(&exists); err != nil {
		t.Fatalf("outbox table exists: %v", err)
	}
	if exists != want {
		t.Errorf("table wagering.outbox_events exists=%v, want %v", exists, want)
	}
}

func assertInboxTable(t *testing.T, pool *Pool, want bool) {
	t.Helper()
	var exists bool
	if err := pool.pool.QueryRow(t.Context(), `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'wagering' AND table_name = 'inbox_messages'
		)`).Scan(&exists); err != nil {
		t.Fatalf("inbox table exists: %v", err)
	}
	if exists != want {
		t.Errorf("table wagering.inbox_messages exists=%v, want %v", exists, want)
	}
}
