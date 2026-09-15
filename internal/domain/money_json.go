package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type moneyJSON struct {
	Amount   json.RawMessage `json:"amount"`
	Currency string          `json:"currency"`
}

// MarshalJSON implements the external contract {"amount":"25.00","currency":"BRL"}.
func (m Money) MarshalJSON() ([]byte, error) {
	if err := m.RequireInitialized(); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
	}{
		Amount:   m.AmountString(),
		Currency: m.currency,
	})
}

// UnmarshalJSON requires amount to be a JSON string (never a number, so no float path).
func (m *Money) UnmarshalJSON(data []byte) error {
	var raw moneyJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return validation(FailureInvalidAmount, fmt.Errorf("%w: %v", ErrInvalidAmount, err))
	}
	amount, err := jsonStringAmount(raw.Amount)
	if err != nil {
		return err
	}
	parsed, err := ParseMoney(amount, raw.Currency)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}

func jsonStringAmount(raw json.RawMessage) (string, error) {
	trim := bytes.TrimSpace(raw)
	if len(trim) == 0 || trim[0] != '"' {
		return "", validation(FailureJSONAmountNotString, ErrJSONAmountNotString)
	}
	var s string
	if err := json.Unmarshal(trim, &s); err != nil {
		return "", validation(FailureInvalidAmount, fmt.Errorf("%w: %v", ErrInvalidAmount, err))
	}
	return s, nil
}
