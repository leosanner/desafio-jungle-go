package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

// InboxConsumerWagerTransactions is the durable consumer_name for inbound SQS (ADR 0017).
const InboxConsumerWagerTransactions = "wager-transactions"

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
	// ErrUnavailable is a retryable infrastructure failure (deadlock, serialization).
	ErrUnavailable = errors.New("unavailable")
	// ErrInboxHashMismatch means a redelivered envelope messageId has a different body hash.
	ErrInboxHashMismatch = errors.New("inbox payload hash mismatch")
)

// Clock is injected so tests do not depend on wall time.
type Clock interface {
	Now() time.Time
}

// IDGenerator assigns stable unique identifiers (UUID v7 in production).
type IDGenerator interface {
	NewID() string
}

// Metrics is a narrow Phase 5 port; full observability is Phase 10.
type Metrics interface {
	IncReconciliationDivergence()
}

// NopMetrics discards metric increments.
type NopMetrics struct{}

func (NopMetrics) IncReconciliationDivergence() {}

// UnitOfWork is the SQL transaction boundary without leaking driver types.
type UnitOfWork interface {
	// Within runs fn in a single SQL transaction. Commit if fn returns nil;
	// rollback on error or panic. Repositories in Repositories MUST share that
	// transaction so wallet, ledger, transaction, outbox and inbox writes commit atomically.
	Within(ctx context.Context, fn func(ctx context.Context, repos Repositories) error) error
}

// Repositories groups persistence ports that share the transaction opened by
// UnitOfWork.Within. Inbox joins the same commit for SQS HandleInbound (ADR 0017).
type Repositories struct {
	Wallets      WalletRepository
	Transactions TransactionRepository
	Ledger       LedgerRepository
	Outbox       OutboxRepository
	Inbox        InboxRepository
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
	// GetByIDForUpdate locks the row so resume cannot race a concurrent Apply
	// after the wallet lock is held (ADR 0019).
	GetByIDForUpdate(ctx context.Context, id string) (domain.WagerTransaction, error) // ErrNotFound
	GetByProviderExternalID(ctx context.Context, providerID, externalID string) (domain.WagerTransaction, error)
	GetByProviderIdempotencyKey(ctx context.Context, providerID, key string) (domain.WagerTransaction, error)
	// ListProcessedReversals returns PROCESSED REFUND/ROLLBACK rows that resolved
	// to the given internal transaction id.
	ListProcessedReversals(ctx context.Context, resolvedReferenceID string) ([]domain.WagerTransaction, error)
	Insert(ctx context.Context, tx domain.WagerTransaction) error // ErrConflict on unique identity
	Update(ctx context.Context, tx domain.WagerTransaction) error
}

// LedgerRepository appends and lists immutable wallet ledger entries.
type LedgerRepository interface {
	Insert(ctx context.Context, e domain.WalletLedgerEntry) error // ErrConflict on (wallet_id, transaction_id)
	// ListByWallet returns entries in stable order (created_at ASC, id ASC).
	// If id != "", the cursor (createdAt, id) is exclusive: (created_at, id) > cursor.
	// If id == "", listing starts from the beginning. limit must be > 0; the adapter
	// may cap it.
	ListByWallet(ctx context.Context, walletID string, createdAt time.Time, id string, limit int) ([]domain.WalletLedgerEntry, error)
	// SumByWallet returns Σcredits − Σdebits in currency and the entry count.
	// An empty ledger returns a zero amount in currency and 0 entries.
	SumByWallet(ctx context.Context, walletID, currency string) (domain.Money, int, error)
}

// OutboxRecord is an unpublished domain event snapshot (ADR 0014 / ADR 0016).
type OutboxRecord struct {
	EventID       string
	EventType     string
	EventVersion  int
	AggregateID   string
	CorrelationID string
	CausationID   string
	OccurredAt    time.Time
	Payload       []byte
	Attempts      int
}

// Envelope is the published outbox message (OBX-06, ADR 0015).
type Envelope struct {
	EventID       string          `json:"eventId"`
	EventType     string          `json:"eventType"`
	AggregateID   string          `json:"aggregateId"`
	CorrelationID string          `json:"correlationId"`
	CausationID   string          `json:"causationId,omitempty"`
	OccurredAt    time.Time       `json:"occurredAt"`
	Version       int             `json:"version"`
	Data          json.RawMessage `json:"data"`
}

// InboxRecord is a completed inbound message (ADR 0017).
type InboxRecord struct {
	ConsumerName string
	MessageID    string
	PayloadHash  string
	ReceivedAt   time.Time
	CompletedAt  time.Time
}

// InboxRepository persists completed inbound messages in the unit-of-work transaction.
type InboxRepository interface {
	Get(ctx context.Context, consumerName, messageID string) (InboxRecord, error) // ErrNotFound
	Insert(ctx context.Context, rec InboxRecord) error                            // ErrConflict
}

// OutboxRepository inserts unpublished event rows in the unit-of-work transaction.
type OutboxRepository interface {
	Insert(ctx context.Context, rec OutboxRecord) error
}

// OutboxClaimer claims and acknowledges unpublished rows outside the financial unit of work.
type OutboxClaimer interface {
	Claim(ctx context.Context, limit int, now time.Time, lease time.Duration) ([]OutboxRecord, error)
	MarkPublished(ctx context.Context, eventID string, at time.Time) error
	ScheduleRetry(ctx context.Context, eventID string, nextAttempt time.Time) error
}

// PendingWork is one claimed PENDING / PENDING_REFERENCE row (ADR 0019).
type PendingWork struct {
	TransactionID string
	WalletID      string
	Attempts      int
	CreatedAt     time.Time
	Status        domain.Status
}

// PendingClaimer claims due pending rows outside the financial unit of work.
type PendingClaimer interface {
	Claim(ctx context.Context, limit int, now time.Time, lease time.Duration) ([]PendingWork, error)
	ScheduleRetry(ctx context.Context, transactionID string, nextAttempt time.Time) error
}

// EventBus publishes a wire envelope to the configured destination.
type EventBus interface {
	Publish(ctx context.Context, env Envelope) error
}
