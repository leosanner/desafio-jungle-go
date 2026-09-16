package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

const (
	defaultLedgerLimit = 50
	maxLedgerLimit     = 100
)

// LedgerPage is a cursor page of ledger entries.
type LedgerPage struct {
	Entries    []domain.WalletLedgerEntry
	NextCursor string
}

// GetWallet loads a wallet by id.
func (s *Service) GetWallet(ctx context.Context, id string) (domain.Wallet, error) {
	var w domain.Wallet
	err := s.uow.Within(ctx, func(ctx context.Context, repos Repositories) error {
		got, err := repos.Wallets.GetByID(ctx, id)
		if err != nil {
			return err
		}
		w = got
		return nil
	})
	return w, err
}

// ListLedger returns a page of ledger entries. cursor is opaque base64url of created_at|id.
func (s *Service) ListLedger(ctx context.Context, walletID, cursor string, limit int) (LedgerPage, error) {
	if limit <= 0 {
		limit = defaultLedgerLimit
	}
	if limit > maxLedgerLimit {
		limit = maxLedgerLimit
	}
	createdAt, id, err := decodeLedgerCursor(cursor)
	if err != nil {
		return LedgerPage{}, err
	}

	var page LedgerPage
	err = s.uow.Within(ctx, func(ctx context.Context, repos Repositories) error {
		if _, err := repos.Wallets.GetByID(ctx, walletID); err != nil {
			return err
		}
		// Fetch one extra row to detect a following page.
		items, err := repos.Ledger.ListByWallet(ctx, walletID, createdAt, id, limit+1)
		if err != nil {
			return err
		}
		if len(items) > limit {
			last := items[limit-1]
			page.NextCursor = encodeLedgerCursor(last.CreatedAt(), last.ID())
			items = items[:limit]
		}
		page.Entries = items
		return nil
	})
	return page, err
}

// GetTransaction returns a transaction if the actor may see it.
// Other-provider access is ErrNotFound so HTTP can 404 without leaking existence.
func (s *Service) GetTransaction(ctx context.Context, actor Actor, id string) (domain.WagerTransaction, error) {
	var tx domain.WagerTransaction
	err := s.uow.Within(ctx, func(ctx context.Context, repos Repositories) error {
		got, err := repos.Transactions.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if !canReadTransaction(actor, got) {
			return ErrNotFound
		}
		tx = got
		return nil
	})
	return tx, err
}

// GetByProviderExternalID loads a provider-scoped transaction.
func (s *Service) GetByProviderExternalID(ctx context.Context, actor Actor, providerID, externalID string) (domain.WagerTransaction, error) {
	if !actor.CanAccessProvider(providerID) {
		return domain.WagerTransaction{}, ErrForbidden
	}
	var tx domain.WagerTransaction
	err := s.uow.Within(ctx, func(ctx context.Context, repos Repositories) error {
		got, err := repos.Transactions.GetByProviderExternalID(ctx, providerID, externalID)
		if err != nil {
			return err
		}
		tx = got
		return nil
	})
	return tx, err
}

func canReadTransaction(actor Actor, tx domain.WagerTransaction) bool {
	if actor.Internal {
		return true
	}
	return tx.ProviderID() != "" && actor.CanAccessProvider(tx.ProviderID())
}

func encodeLedgerCursor(createdAt time.Time, id string) string {
	raw := createdAt.UTC().Format(time.RFC3339Nano) + "|" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeLedgerCursor(cursor string) (time.Time, string, error) {
	if cursor == "" {
		return time.Time{}, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", domain.NewValidation(domain.FailureMissingIdentity, fmt.Errorf("invalid cursor"))
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 || parts[1] == "" {
		return time.Time{}, "", domain.NewValidation(domain.FailureMissingIdentity, fmt.Errorf("invalid cursor"))
	}
	ts, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", domain.NewValidation(domain.FailureMissingIdentity, fmt.Errorf("invalid cursor"))
	}
	return ts, parts[1], nil
}

// ParseLimit converts a query limit; empty uses the default.
func ParseLimit(raw string) (int, error) {
	if raw == "" {
		return defaultLedgerLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, domain.NewValidation(domain.FailureMissingIdentity, fmt.Errorf("invalid limit"))
	}
	return n, nil
}
