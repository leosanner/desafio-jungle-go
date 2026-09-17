package composition

import (
	"log/slog"
	"time"

	"github.com/leosanner/desafio-jungle-go/internal/config"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
)

// Modules is the composition root. cmd/wagering should pass this to fx.New.
func Modules() fx.Option {
	return fx.Options(
		timeouts(),
		fx.WithLogger(func(log *slog.Logger) fxevent.Logger {
			l := &fxevent.SlogLogger{Logger: log}
			l.UseLogLevel(slog.LevelError)
			return l
		}),
		configModule(),
		metricsModule(),
		postgresModule(),
		sqsModule(),
		authModule(),
		wageringModule(),
		outboxModule(),
		inboundModule(),
		pendingModule(),
		httpModule(),
	)
}

func timeouts() fx.Option {
	start, stop := 15*time.Second, 30*time.Second
	cfg, err := config.Load()
	if err == nil {
		start = cfg.FXStartTimeout
		stop = cfg.FXStopTimeout
	}
	return fx.Options(
		fx.StartTimeout(start),
		fx.StopTimeout(stop),
	)
}
