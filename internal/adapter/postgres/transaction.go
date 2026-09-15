package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/domain"
)

const transactionColumns = `id, origin, provider_id, external_transaction_id, idempotency_key, payload_hash, wallet_id, player_id, round_id, game_id, kind, amount_minor, currency, reference_external_id, resolved_reference_id, status, failure_code, result_balance_minor, result_currency, created_at, updated_at`

type transactionRepo struct {
	q querier
}

var _ app.TransactionRepository = (*transactionRepo)(nil)

func (r *transactionRepo) GetByID(ctx context.Context, id string) (domain.WagerTransaction, error) {
	const q = `SELECT ` + transactionColumns + ` FROM wagering.wager_transactions WHERE id = $1`
	return scanTransaction(r.q.QueryRow(ctx, q, id))
}

func (r *transactionRepo) GetByProviderExternalID(ctx context.Context, providerID, externalID string) (domain.WagerTransaction, error) {
	const q = `SELECT ` + transactionColumns + `
		FROM wagering.wager_transactions
		WHERE provider_id = $1 AND external_transaction_id = $2`
	return scanTransaction(r.q.QueryRow(ctx, q, providerID, externalID))
}

func (r *transactionRepo) GetByProviderIdempotencyKey(ctx context.Context, providerID, key string) (domain.WagerTransaction, error) {
	const q = `SELECT ` + transactionColumns + `
		FROM wagering.wager_transactions
		WHERE provider_id = $1 AND idempotency_key = $2`
	return scanTransaction(r.q.QueryRow(ctx, q, providerID, key))
}

func (r *transactionRepo) Insert(ctx context.Context, tx domain.WagerTransaction) error {
	resultMinor, resultCurrency := resultBalanceArgs(tx)
	// attempt_count / next_attempt_at are omitted so the schema defaults apply.
	const q = `
		INSERT INTO wagering.wager_transactions (
			id, origin, provider_id, external_transaction_id, idempotency_key, payload_hash,
			wallet_id, player_id, round_id, game_id, kind, amount_minor, currency,
			reference_external_id, resolved_reference_id, status, failure_code,
			result_balance_minor, result_currency, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21
		)`
	_, err := r.q.Exec(ctx, q,
		tx.ID(),
		string(tx.Origin()),
		nullIfEmpty(tx.ProviderID()),
		nullIfEmpty(tx.ExternalID()),
		nullIfEmpty(tx.IdempotencyKey()),
		nullIfEmpty(tx.PayloadHash()),
		tx.WalletID(),
		tx.PlayerID(),
		nullIfEmpty(tx.RoundID()),
		nullIfEmpty(tx.GameID()),
		string(tx.Kind()),
		tx.Money().Minor(),
		tx.Money().Currency(),
		nullIfEmpty(tx.ReferenceExternalID()),
		nullIfEmpty(tx.ResolvedReferenceID()),
		string(tx.Status()),
		nullIfEmpty(string(tx.FailureCode())),
		resultMinor,
		resultCurrency,
		tx.CreatedAt(),
		tx.UpdatedAt(),
	)
	if err != nil {
		return mapError(err)
	}
	return nil
}

func (r *transactionRepo) Update(ctx context.Context, tx domain.WagerTransaction) error {
	resultMinor, resultCurrency := resultBalanceArgs(tx)
	// Do not SET attempt_count / next_attempt_at: workers own those columns.
	const q = `
		UPDATE wagering.wager_transactions SET
			origin = $1,
			provider_id = $2,
			external_transaction_id = $3,
			idempotency_key = $4,
			payload_hash = $5,
			wallet_id = $6,
			player_id = $7,
			round_id = $8,
			game_id = $9,
			kind = $10,
			amount_minor = $11,
			currency = $12,
			reference_external_id = $13,
			resolved_reference_id = $14,
			status = $15,
			failure_code = $16,
			result_balance_minor = $17,
			result_currency = $18,
			created_at = $19,
			updated_at = $20
		WHERE id = $21`
	tag, err := r.q.Exec(ctx, q,
		string(tx.Origin()),
		nullIfEmpty(tx.ProviderID()),
		nullIfEmpty(tx.ExternalID()),
		nullIfEmpty(tx.IdempotencyKey()),
		nullIfEmpty(tx.PayloadHash()),
		tx.WalletID(),
		tx.PlayerID(),
		nullIfEmpty(tx.RoundID()),
		nullIfEmpty(tx.GameID()),
		string(tx.Kind()),
		tx.Money().Minor(),
		tx.Money().Currency(),
		nullIfEmpty(tx.ReferenceExternalID()),
		nullIfEmpty(tx.ResolvedReferenceID()),
		string(tx.Status()),
		nullIfEmpty(string(tx.FailureCode())),
		resultMinor,
		resultCurrency,
		tx.CreatedAt(),
		tx.UpdatedAt(),
		tx.ID(),
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("postgres: %w", app.ErrNotFound)
	}
	return nil
}

func resultBalanceArgs(tx domain.WagerTransaction) (minor any, currency any) {
	m, ok := tx.ResultBalance()
	if !ok {
		return nil, nil
	}
	return m.Minor(), m.Currency()
}

func scanTransaction(row scanner) (domain.WagerTransaction, error) {
	var (
		id, origin, walletID, playerID, kind, currency, status string
		providerID, externalID, idempotencyKey, payloadHash    *string
		roundID, gameID, referenceExternalID, resolvedRefID    *string
		failureCode                                            *string
		amountMinor                                            int64
		resultMinor                                            *int64
		resultCurrency                                         *string
		createdAt, updatedAt                                   time.Time
	)
	if err := row.Scan(
		&id,
		&origin,
		&providerID,
		&externalID,
		&idempotencyKey,
		&payloadHash,
		&walletID,
		&playerID,
		&roundID,
		&gameID,
		&kind,
		&amountMinor,
		&currency,
		&referenceExternalID,
		&resolvedRefID,
		&status,
		&failureCode,
		&resultMinor,
		&resultCurrency,
		&createdAt,
		&updatedAt,
	); err != nil {
		return domain.WagerTransaction{}, mapError(err)
	}

	money, err := moneyFromMinorScan(amountMinor, currency)
	if err != nil {
		return domain.WagerTransaction{}, err
	}

	var result domain.Money
	hasResult := false
	if resultMinor != nil && resultCurrency != nil {
		result, err = moneyFromMinorScan(*resultMinor, *resultCurrency)
		if err != nil {
			return domain.WagerTransaction{}, err
		}
		hasResult = true
	}

	tx, err := domain.RehydrateTransaction(domain.RehydrateTxParams{
		ID:                  id,
		Origin:              domain.Origin(origin),
		ProviderID:          derefString(providerID),
		ExternalID:          derefString(externalID),
		IdempotencyKey:      derefString(idempotencyKey),
		PayloadHash:         derefString(payloadHash),
		WalletID:            walletID,
		PlayerID:            playerID,
		RoundID:             derefString(roundID),
		GameID:              derefString(gameID),
		Kind:                domain.Kind(kind),
		Money:               money,
		ReferenceExternalID: derefString(referenceExternalID),
		ResolvedReferenceID: derefString(resolvedRefID),
		Status:              domain.Status(status),
		FailureCode:         domain.FailureCode(derefString(failureCode)),
		ResultBalance:       result,
		HasResult:           hasResult,
		CreatedAt:           createdAt,
		UpdatedAt:           updatedAt,
	})
	if err != nil {
		return domain.WagerTransaction{}, fmt.Errorf("postgres: rehydrate transaction: %w", err)
	}
	return tx, nil
}
