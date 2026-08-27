package readiness

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
		return errors.New("readiness: graph is already running")
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
	for _, dep := range n.component.spec.Dependencies {
		dependency := n.graph.nodes[dep.component]
		var err error
		switch dep.state {
		case dependencyStarted:
			err = dependency.started.wait(ctx)
		case dependencyReady:
			err = dependency.ready.wait(ctx)
		default:
			err = errors.New("unknown dependency state")
		}
		if err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if n.component.spec.Start != nil {
		n.attempted.set(true)
		if err := n.component.spec.Start(ctx, (*reporter)(n)); err != nil {
			return err
		}
	}

	n.reportStarted()
	n.completeStart()
	return nil
}

type reporter node

func (r *reporter) Started() {
	(*node)(r).reportStarted()
}

func (r *reporter) Ready() {
	n := (*node)(r)
	n.reportStarted()
	n.reportReady(true)
}

func (r *reporter) NotReady() {
	(*node)(r).reportReady(false)
}

func (n *node) reportStarted() {
	n.started.set(true)
}

func (n *node) reportReady(value bool) {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	n.readinessManaged = true
	n.setReadyLocked(value)
}

func (n *node) completeStart() {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	if n.readinessManaged {
		return
	}
	n.setReadyLocked(true)
}

func (n *node) setReadyLocked(value bool) {
	if value && n.graph.stopped {
		return
	}
	if !n.ready.set(value) {
		return
	}
	n.graph.recomputeConditionsLocked(n.component)
}

// Stop stops successfully started components in reverse dependency order.
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
			if !n.attempted.current() && !n.started.current() {
				continue
			}
			n.dropReady()
			if n.component.spec.Stop == nil {
				continue
			}
			wg.Go(func() {
				stopCtx := ctx
				cancelStop := func() {}
				if timeout := n.component.spec.StopTimeout; timeout > 0 {
					stopCtx, cancelStop = context.WithTimeout(ctx, timeout)
				}
				defer cancelStop()
				if err := n.component.spec.Stop(stopCtx); err != nil {
					errCh <- fmt.Errorf("stopping %s: %w", n.component, err)
					return
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

func (n *node) dropReady() {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	n.setReadyLocked(false)
}
