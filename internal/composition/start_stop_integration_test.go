//go:build integration

package composition_test

import (
	"os"
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
	app := fxtest.New(t, composition.Modules())
	app.RequireStart()
	app.RequireStop()
}
