package domain

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

func TestParseMoneyAcceptsCanonical(t *testing.T) {
	t.Parallel()
	cases := []struct {
		amount   string
		currency string
		minor    int64
	}{
		{"0.00", "BRL", 0},
		{"0.01", "BRL", 1},
		{"1.00", "BRL", 100},
		{"25.00", "BRL", 2500},
		{"1000.00", "USD", 100000},
		{"92233720368547758.07", "BRL", math.MaxInt64},
	}
	for _, tc := range cases {
		t.Run(tc.amount+" "+tc.currency, func(t *testing.T) {
			t.Parallel()
			m, err := ParseMoney(tc.amount, tc.currency)
			if err != nil {
				t.Fatalf("ParseMoney: %v", err)
			}
			if m.Minor() != tc.minor {
				t.Errorf("minor = %d, want %d", m.Minor(), tc.minor)
			}
			if m.AmountString() != tc.amount {
				t.Errorf("AmountString = %q, want %q", m.AmountString(), tc.amount)
			}
			if m.Currency() != tc.currency {
				t.Errorf("currency = %q", m.Currency())
			}
		})
	}
}

func TestParseMoneyRejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		amount   string
		currency string
		want     error
	}{
		{"empty", "", "BRL", ErrInvalidAmount},
		{"nan", "NaN", "BRL", ErrInvalidAmount},
		{"infinity", "Infinity", "BRL", ErrInvalidAmount},
		{"inf", "Inf", "BRL", ErrInvalidAmount},
		{"scientific", "1e2", "BRL", ErrInvalidAmount},
		{"scientificE", "1.00E+1", "BRL", ErrInvalidAmount},
		{"no scale", "25", "BRL", ErrInvalidAmount},
		{"one decimal", "25.0", "BRL", ErrInvalidAmount},
		{"excess scale", "25.001", "BRL", ErrInvalidAmount},
		{"negative", "-1.00", "BRL", ErrInvalidAmount},
		{"plus", "+1.00", "BRL", ErrInvalidAmount},
		{"leading zero", "01.00", "BRL", ErrInvalidAmount},
		{"comma", "1,00", "BRL", ErrInvalidAmount},
		{"empty currency", "1.00", "", ErrInvalidCurrency},
		{"lower currency", "1.00", "brl", ErrInvalidCurrency},
		{"short currency", "1.00", "BR", ErrInvalidCurrency},
		{"overflow", "92233720368547758.08", "BRL", ErrOverflow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseMoney(tc.amount, tc.currency)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestMoneyZeroValueRejected(t *testing.T) {
	t.Parallel()
	var m Money
	if m.Initialized() {
		t.Fatal("zero value should be uninitialized")
	}
	if err := m.RequireInitialized(); !errors.Is(err, ErrUninitializedMoney) {
		t.Fatalf("RequireInitialized: %v", err)
	}
	z := mustZero(t, "BRL")
	if _, err := m.Add(z); !errors.Is(err, ErrUninitializedMoney) {
		t.Fatalf("Add: %v", err)
	}
}

func TestMoneyArithmetic(t *testing.T) {
	t.Parallel()
	a := mustMoney(t, "25.00", "BRL")
	b := mustMoney(t, "10.50", "BRL")
	sum, err := a.Add(b)
	if err != nil {
		t.Fatal(err)
	}
	if sum.AmountString() != "35.50" {
		t.Errorf("sum = %s", sum.AmountString())
	}
	diff, err := a.Sub(b)
	if err != nil {
		t.Fatal(err)
	}
	if diff.AmountString() != "14.50" {
		t.Errorf("diff = %s", diff.AmountString())
	}
	neg, err := b.Negate()
	if err != nil {
		t.Fatal(err)
	}
	if neg.AmountString() != "-10.50" {
		t.Errorf("neg = %s", neg.AmountString())
	}
	internal, err := MoneyFromMinor(-1, "BRL")
	if err != nil {
		t.Fatal(err)
	}
	if !internal.IsNegative() {
		t.Error("expected negative internal money")
	}
}

func TestMoneyIncompatibleCurrency(t *testing.T) {
	t.Parallel()
	a := mustMoney(t, "1.00", "BRL")
	b := mustMoney(t, "1.00", "USD")
	if _, err := a.Add(b); !errors.Is(err, ErrIncompatibleCurrency) {
		t.Fatalf("Add: %v", err)
	}
	if _, err := a.Cmp(b); !errors.Is(err, ErrIncompatibleCurrency) {
		t.Fatalf("Cmp: %v", err)
	}
}

func TestMoneyOverflow(t *testing.T) {
	t.Parallel()
	max, err := MoneyFromMinor(math.MaxInt64, "BRL")
	if err != nil {
		t.Fatal(err)
	}
	one, err := MoneyFromMinor(1, "BRL")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := max.Add(one); !errors.Is(err, ErrOverflow) {
		t.Fatalf("Add max: %v", err)
	}
	min, err := MoneyFromMinor(math.MinInt64, "BRL")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := min.Negate(); !errors.Is(err, ErrOverflow) {
		t.Fatalf("Negate min: %v", err)
	}
	if _, err := min.Sub(one); !errors.Is(err, ErrOverflow) {
		t.Fatalf("Sub min: %v", err)
	}
}

func TestMoneyJSON(t *testing.T) {
	t.Parallel()
	m := mustMoney(t, "25.00", "BRL")
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"amount":"25.00","currency":"BRL"}`
	if string(raw) != want {
		t.Fatalf("json = %s, want %s", raw, want)
	}
	var back Money
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	eq, err := m.Equal(back)
	if err != nil || !eq {
		t.Fatalf("round-trip equal=%v err=%v", eq, err)
	}

	if err := json.Unmarshal([]byte(`{"amount":25.00,"currency":"BRL"}`), &back); !errors.Is(err, ErrJSONAmountNotString) {
		t.Fatalf("numeric amount: %v", err)
	}
	if err := json.Unmarshal([]byte(`{"amount":"NaN","currency":"BRL"}`), &back); !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("NaN json: %v", err)
	}
}

func TestMoneyJSONRejectsUninitialized(t *testing.T) {
	t.Parallel()
	var m Money
	if _, err := json.Marshal(m); !errors.Is(err, ErrUninitializedMoney) {
		t.Fatalf("Marshal zero: %v", err)
	}
}
