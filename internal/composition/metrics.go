package composition

import (
	metricsadapter "github.com/leosanner/desafio-jungle-go/internal/adapter/metrics"
	"github.com/leosanner/desafio-jungle-go/internal/app"

	"go.uber.org/fx"
)

func metricsModule() fx.Option {
	return fx.Module("metrics",
		fx.Provide(
			fx.Annotate(metricsadapter.New, fx.As(new(app.Metrics))),
		),
	)
}
