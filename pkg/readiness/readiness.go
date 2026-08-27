// Package readiness starts components in a safe order and tracks when the
// system is ready for work such as builds, sandboxes, and traffic.
//
// This is not a dynamic graph. It validates and starts one fixed set of
// components. Do any work that decides which components should participate up
// front, before the graph starts.
// Startup makes one forward pass, running independent components in parallel.
// Shutdown follows the dependencies in reverse. The graph does not restart
// anything or run startup again.
package readiness

import (
	"context"
	"time"
)

// Component defines one participant in the startup dependency graph.
type Component struct {
	name string
	spec Spec
}

// NewComponent defines a component and its lifecycle.
func NewComponent(name string, spec Spec) *Component {
	spec.Dependencies = append([]Dependency(nil), spec.Dependencies...)
	return &Component{name: name, spec: spec}
}

func (c *Component) String() string {
	if c == nil {
		return "<nil>"
	}
	return c.name
}

// Condition names something consumers may need the system to be ready to do,
// such as running builds or launching sandboxes. Each graph decides which
// components must be ready for the condition to hold.
type Condition struct {
	name string
}

// NewCondition defines a typed condition handle.
func NewCondition(name string) Condition {
	return Condition{name: name}
}

func (c Condition) String() string {
	return c.name
}

type dependencyState uint8

const (
	dependencyStarted dependencyState = iota
	dependencyReady
)

// Dependency is an ordering edge between two components.
type Dependency struct {
	component *Component
	state     dependencyState
}

// StartDep allows startup after the dependency has published any outputs that
// are safe to use, even if it is not ready yet.
func StartDep(component *Component) Dependency {
	return Dependency{component: component, state: dependencyStarted}
}

// ReadyDep allows startup after the dependency reports that it is ready.
func ReadyDep(component *Component) Dependency {
	return Dependency{component: component, state: dependencyReady}
}

// Reporter publishes lifecycle state for a component. It remains valid after
// Start returns so a component can report runtime readiness changes later.
// Calling Ready or NotReady during Start opts the component into explicitly
// managed readiness; otherwise a successful Start return implies readiness.
type Reporter interface {
	Started()
	Ready()
	NotReady()
}

// StartFunc starts a component. A successful return implicitly reports Started
// and, unless Ready or NotReady was called, Ready.
type StartFunc func(context.Context, Reporter) error

// StopFunc stops a component.
type StopFunc func(context.Context) error

// Waiter is the consumer-facing readiness surface.
type Waiter interface {
	Await(context.Context, Condition) error
}

// Spec describes one component and its dependencies.
type Spec struct {
	Dependencies []Dependency
	Start        StartFunc
	Stop         StopFunc
	// StopTimeout gives this component its own teardown budget within the
	// caller's overall Stop deadline. Zero uses the caller's context directly.
	StopTimeout time.Duration
}
