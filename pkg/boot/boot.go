// Package boot starts components in dataflow order and passes their outputs to
// downstream components once startup succeeds.
//
// This is not a dynamic graph. It validates and starts one fixed set of
// components. Do any work that decides which components should participate up
// front, before the graph starts. Startup makes one forward pass, running
// independent components in parallel. Shutdown follows the same dataflow edges
// in reverse. The graph does not restart anything or run startup again.
//
// Use ProvideN for a component that publishes a value to downstream work. Its
// start callback must not return until that value is ready to use; the graph can
// order callback completion, but it cannot infer what readiness means for the
// value. Use RunN for side-effect-only work at the edge of the graph. A RunN
// callback may install long-lived background work and return because it
// publishes nothing for another component to consume.
package boot

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Component defines one participant in the boot dataflow graph.
type Component struct {
	name        string
	inputs      []input
	start       func(context.Context) error
	stop        StopFunc
	stopTimeout time.Duration
}

func (c *Component) String() string {
	if c == nil {
		return "<nil>"
	}
	return c.name
}

// input is a typed output from another component. ProvideN and RunN use these
// outputs both to record graph edges and to supply resolved callback arguments.
type input interface {
	producerComponent() *Component
}

// Output is a ready-to-use value published after its producer starts
// successfully.
type Output[T any] struct {
	producer *Component
	state    *outputState[T]
}

type outputState[T any] struct {
	mu        sync.RWMutex
	published bool
	value     T
}

// ResolvedOutput returns an already-published value for focused component
// tests that do not assemble a graph.
func ResolvedOutput[T any](value T) Output[T] {
	return Output[T]{state: &outputState[T]{published: true, value: value}}
}

func (o Output[T]) producerComponent() *Component {
	return o.producer
}

// Value returns a published output for callers outside the boot graph, such as
// the owner collecting top-level services after Graph.Start. Component start
// callbacks receive resolved values directly and do not need to call Value.
func (o Output[T]) Value() T {
	if o.state == nil {
		panic("boot: uninitialized output")
	}
	o.state.mu.RLock()
	defer o.state.mu.RUnlock()
	if !o.state.published {
		panic(fmt.Sprintf("boot: output from %q read before publication", o.producer))
	}
	return o.state.value
}

func (s *outputState[T]) publish(value T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.value = value
	s.published = true
}

// StopFunc stops a component.
type StopFunc func(context.Context) error

// Option adds lifecycle behavior to a provided or side-effect-only component.
type Option func(*Component)

// WithStop adds a stop function and its component-specific timeout. A zero
// timeout uses the Graph.Stop caller's context directly.
func WithStop(stop StopFunc, timeout time.Duration) Option {
	return func(component *Component) {
		component.stop = stop
		component.stopTimeout = timeout
	}
}

func newComponent(name string, inputs []input, start func(context.Context) error, options []Option) *Component {
	component := &Component{
		name:   name,
		inputs: append([]input(nil), inputs...),
		start:  start,
	}
	for _, option := range options {
		option(component)
	}
	return component
}

func provide[O any](name string, inputs []input, start func(context.Context) (O, error), options []Option) (*Component, Output[O]) {
	state := new(outputState[O])
	component := newComponent(name, inputs, func(ctx context.Context) error {
		value, err := start(ctx)
		if err != nil {
			return err
		}
		state.publish(value)
		return nil
	}, options)
	return component, Output[O]{producer: component, state: state}
}

func run(name string, inputs []input, start func(context.Context) error, options []Option) *Component {
	return newComponent(name, inputs, start, options)
}
