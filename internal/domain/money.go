package domain

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

const (
	moneyScale   = 2
	minorUnits   = int64(100)
	maxInt64     = int64(^uint64(0) >> 1)
	minInt64     = -maxInt64 - 1
	maxAbsIP     = maxInt64 / minorUnits // 92233720368547758
	maxFracAtCap = maxInt64 % minorUnits // 7
)

// Money is an immutable amount in minor units plus an ISO 4217 currency.
// The zero value is uninitialized and must be rejected.
type Money struct {
	minor       int64
	currency    string
	initialized bool
}

// ParseMoney parses an external non-negative amount with exactly two decimal places.
func ParseMoney(amount, currency string) (Money, error) {
	cur, err := ParseCurrency(currency)
	if err != nil {
		return Money{}, err
	}
	minor, err := parseExternalAmount(amount)
	if err != nil {
		return Money{}, err
	}
	return Money{minor: minor, currency: cur, initialized: true}, nil
}

// MoneyFromMinor builds Money from minor units. Negative values are allowed for
// internal differences; wallet balances and ParseMoney stay non-negative.
func MoneyFromMinor(minor int64, currency string) (Money, error) {
	cur, err := ParseCurrency(currency)
	if err != nil {
		return Money{}, err
	}
	return Money{minor: minor, currency: cur, initialized: true}, nil
}

// Zero returns a zero amount in currency.
func Zero(currency string) (Money, error) {
	return MoneyFromMinor(0, currency)
}

// ParseCurrency accepts a 3-letter uppercase ISO 4217 alphabetic code.
// The official catalog is not loaded; format is validated, not membership.
func ParseCurrency(code string) (string, error) {
	code = strings.TrimSpace(code)
	if len(code) != 3 {
		return "", validation(FailureInvalidCurrency, fmt.Errorf("%w: %q", ErrInvalidCurrency, code))
	}
	for _, r := range code {
		if r < 'A' || r > 'Z' {
			return "", validation(FailureInvalidCurrency, fmt.Errorf("%w: %q", ErrInvalidCurrency, code))
		}
	}
	return code, nil
}

// Initialized reports whether m was constructed by a validating constructor.
func (m Money) Initialized() bool { return m.initialized }

// Currency returns the ISO code, or empty if uninitialized.
func (m Money) Currency() string { return m.currency }

// Minor returns minor units. Valid only when Initialized.
func (m Money) Minor() int64 { return m.minor }

// AmountString is the canonical two-decimal representation (sign included if negative).
func (m Money) AmountString() string {
	if !m.initialized {
		return ""
	}
	sign := ""
	minor := m.minor
	if minor < 0 {
		sign = "-"
		if minor == minInt64 {
			// minInt64 cannot be negated; format via uint.
			u := uint64(minor)
			ip := u / uint64(minorUnits)
			fp := u % uint64(minorUnits)
			return fmt.Sprintf("-%d.%02d", ip, fp)
		}
		minor = -minor
	}
	ip := minor / minorUnits
	fp := minor % minorUnits
	return fmt.Sprintf("%s%d.%02d", sign, ip, fp)
}

// IsZero is true only for an initialized zero amount.
func (m Money) IsZero() bool { return m.initialized && m.minor == 0 }

// IsPositive is true only for an initialized amount strictly greater than zero.
func (m Money) IsPositive() bool { return m.initialized && m.minor > 0 }

// IsNegative is true only for an initialized amount strictly less than zero.
func (m Money) IsNegative() bool { return m.initialized && m.minor < 0 }

// RequireInitialized returns ErrUninitializedMoney when m is the zero value.
func (m Money) RequireInitialized() error {
	if !m.initialized {
		return validation(FailureUninitialized, ErrUninitializedMoney)
	}
	return nil
}

// Add returns m + other. Currencies must match. Detects int64 overflow.
func (m Money) Add(other Money) (Money, error) {
	if err := m.requireCompatible(other); err != nil {
		return Money{}, err
	}
	sum, err := addInt64(m.minor, other.minor)
	if err != nil {
		return Money{}, err
	}
	return Money{minor: sum, currency: m.currency, initialized: true}, nil
}

// Sub returns m - other.
func (m Money) Sub(other Money) (Money, error) {
	if err := m.requireCompatible(other); err != nil {
		return Money{}, err
	}
	diff, err := subInt64(m.minor, other.minor)
	if err != nil {
		return Money{}, err
	}
	return Money{minor: diff, currency: m.currency, initialized: true}, nil
}

