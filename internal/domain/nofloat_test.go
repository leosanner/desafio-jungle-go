package domain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDomainSourcesHaveNoFloatMoney(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	needles := []string{"float32", "float64", "ParseFloat"}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(".", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		for _, n := range needles {
			if strings.Contains(text, n) {
				t.Errorf("%s contains %q", e.Name(), n)
			}
		}
	}
}
