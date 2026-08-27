package boot

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Start validates the graph, starts independent components concurrently, and
// waits until every component has returned successfully.
func (g *Graph) Start(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return errors.New("boot: graph is already running")
	}
	if err := g.validateLocked(); err != nil {
		g.mu.Unlock()
		return err
	}
	g.running = true
	startCtx, cancel := context.WithCancel(ctx)
	g.cancel = cancel
	nodes := make([]*node, 0, len(g.nodes))
	for _, n := range g.nodes {
		nodes = append(nodes, n)
	}
	g.mu.Unlock()

	errCh := make(chan error, len(nodes))
	var wg sync.WaitGroup
	for _, n := range nodes {
		wg.Go(func() {
			if err := n.start(startCtx); err != nil {
				errCh <- fmt.Errorf("starting %s: %w", n.component, err)
				cancel()
			}
		})
	}

	wg.Wait()
	close(errCh)

	var errs []error
	var substantive []error
	for err := range errCh {
		errs = append(errs, err)
		if !errors.Is(err, context.Canceled) {
			substantive = append(substantive, err)
		}
	}
	if len(substantive) > 0 {
		return errors.Join(substantive...)
	}
	return errors.Join(errs...)
}

func (n *node) start(ctx context.Context) error {
	for _, producer := range inputProducers(n.component) {
		if err := n.graph.nodes[producer].completed.wait(ctx); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if n.component.start != nil {
		n.attempted.Store(true)
		if err := n.component.start(ctx); err != nil {
			return err
		}
	}
	n.completed.complete()
	return nil
}

// Stop stops successfully started components in reverse dataflow order.
func (g *Graph) Stop(ctx context.Context) error {
	g.mu.Lock()
	if !g.validated || g.stopped {
		g.mu.Unlock()
		return nil
	}
	g.stopped = true
	layers := append([][]*node(nil), g.layers...)
	cancel := g.cancel
	g.mu.Unlock()
	if cancel != nil {
		// Ask context-owned work to quiesce before explicit teardown starts.
		cancel()
	}

	var errs []error
	for i := len(layers) - 1; i >= 0; i-- {
		var wg sync.WaitGroup
		errCh := make(chan error, len(layers[i]))
		for _, n := range layers[i] {
			if !n.attempted.Load() && !n.completed.current() {
				continue
			}
			if n.component.stop == nil {
				continue
			}
			wg.Go(func() {
				stopCtx := ctx
				cancelStop := func() {}
				if n.component.stopTimeout > 0 {
					stopCtx, cancelStop = context.WithTimeout(ctx, n.component.stopTimeout)
				}
				defer cancelStop()
				if err := n.component.stop(stopCtx); err != nil {
					errCh <- fmt.Errorf("stopping %s: %w", n.component, err)
				}
			})
		}
		wg.Wait()
		close(errCh)
		for err := range errCh {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
