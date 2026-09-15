package composition

import (
	"context"

	"github.com/leosanner/desafio-jungle-go/internal/adapter/postgres"
	"github.com/leosanner/desafio-jungle-go/internal/config"

	"go.uber.org/fx"
)

func postgresModule() fx.Option {
	return fx.Module("postgres",
		fx.Provide(postgres.NewPool),
		fx.Invoke(registerPostgres),
	)
}

func registerPostgres(lc fx.Lifecycle, p *postgres.Pool, cfg config.Config) {
	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			p.Close()
			return nil
		},
	})
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := postgres.RunUp(cfg.MigrationsPath, cfg.PostgresDSN); err != nil {
				return err
			}
			return p.Ping(ctx)
		},
	})
}