// Negate returns -m.
func (m Money) Negate() (Money, error) {
	if err := m.RequireInitialized(); err != nil {
		return Money{}, err
	}
	if m.minor == minInt64 {
		return Money{}, validation(FailureOverflow, fmt.Errorf("%w: negate", ErrOverflow))
	}
	return Money{minor: -m.minor, currency: m.currency, initialized: true}, nil
}

// Cmp returns -1, 0 or 1 as m <, == or > other.
func (m Money) Cmp(other Money) (int, error) {
	if err := m.requireCompatible(other); err != nil {
		return 0, err
	}
	switch {
	case m.minor < other.minor:
		return -1, nil
	case m.minor > other.minor:
		return 1, nil
	default:
		return 0, nil
	}
}

// Equal is true when amounts and currencies match.
func (m Money) Equal(other Money) (bool, error) {
	c, err := m.Cmp(other)
	if err != nil {
		return false, err
	}
	return c == 0, nil
}

func (m Money) requireCompatible(other Money) error {
	if err := m.RequireInitialized(); err != nil {
		return err
	}
	if err := other.RequireInitialized(); err != nil {
		return err
	}
	if m.currency != other.currency {
		return validation(FailureIncompatibleCurrency, fmt.Errorf("%w: %s vs %s", ErrIncompatibleCurrency, m.currency, other.currency))
	}
	return nil
}

func parseExternalAmount(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, validation(FailureInvalidAmount, fmt.Errorf("%w: empty", ErrInvalidAmount))
	}
	lower := strings.ToLower(s)
	if strings.Contains(lower, "nan") || strings.Contains(lower, "inf") {
		return 0, validation(FailureInvalidAmount, fmt.Errorf("%w: non-finite", ErrInvalidAmount))
	}
	if strings.ContainsAny(s, "eE+") {
		return 0, validation(FailureInvalidAmount, fmt.Errorf("%w: scientific or signed", ErrInvalidAmount))
	}
	if s[0] == '-' {
		return 0, validation(FailureInvalidAmount, fmt.Errorf("%w: negative external amount", ErrInvalidAmount))
	}
	dot := strings.IndexByte(s, '.')
	if dot < 0 {
		return 0, validation(FailureInvalidAmount, fmt.Errorf("%w: scale must be %d", ErrInvalidAmount, moneyScale))
	}
	ipStr, fpStr := s[:dot], s[dot+1:]
	if len(fpStr) != moneyScale {
		return 0, validation(FailureInvalidAmount, fmt.Errorf("%w: scale must be %d", ErrInvalidAmount, moneyScale))
	}
	if ipStr == "" || fpStr == "" {
		return 0, validation(FailureInvalidAmount, fmt.Errorf("%w: malformed", ErrInvalidAmount))
	}
	if !allDigits(ipStr) || !allDigits(fpStr) {
		return 0, validation(FailureInvalidAmount, fmt.Errorf("%w: non-digit", ErrInvalidAmount))
	}
	if len(ipStr) > 1 && ipStr[0] == '0' {
		return 0, validation(FailureInvalidAmount, fmt.Errorf("%w: leading zero", ErrInvalidAmount))
	}

	ip, err := strconv.ParseInt(ipStr, 10, 64)
	if err != nil {
		return 0, validation(FailureOverflow, fmt.Errorf("%w: integer part", ErrOverflow))
	}
	fp, err := strconv.ParseInt(fpStr, 10, 64)
	if err != nil {
		return 0, validation(FailureInvalidAmount, fmt.Errorf("%w: fraction", ErrInvalidAmount))
	}
	if ip > maxAbsIP || (ip == maxAbsIP && fp > maxFracAtCap) {
		return 0, validation(FailureOverflow, fmt.Errorf("%w: amount exceeds int64 minor units", ErrOverflow))
	}
	return ip*minorUnits + fp, nil
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' || unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

func addInt64(a, b int64) (int64, error) {
	if b > 0 && a > maxInt64-b {
		return 0, validation(FailureOverflow, fmt.Errorf("%w: add", ErrOverflow))
	}
	if b < 0 && a < minInt64-b {
		return 0, validation(FailureOverflow, fmt.Errorf("%w: add", ErrOverflow))
	}
	return a + b, nil
}

func subInt64(a, b int64) (int64, error) {
	if b < 0 && a > maxInt64+b {
		return 0, validation(FailureOverflow, fmt.Errorf("%w: subtract", ErrOverflow))
	}
	if b > 0 && a < minInt64+b {
		return 0, validation(FailureOverflow, fmt.Errorf("%w: subtract", ErrOverflow))
	}
	return a - b, nil
}
