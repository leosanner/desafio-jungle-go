package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

type memStore struct {
	mu        sync.Mutex
	wallets   map[string]domain.Wallet
	playerKey map[string]string
	txs       map[string]domain.WagerTransaction
	byKey     map[string]string
	byExt     map[string]string
	ledger    []domain.WalletLedgerEntry
	outbox    []OutboxRecord
	inbox     map[string]InboxRecord
}

func newMemStore() *memStore {
	return &memStore{
		wallets:   map[string]domain.Wallet{},
		playerKey: map[string]string{},
		txs:       map[string]domain.WagerTransaction{},
		byKey:     map[string]string{},
		byExt:     map[string]string{},
		inbox:     map[string]InboxRecord{},
	}
}

func (m *memStore) Within(ctx context.Context, fn func(context.Context, Repositories) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fn(ctx, Repositories{
		Wallets:      memWallets{m},
		Transactions: memTx{m},
		Ledger:       memLedger{m},
		Outbox:       memOutbox{m},
		Inbox:        memInbox{m},
	})
}

type memWallets struct{ s *memStore }

func (r memWallets) GetByID(_ context.Context, id string) (domain.Wallet, error) {
	w, ok := r.s.wallets[id]
	if !ok {
		return domain.Wallet{}, ErrNotFound
	}
	return w, nil
}

func (r memWallets) GetByIDForUpdate(ctx context.Context, id string) (domain.Wallet, error) {
	return r.GetByID(ctx, id)
}

func (r memWallets) GetByPlayerAndCurrency(_ context.Context, playerID, currency string) (domain.Wallet, error) {
	id, ok := r.s.playerKey[playerID+"|"+currency]
	if !ok {
		return domain.Wallet{}, ErrNotFound
	}
	return r.s.wallets[id], nil
}

func (r memWallets) Insert(_ context.Context, w domain.Wallet) error {
	k := w.PlayerID() + "|" + w.Currency()
	if _, ok := r.s.playerKey[k]; ok {
		return ErrConflict
	}
	if _, ok := r.s.wallets[w.ID()]; ok {
		return ErrConflict
	}
	r.s.wallets[w.ID()] = w
	r.s.playerKey[k] = w.ID()
	return nil
}

func (r memWallets) Update(_ context.Context, w domain.Wallet) error {
	if _, ok := r.s.wallets[w.ID()]; !ok {
		return ErrNotFound
	}
	r.s.wallets[w.ID()] = w
	return nil
}

type memTx struct{ s *memStore }

func (r memTx) GetByID(_ context.Context, id string) (domain.WagerTransaction, error) {
	tx, ok := r.s.txs[id]
	if !ok {
		return domain.WagerTransaction{}, ErrNotFound
	}
	return tx, nil
}

func (r memTx) GetByProviderExternalID(_ context.Context, providerID, externalID string) (domain.WagerTransaction, error) {
	id, ok := r.s.byExt[providerID+"|"+externalID]
	if !ok {
		return domain.WagerTransaction{}, ErrNotFound
	}
	return r.s.txs[id], nil
}

func (r memTx) GetByProviderIdempotencyKey(_ context.Context, providerID, key string) (domain.WagerTransaction, error) {
	id, ok := r.s.byKey[providerID+"|"+key]
	if !ok {
		return domain.WagerTransaction{}, ErrNotFound
	}
	return r.s.txs[id], nil
}

func (r memTx) ListProcessedReversals(_ context.Context, resolvedReferenceID string) ([]domain.WagerTransaction, error) {
	var out []domain.WagerTransaction
	for _, tx := range r.s.txs {
		if tx.Status() == domain.StatusProcessed &&
			(tx.Kind() == domain.KindRefund || tx.Kind() == domain.KindRollback) &&
			tx.ResolvedReferenceID() == resolvedReferenceID {
			out = append(out, tx)
		}
	}
	return out, nil
}

func (r memTx) Insert(_ context.Context, tx domain.WagerTransaction) error {
	if _, ok := r.s.txs[tx.ID()]; ok {
		return ErrConflict
	}
	if tx.Origin() == domain.OriginExternal {
		if _, ok := r.s.byKey[tx.ProviderID()+"|"+tx.IdempotencyKey()]; ok {
			return ErrConflict
		}
		if _, ok := r.s.byExt[tx.ProviderID()+"|"+tx.ExternalID()]; ok {
			return ErrConflict
		}
		r.s.byKey[tx.ProviderID()+"|"+tx.IdempotencyKey()] = tx.ID()
		r.s.byExt[tx.ProviderID()+"|"+tx.ExternalID()] = tx.ID()
	}
	r.s.txs[tx.ID()] = tx
	return nil
}

func (r memTx) Update(_ context.Context, tx domain.WagerTransaction) error {
	if _, ok := r.s.txs[tx.ID()]; !ok {
		return ErrNotFound
	}
	r.s.txs[tx.ID()] = tx
	return nil
}

type memLedger struct{ s *memStore }

func (r memLedger) Insert(_ context.Context, e domain.WalletLedgerEntry) error {
	r.s.ledger = append(r.s.ledger, e)
	return nil
}

func (r memLedger) ListByWallet(_ context.Context, walletID string, createdAt time.Time, id string, limit int) ([]domain.WalletLedgerEntry, error) {
	var out []domain.WalletLedgerEntry
	for _, e := range r.s.ledger {
		if e.WalletID() != walletID {
			continue
		}
		if id != "" {
			if e.CreatedAt().Before(createdAt) || (e.CreatedAt().Equal(createdAt) && e.ID() <= id) {
				continue
			}
		}
		out = append(out, e)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (r memLedger) SumByWallet(_ context.Context, walletID, currency string) (domain.Money, int, error) {
	sum, err := domain.Zero(currency)
	if err != nil {
		return domain.Money{}, 0, err
	}
	n := 0
	for _, e := range r.s.ledger {
		if e.WalletID() != walletID {
			continue
		}
		n++
		switch e.Direction() {
		case domain.DirectionCredit:
			sum, err = sum.Add(e.Money())
		case domain.DirectionDebit:
			sum, err = sum.Sub(e.Money())
		}
		if err != nil {
			return domain.Money{}, 0, err
		}
	}
	return sum, n, nil
}

type memOutbox struct{ s *memStore }

func (r memOutbox) Insert(_ context.Context, rec OutboxRecord) error {
	r.s.outbox = append(r.s.outbox, rec)
	return nil
}

type memInbox struct{ s *memStore }

func inboxKey(consumerName, messageID string) string {
	return consumerName + "|" + messageID
}

func (r memInbox) Get(_ context.Context, consumerName, messageID string) (InboxRecord, error) {
	rec, ok := r.s.inbox[inboxKey(consumerName, messageID)]
	if !ok {
		return InboxRecord{}, ErrNotFound
	}
	return rec, nil
}

func (r memInbox) Insert(_ context.Context, rec InboxRecord) error {
	k := inboxKey(rec.ConsumerName, rec.MessageID)
	if _, ok := r.s.inbox[k]; ok {
		return ErrConflict
	}
	r.s.inbox[k] = rec
	return nil
}

type seqIDs struct{ n int }

func (s *seqIDs) NewID() string {
	s.n++
	return fmt.Sprintf("id-%d", s.n)
}

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }
