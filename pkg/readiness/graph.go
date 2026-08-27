package readiness

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
)

// Graph owns component startup order and named readiness conditions.
type Graph struct {
	mu sync.Mutex

	nodes      map[*Component]*node
	names      map[string]*Component
	conditions map[Condition]*conditionState
	layers     [][]*node
	validated  bool
	running    bool
	stopped    bool
	cancel     context.CancelFunc
}

type node struct {
	component *Component

	attempted level
	started   level
	ready     level
	// readinessManaged is true after the component reports Ready or NotReady.
	// Returning from Start only marks a component ready automatically when it
	// has not reported its own readiness. Protected by graph.mu.
	readinessManaged bool

	graph *Graph
}

type level struct {
	mu      sync.Mutex
	value   bool
	changed chan struct{}
}

func newLevel() level {
	return level{changed: make(chan struct{})}
}

func (l *level) set(value bool) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.value == value {
		return false
	}

	l.value = value
	close(l.changed)
	l.changed = make(chan struct{})
	return true
}

func (l *level) current() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.value
}

func (l *level) wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		if l.value {
			l.mu.Unlock()
			return nil
		}
		changed := l.changed
		l.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

// NewGraph creates an empty startup graph.
func NewGraph() *Graph {
	return &Graph{
		nodes:      make(map[*Component]*node),
		names:      make(map[string]*Component),
		conditions: make(map[Condition]*conditionState),
	}
}

// Add registers a component with the graph. All components must be added
// before Validate or Start is called.
func (g *Graph) Add(component *Component) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.validated || g.running {
		return errors.New("readiness: graph declarations are closed")
	}
	if component == nil {
		return errors.New("readiness: component is nil")
	}
	if component.name == "" {
		return errors.New("readiness: component name is empty")
	}
	if _, exists := g.names[component.name]; exists {
		return fmt.Errorf("readiness: component %q is already declared", component)
	}

	g.nodes[component] = &node{
		component: component,
		attempted: newLevel(),
		started:   newLevel(),
		ready:     newLevel(),
		graph:     g,
	}
	g.names[component.name] = component
	return nil
}

// Validate checks dependency references and cycles without starting anything.
func (g *Graph) Validate() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.validateLocked()
}

func (g *Graph) validateLocked() error {
	if g.validated {
		return nil
	}

	for _, n := range g.nodes {
		seen := make(map[*Component]struct{}, len(n.component.spec.Dependencies))
		for _, dep := range n.component.spec.Dependencies {
			if dep.component == nil {
				return fmt.Errorf("readiness: component %q has nil dependency", n.component)
			}
			if dep.component == n.component {
				return fmt.Errorf("readiness: component %q depends on itself", n.component)
			}
			if _, exists := g.nodes[dep.component]; !exists {
				return fmt.Errorf("readiness: component %q has undeclared dependency %q", n.component, dep.component)
			}
			if _, duplicate := seen[dep.component]; duplicate {
				return fmt.Errorf("readiness: component %q repeats dependency %q", n.component, dep.component)
			}
			seen[dep.component] = struct{}{}
		}
	}

	layers, err := g.topologicalLayersLocked()
	if err != nil {
		return err
	}
	g.layers = layers
	g.validated = true
	return nil
}

func (g *Graph) topologicalLayersLocked() ([][]*node, error) {
	indegree := make(map[*Component]int, len(g.nodes))
	dependents := make(map[*Component][]*Component, len(g.nodes))
	for component, n := range g.nodes {
		indegree[component] = len(n.component.spec.Dependencies)
		for _, dep := range n.component.spec.Dependencies {
			dependents[dep.component] = append(dependents[dep.component], component)
		}
	}

	var ready []*Component
	for component, degree := range indegree {
		if degree == 0 {
			ready = append(ready, component)
		}
	}
	slices.SortFunc(ready, func(a, b *Component) int {
		return compareName(a.name, b.name)
	})

	var layers [][]*node
	visited := 0
	for len(ready) > 0 {
		current := ready
		ready = nil
		layer := make([]*node, 0, len(current))
		for _, component := range current {
			visited++
			layer = append(layer, g.nodes[component])
			for _, dependent := range dependents[component] {
				indegree[dependent]--
				if indegree[dependent] == 0 {
					ready = append(ready, dependent)
				}
			}
		}
		slices.SortFunc(ready, func(a, b *Component) int {
			return compareName(a.name, b.name)
		})
		layers = append(layers, layer)
	}

	if visited != len(g.nodes) {
		return nil, errors.New("readiness: dependency cycle detected")
	}
	return layers, nil
}

func compareName(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
