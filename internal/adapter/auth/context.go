package auth

import (
	"context"

	"github.com/leosanner/desafio-jungle-go/internal/app"
)

type ctxKey struct{}

// WithActor stores the authenticated actor on ctx.
func WithActor(ctx context.Context, a app.Actor) context.Context {
	return context.WithValue(ctx, ctxKey{}, a)
}

// ActorFrom returns the actor stored by WithActor.
func ActorFrom(ctx context.Context) (app.Actor, bool) {
	a, ok := ctx.Value(ctxKey{}).(app.Actor)
	return a, ok
}
