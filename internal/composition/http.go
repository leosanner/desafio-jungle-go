package composition

import (
	httpserver "github.com/leosanner/desafio-jungle-go/internal/adapter/http"
	"github.com/leosanner/desafio-jungle-go/internal/adapter/postgres"
	sqsadapter "github.com/leosanner/desafio-jungle-go/internal/adapter/sqs"

	"go.uber.org/fx"
)

func httpModule() fx.Option {
	return fx.Module("http",
		fx.Provide(newCheckers),
		fx.Provide(httpserver.New),
		fx.Invoke(registerHTTP),
	)
}

func newCheckers(p *postgres.Pool, c *sqsadapter.Client) httpserver.Checkers {
	return httpserver.Checkers{p, c}
}

func registerHTTP(lc fx.Lifecycle, s *httpserver.Server) {
	lc.Append(fx.Hook{
		OnStart: s.Start,
		OnStop:  s.Stop,
	})
}
