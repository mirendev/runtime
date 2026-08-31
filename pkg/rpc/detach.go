package rpc

import "context"

type serverLifetimeContextKey struct{}

// contextWithServerLifetime records the server process context on an RPC
// handler context. A handler may then detach long-running work from the client
// transport without also detaching it from server shutdown.
func contextWithServerLifetime(ctx, lifetime context.Context) context.Context {
	if lifetime == nil {
		return ctx
	}
	return context.WithValue(ctx, serverLifetimeContextKey{}, lifetime)
}

// Detach returns a context that preserves the caller's values but does not end
// when its transport goes away. When called by an RPC handler, the returned
// context remains tied to the server process lifetime. Outside RPC dispatch it
// falls back to context.WithoutCancel, which keeps the helper useful in direct
// unit tests.
func Detach(ctx context.Context) (context.Context, context.CancelFunc) {
	detached, cancel := context.WithCancel(context.WithoutCancel(ctx))
	lifetime, _ := ctx.Value(serverLifetimeContextKey{}).(context.Context)
	if lifetime == nil {
		return detached, cancel
	}

	stop := context.AfterFunc(lifetime, cancel)
	return detached, func() {
		stop()
		cancel()
	}
}
