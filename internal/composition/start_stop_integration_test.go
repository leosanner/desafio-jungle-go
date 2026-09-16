//go:build integration

package composition_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/leosanner/desafio-jungle-go/internal/composition"
	"go.uber.org/fx/fxtest"
)

func TestAppStartStop(t *testing.T) {
	if os.Getenv("POSTGRES_DSN") == "" {
		t.Skip("POSTGRES_DSN is not set")
	}
	if os.Getenv("OIDC_ISSUER") == "" {
		t.Skip("OIDC_ISSUER is not set")
	}
	t.Setenv("MIGRATIONS_PATH", repoMigrations(t))
	t.Setenv("HTTP_ADDR", ":0")
	app := fxtest.New(t, composition.Modules())
	app.RequireStart()
	app.RequireStop()
}

func repoMigrations(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "migrations"))
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("migrations path %s: %v", dir, err)
	}
	return dir
}
