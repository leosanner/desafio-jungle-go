package composition

import (
	"github.com/leosanner/desafio-jungle-go/internal/app"

	"go.uber.org/fx"
)

func wageringModule() fx.Option {
	return fx.Module("wagering",
		fx.Provide(
			func() app.Clock { return app.SystemClock{} },
			func() app.IDGenerator { return app.UUIDGenerator{} },
			app.NewService,
		),
	)
}
