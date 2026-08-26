//go:build linux

package server

import (
	"context"
	"sync"
)

// initialSweepGate is a process-local, one-shot rendezvous between migration
// and the first entity-sync snapshot. It is separate from boot because a
// transient migration failure must retry without holding Runtime.Start open.
type initialSweepGate struct {
	done chan struct{}
	once sync.Once
}

func newInitialSweepGate() *initialSweepGate {
	return &initialSweepGate{done: make(chan struct{})}
}

func (g *initialSweepGate) Complete() {
	g.once.Do(func() { close(g.done) })
}

func (g *initialSweepGate) Wait(ctx context.Context) error {
	select {
	case <-g.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
