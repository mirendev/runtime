package boot

import "context"

func Provide0[O any](name string, start func(context.Context) (O, error), options ...Option) (*Component, Output[O]) {
	return provide(name, nil, start, options)
}

func Provide1[A, O any](name string, a Output[A], start func(context.Context, A) (O, error), options ...Option) (*Component, Output[O]) {
	return provide(name, []input{a}, func(ctx context.Context) (O, error) {
		return start(ctx, a.Value())
	}, options)
}

func Provide2[A, B, O any](name string, a Output[A], b Output[B], start func(context.Context, A, B) (O, error), options ...Option) (*Component, Output[O]) {
	return provide(name, []input{a, b}, func(ctx context.Context) (O, error) {
		return start(ctx, a.Value(), b.Value())
	}, options)
}

func Provide3[A, B, C, O any](name string, a Output[A], b Output[B], c Output[C], start func(context.Context, A, B, C) (O, error), options ...Option) (*Component, Output[O]) {
	return provide(name, []input{a, b, c}, func(ctx context.Context) (O, error) {
		return start(ctx, a.Value(), b.Value(), c.Value())
	}, options)
}

func Provide4[A, B, C, D, O any](name string, a Output[A], b Output[B], c Output[C], d Output[D], start func(context.Context, A, B, C, D) (O, error), options ...Option) (*Component, Output[O]) {
	return provide(name, []input{a, b, c, d}, func(ctx context.Context) (O, error) {
		return start(ctx, a.Value(), b.Value(), c.Value(), d.Value())
	}, options)
}

func Provide5[A, B, C, D, E, O any](name string, a Output[A], b Output[B], c Output[C], d Output[D], e Output[E], start func(context.Context, A, B, C, D, E) (O, error), options ...Option) (*Component, Output[O]) {
	return provide(name, []input{a, b, c, d, e}, func(ctx context.Context) (O, error) {
		return start(ctx, a.Value(), b.Value(), c.Value(), d.Value(), e.Value())
	}, options)
}

func Provide6[A, B, C, D, E, F, O any](name string, a Output[A], b Output[B], c Output[C], d Output[D], e Output[E], f Output[F], start func(context.Context, A, B, C, D, E, F) (O, error), options ...Option) (*Component, Output[O]) {
	return provide(name, []input{a, b, c, d, e, f}, func(ctx context.Context) (O, error) {
		return start(ctx, a.Value(), b.Value(), c.Value(), d.Value(), e.Value(), f.Value())
	}, options)
}

func Provide7[A, B, C, D, E, F, G, O any](name string, a Output[A], b Output[B], c Output[C], d Output[D], e Output[E], f Output[F], g Output[G], start func(context.Context, A, B, C, D, E, F, G) (O, error), options ...Option) (*Component, Output[O]) {
	return provide(name, []input{a, b, c, d, e, f, g}, func(ctx context.Context) (O, error) {
		return start(ctx, a.Value(), b.Value(), c.Value(), d.Value(), e.Value(), f.Value(), g.Value())
	}, options)
}
