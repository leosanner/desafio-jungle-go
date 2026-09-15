package composition

import (
	"context"

	"github.com/leosanner/desafio-jungle-go/internal/adapter/sqs"

	"go.uber.org/fx"
)

func sqsModule() fx.Option {
	return fx.Module("sqs",
		fx.Provide(sqsadapter.NewClient),
		fx.Invoke(registerSQS),
	)
}

func registerSQS(lc fx.Lifecycle, c *sqsadapter.Client) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return c.CheckQueues(ctx)
		},
	})
}
