package composition

import (
	"github.com/leosanner/desafio-jungle-go/internal/config"

	"go.uber.org/fx"
)

func configModule() fx.Option {
	return fx.Module("config",
		fx.Provide(
			config.Load,
			config.NewLogger,
		),
	)
}
