package httpserver

import (
	"context"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

type stubUoW struct{}

func (stubUoW) Within(ctx context.Context, fn func(context.Context, app.Repositories) error) error {
	return fn(ctx, app.Repositories{
		Wallets:      stubWallets{},
		Transactions: stubTransactions{},
		Ledger:       stubLedger{},
		Outbox:       stubOutbox{},
		Inbox:        stubInbox{},
	})
}

type stubWallets struct{}

func (stubWallets) GetByID(context.Context, string) (domain.Wallet, error) {
	return domain.Wallet{}, app.ErrNotFound
}
func (stubWallets) GetByIDForUpdate(context.Context, string) (domain.Wallet, error) {
	return domain.Wallet{}, app.ErrNotFound
}
func (stubWallets) GetByPlayerAndCurrency(context.Context, string, string) (domain.Wallet, error) {
	return domain.Wallet{}, app.ErrNotFound
}
func (stubWallets) Insert(context.Context, domain.Wallet) error { return nil }
func (stubWallets) Update(context.Context, domain.Wallet) error { return app.ErrNotFound }

type stubTransactions struct{}

func (stubTransactions) GetByID(context.Context, string) (domain.WagerTransaction, error) {
	return domain.WagerTransaction{}, app.ErrNotFound
}
func (stubTransactions) GetByProviderExternalID(context.Context, string, string) (domain.WagerTransaction, error) {
	return domain.WagerTransaction{}, app.ErrNotFound
}
func (stubTransactions) GetByProviderIdempotencyKey(context.Context, string, string) (domain.WagerTransaction, error) {
	return domain.WagerTransaction{}, app.ErrNotFound
}
func (stubTransactions) ListProcessedReversals(context.Context, string) ([]domain.WagerTransaction, error) {
	return nil, nil
}
func (stubTransactions) Insert(context.Context, domain.WagerTransaction) error { return nil }
func (stubTransactions) Update(context.Context, domain.WagerTransaction) error {
	return app.ErrNotFound
}

type stubLedger struct{}

func (stubLedger) Insert(context.Context, domain.WalletLedgerEntry) error { return nil }
func (stubLedger) ListByWallet(context.Context, string, time.Time, string, int) ([]domain.WalletLedgerEntry, error) {
	return nil, nil
}
func (stubLedger) SumByWallet(context.Context, string, string) (domain.Money, int, error) {
	return domain.Money{}, 0, nil
}

type stubOutbox struct{}

func (stubOutbox) Insert(context.Context, app.OutboxRecord) error { return nil }

type stubInbox struct{}

func (stubInbox) Get(context.Context, string, string) (app.InboxRecord, error) {
	return app.InboxRecord{}, app.ErrNotFound
}
func (stubInbox) Insert(context.Context, app.InboxRecord) error { return nil }

func stubService() *app.Service {
	return app.NewService(stubUoW{}, app.SystemClock{}, app.UUIDGenerator{}, app.NopMetrics{})
}
