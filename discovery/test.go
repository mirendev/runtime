package discovery

import (
	"context"
	"time"
)

type slowLookup struct {
	l   Lookup
	dur time.Duration
}

func (s *slowLookup) Lookup(ctx context.Context, app string) (Endpoint, chan BackgroundLookup, error) {
	ch := make(chan BackgroundLookup, 1)

	go func() {
		defer close(ch)
		time.Sleep(s.dur)
		ep, _, err := s.l.Lookup(ctx, app)
		ch <- BackgroundLookup{ep, err}
	}()

	return nil, ch, nil
}

func SlowLookup(l Lookup, dur time.Duration) Lookup {
	return &slowLookup{l, dur}
}

type failLookup struct {
	err error
	bg  bool
}

func (f *failLookup) Lookup(ctx context.Context, app string) (Endpoint, chan BackgroundLookup, error) {
	if f.bg {
		ch := make(chan BackgroundLookup, 1)
		ch <- BackgroundLookup{nil, f.err}
		return nil, ch, nil
	}
	return nil, nil, f.err
}

func FailLookup(bg bool, err error) Lookup {
	return &failLookup{err, bg}
}
