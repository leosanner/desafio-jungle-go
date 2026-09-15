package composition_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/composition"
	"github.com/leosanner/desafio-jungle-go/internal/config"

	"go.uber.org/fx"
)

func TestValidateApp(t *testing.T) {
	t.Parallel()
	if err := fx.ValidateApp(composition.Modules()); err != nil {
		t.Fatalf("invalid fx graph: %v", err)
	}
}

func TestInvalidConfigFailsStart(t *testing.T) {
	keys := []string{
		"LOG_LEVEL",
		"HTTP_ADDR",
		"POSTGRES_DSN",
		"MIGRATIONS_PATH",
		"AWS_REGION",
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_ENDPOINT_URL",
		"SQS_WAGER_QUEUE_NAME",
		"SQS_WAGER_DLQ_NAME",
		"FX_START_TIMEOUT",
		"FX_STOP_TIMEOUT",
	}
	for _, key := range keys {
		t.Setenv(key, "")
	}

	app := fx.New(composition.Modules(), fx.NopLogger)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := app.Start(ctx)
	if err == nil {
		_ = app.Stop(ctx)
		t.Fatal("expected Start to fail with invalid config")
	}
	if !errors.Is(err, config.ErrMissingEnv) {
		t.Fatalf("Start err = %v, want %v", err, config.ErrMissingEnv)
	}
}
