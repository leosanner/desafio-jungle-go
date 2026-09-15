---
name: fx-module
description: Uber Fx composition patterns — fx.Module, fx.Provide, fx.Invoke, fx.Lifecycle, cancellable workers and ordered shutdown. Use when creating or changing wiring, the HTTP server, SQS consumers, outbox publishers or any component with a lifecycle.
---

# Composition and lifecycle with Uber Fx

Source requirements: `init.md` §4 "Composição e ciclo de vida" and §10 (SIGTERM in the consumer).

## Organization

- One `fx.Module("name", ...)` per infrastructure/feature area (e.g. `config`, `postgres`, `sqs`,
  `auth`, `http`, `wagering`, `outbox`). Modules are aggregated in a single composition root used by `cmd/`.
- `fx.Provide` with **plain constructors** (`func NewX(deps...) (*X, error)`). Avoid `fx.In`/`fx.Out`
  unless there is a real gain (groups, names); never in the domain.
- `fx.Invoke` only to "wire up" components that must run (server, workers), not for logic.
- Use `fx.Annotate`/`fx.As` to expose implementations as application interfaces (ports).
- Configuration is loaded and **validated** in its own provider; a validation error fails `app.Start`.

## Lifecycle rules

1. `OnStart` must return quickly. Long-running work runs in a goroutine started there.
2. Check dependencies at startup (PostgreSQL ping, SQS queue exists, JWKS reachable) and fail fast
   with a clear error.
3. `OnStop` must: stop accepting input → wait for in-flight work within the `ctx` deadline → release
   what did not finish (e.g. `ChangeMessageVisibility` to 0) → return.
4. Fx runs `OnStop` in **reverse** registration order. Since dependencies are constructed before their
   users, the DB pool closes after the worker that uses it — keep hooks registered in the resource's own
   constructor to preserve that order.
5. Explicit deadlines: configurable `fx.StartTimeout` / `fx.StopTimeout`; worker shutdown honors the
   `ctx` passed to `OnStop`.

## Worker skeleton

```go
type Worker struct {
	cancel context.CancelFunc
	done   chan struct{}
	// deps...
}

func NewWorker(lc fx.Lifecycle /*, deps */) *Worker {
	w := &Worker{done: make(chan struct{})}
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			ctx, cancel := context.WithCancel(context.Background())
			w.cancel = cancel
			go w.run(ctx)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			w.cancel() // stop fetching new work
			select {
			case <-w.done: // observable termination
				return nil
			case <-ctx.Done():
				return fmt.Errorf("worker: shutdown deadline: %w", ctx.Err())
			}
		},
	})
	return w
}

func (w *Worker) run(ctx context.Context) {
	defer close(w.done)
	// loop: poll → process item with its own timeout → backoff
	// the in-flight item uses a ctx that is NOT cancelled immediately on shutdown,
	// but is bounded by a deadline, so it can finish or release the message.
}
```

## HTTP server

- `OnStart`: synchronous `net.Listen` (a port error fails start) and `srv.Serve(ln)` in a goroutine.
- `OnStop`: `srv.Shutdown(ctx)` — stops accepting connections and waits for in-flight requests.
- Readiness must start failing as soon as shutdown begins.

## Required composition tests

- `fx.ValidateApp(...)` (or `fxtest.New` + `RequireStart`/`RequireStop`) to check the graph.
- Integration test that starts the app against real infrastructure, stops it, and verifies workers
  finished (`done` closed) and resources were released (pool closed, no leaked goroutines — consider
  `go.uber.org/goleak`).

## Checklist

- [ ] No Fx imports in the domain or use cases
- [ ] Config validated on start
- [ ] Every goroutine has cancellation and observable termination
- [ ] Shutdown: stop input → finish/release work → close dependencies
- [ ] Composition test + start/stop
