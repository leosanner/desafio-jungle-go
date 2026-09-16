package composition

import (
	"context"

	"github.com/leosanner/desafio-jungle-go/internal/adapter/auth"
	httpserver "github.com/leosanner/desafio-jungle-go/internal/adapter/http"

	"go.uber.org/fx"
)

func authModule() fx.Option {
	return fx.Module("auth",
		fx.Provide(auth.NewOIDCVerifier),
		fx.Provide(func(v *auth.OIDCVerifier) httpserver.TokenVerifier { return v }),
		fx.Invoke(registerAuth),
	)
}

func registerAuth(lc fx.Lifecycle, v *auth.OIDCVerifier) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return v.Check(ctx)
		},
	})
}
