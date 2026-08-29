package boot

import "context"

func Run0(name string, start func(context.Context) error, options ...Option) *Component {
	return run(name, nil, start, options)
}

func Run1[A any](name string, a Output[A], start func(context.Context, A) error, options ...Option) *Component {
	return run(name, []input{a}, func(ctx context.Context) error {
		return start(ctx, a.Value())
	}, options)
}

func Run2[A, B any](name string, a Output[A], b Output[B], start func(context.Context, A, B) error, options ...Option) *Component {
	return run(name, []input{a, b}, func(ctx context.Context) error {
		return start(ctx, a.Value(), b.Value())
	}, options)
}

func Run3[A, B, C any](name string, a Output[A], b Output[B], c Output[C], start func(context.Context, A, B, C) error, options ...Option) *Component {
	return run(name, []input{a, b, c}, func(ctx context.Context) error {
		return start(ctx, a.Value(), b.Value(), c.Value())
	}, options)
}

func Run4[A, B, C, D any](name string, a Output[A], b Output[B], c Output[C], d Output[D], start func(context.Context, A, B, C, D) error, options ...Option) *Component {
	return run(name, []input{a, b, c, d}, func(ctx context.Context) error {
		return start(ctx, a.Value(), b.Value(), c.Value(), d.Value())
	}, options)
}

func Run5[A, B, C, D, E any](name string, a Output[A], b Output[B], c Output[C], d Output[D], e Output[E], start func(context.Context, A, B, C, D, E) error, options ...Option) *Component {
	return run(name, []input{a, b, c, d, e}, func(ctx context.Context) error {
		return start(ctx, a.Value(), b.Value(), c.Value(), d.Value(), e.Value())
	}, options)
}

func Run6[A, B, C, D, E, F any](name string, a Output[A], b Output[B], c Output[C], d Output[D], e Output[E], f Output[F], start func(context.Context, A, B, C, D, E, F) error, options ...Option) *Component {
	return run(name, []input{a, b, c, d, e, f}, func(ctx context.Context) error {
		return start(ctx, a.Value(), b.Value(), c.Value(), d.Value(), e.Value(), f.Value())
	}, options)
}

func Run7[A, B, C, D, E, F, G any](name string, a Output[A], b Output[B], c Output[C], d Output[D], e Output[E], f Output[F], g Output[G], start func(context.Context, A, B, C, D, E, F, G) error, options ...Option) *Component {
	return run(name, []input{a, b, c, d, e, f, g}, func(ctx context.Context) error {
		return start(ctx, a.Value(), b.Value(), c.Value(), d.Value(), e.Value(), f.Value(), g.Value())
	}, options)
}
