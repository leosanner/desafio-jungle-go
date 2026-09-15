package app

import (
	"context"
	"errors"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

// Sentinel errors for persistence mapping. Adapters wrap driver errors with %w
// so callers can use errors.Is without depending on pgx or database/sql.
var (
	// ErrNotFound means the requested row does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict is a unique-constraint violation (wallet (player_id, currency),
	// idempotency key, external id, opening, reversal, or ledger (wallet_id, transaction_id)).
	ErrConflict = errors.New("conflict")
	// ErrOptimisticLock means UPDATE ... WHERE id AND version = $n affected 0 rows.
	ErrOptimisticLock = errors.New("optimistic lock failure")
)

// UnitOfWork is the SQL transaction boundary without leaking driver types.
// Inbox and outbox ports will join Repositories later; Within stays unchanged.
type UnitOfWork interface {
	// Within runs fn in a single SQL transaction. Commit if fn returns nil;
	// rollback on error or panic. Repositories in Repositories MUST share that
	// transaction so wallet, ledger and transaction writes commit atomically.
	Within(ctx context.Context, fn func(ctx context.Context, repos Repositories) error) error
}

// Repositories groups persistence ports that share the transaction opened by
// UnitOfWork.Within. Inbox and outbox fields will be added here later without
// changing Within's signature.
type Repositories struct {
	Wallets      WalletRepository
	Transactions TransactionRepository
	Ledger       LedgerRepository
}

// WalletRepository loads and persists Wallet aggregates.
type WalletRepository interface {
	GetByID(ctx context.Context, id string) (domain.Wallet, error) // ErrNotFound
	// GetByIDForUpdate locks the row (SELECT FOR UPDATE) so concurrent debit/credit
	// on the same wallet serialize without a global lock.
	GetByIDForUpdate(ctx context.Context, id string) (domain.Wallet, error)                       // ErrNotFound
	GetByPlayerAndCurrency(ctx context.Context, playerID, currency string) (domain.Wallet, error) // ErrNotFound
	Insert(ctx context.Context, w domain.Wallet) error                                            // ErrConflict on (player_id, currency)
	// Update persists a wallet after an in-memory mutation (Debit/Credit).
	// domain.Wallet only exposes Version() after that increment, not the previous
	// value. The adapter MUST UPDATE ... WHERE id = $id AND version = $newVersion-1
	// (the persisted version before this mutation). Zero affected rows maps to
	// ErrOptimisticLock.
	Update(ctx context.Context, w domain.Wallet) error
}

// TransactionRepository loads and persists WagerTransaction rows.
type TransactionRepository interface {
	GetByID(ctx context.Context, id string) (domain.WagerTransaction, error) // ErrNotFound
	GetByProviderExternalID(ctx context.Context, providerID, externalID string) (domain.WagerTransaction, error)
	GetByProviderIdempotencyKey(ctx context.Context, providerID, key string) (domain.WagerTransaction, error)
	Insert(ctx context.Context, tx domain.WagerTransaction) error // ErrConflict on unique identity
	Update(ctx context.Context, tx domain.WagerTransaction) error
}

// LedgerRepository appends and lists immutable wallet ledger entries.
type LedgerRepository interface {
	Insert(ctx context.Context, e domain.WalletLedgerEntry) error // ErrConflict on (wallet_id, transaction_id)
	// ListByWallet returns entries in stable order (created_at ASC, id ASC).
	// If id != "", the cursor (createdAt, id) is exclusive: (created_at, id) > cursor.
	// If id == "", listing starts from the beginning. limit must be > 0; the adapter
	// may cap it. HTTP opaque cursors are deferred to the HTTP layer.
	ListByWallet(ctx context.Context, walletID string, createdAt time.Time, id string, limit int) ([]domain.WalletLedgerEntry, error)
}
