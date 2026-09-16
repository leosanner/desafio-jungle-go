package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// CanonicalPayload is the business fields hashed for idempotency (ADR 0012).
// Idempotency-Key and transport metadata are not included.
type CanonicalPayload struct {
	ProviderID          string
	ExternalID          string
	PlayerID            string
	WalletID            string
	RoundID             string
	GameID              string
	Kind                Kind
	Money               Money
	ReferenceExternalID string
}

// HashCanonicalPayload returns a lowercase SHA-256 hex digest of key-sorted JSON.
func HashCanonicalPayload(p CanonicalPayload) (string, error) {
	if err := p.Money.RequireInitialized(); err != nil {
		return "", err
	}
	body := map[string]any{
		"externalTransactionId": p.ExternalID,
		"gameId":                p.GameID,
		"kind":                  string(p.Kind),
		"money": map[string]any{
			"amount":   p.Money.AmountString(),
			"currency": p.Money.Currency(),
		},
		"playerId":   p.PlayerID,
		"providerId": p.ProviderID,
		"roundId":    p.RoundID,
		"walletId":   p.WalletID,
	}
	if p.ReferenceExternalID != "" {
		body["referenceExternalTransactionId"] = p.ReferenceExternalID
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("canonical payload: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
