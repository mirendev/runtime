package boot

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
)

// Graph owns component startup and reverse shutdown ordering.
type Graph struct {
	mu sync.Mutex

	nodes     map[*Component]*node
	names     map[string]*Component
	layers    [][]*node
	validated bool
	running   bool
	stopped   bool
	cancel    context.CancelFunc
}

type node struct {
	component *Component
	attempted atomic.Bool
	completed completion
	graph     *Graph
}

type completion struct {
	done chan struct{}
}

func newCompletion() completion {
	return completion{done: make(chan struct{})}
}

func (c *completion) complete() {
	close(c.done)
}

func (c *completion) current() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

func (c *completion) wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return nil
	}
}

// NewGraph creates an empty boot graph.
func NewGraph() *Graph {
	return &Graph{
		nodes: make(map[*Component]*node),
		names: make(map[string]*Component),
	}
}

// Add registers a component with the graph. All components must be added
// before Validate or Start is called.
func (g *Graph) Add(component *Component) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.validated || g.running {
		return errors.New("boot: graph declarations are closed")
	}
	if component == nil {
		return errors.New("boot: component is nil")
	}
	if component.name == "" {
		return errors.New("boot: component name is empty")
	}
	if _, exists := g.names[component.name]; exists {
		return fmt.Errorf("boot: component %q is already declared", component)
	}

	g.nodes[component] = &node{
		component: component,
		completed: newCompletion(),
		graph:     g,
	}
	g.names[component.name] = component
	return nil
}

// Validate checks input producers and cycles without starting anything.
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
		for _, producer := range inputProducers(n.component) {
			if producer == n.component {
				return fmt.Errorf("boot: component %q consumes its own output", n.component)
			}
			if _, exists := g.nodes[producer]; !exists {
				return fmt.Errorf("boot: component %q has undeclared input producer %q", n.component, producer)
			}
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

func inputProducers(component *Component) []*Component {
	seen := make(map[*Component]struct{}, len(component.inputs))
	producers := make([]*Component, 0, len(component.inputs))
	for _, input := range component.inputs {
		if input == nil {
			continue
		}
		producer := input.producerComponent()
		// ResolvedOutput has no producer and therefore adds no graph edge.
		if producer == nil {
			continue
		}
		if _, duplicate := seen[producer]; duplicate {
			continue
		}
		seen[producer] = struct{}{}
		producers = append(producers, producer)
	}
	return producers
}

func (g *Graph) topologicalLayersLocked() ([][]*node, error) {
	indegree := make(map[*Component]int, len(g.nodes))
	dependents := make(map[*Component][]*Component, len(g.nodes))
	for component := range g.nodes {
		producers := inputProducers(component)
		indegree[component] = len(producers)
		for _, producer := range producers {
			dependents[producer] = append(dependents[producer], component)
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
		return nil, errors.New("boot: dataflow cycle detected")
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
