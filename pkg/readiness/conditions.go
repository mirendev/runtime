package readiness

import (
	"context"
	"errors"
	"fmt"
)

type conditionState struct {
	condition Condition
	providers []*node
	ready     level
}

// AddCondition declares a named condition as the AND of its providers.
func (g *Graph) AddCondition(condition Condition, providers ...*Component) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.validated || g.running {
		return errors.New("readiness: graph declarations are closed")
	}
	if condition.name == "" {
		return errors.New("readiness: condition name is empty")
	}
	if _, exists := g.conditions[condition]; exists {
		return fmt.Errorf("readiness: condition %q is already declared", condition)
	}
	if len(providers) == 0 {
		return fmt.Errorf("readiness: condition %q has no providers", condition)
	}

	providerNodes := make([]*node, 0, len(providers))
	seen := make(map[*Component]struct{}, len(providers))
	for _, provider := range providers {
		if provider == nil {
			return fmt.Errorf("readiness: condition %q has nil provider", condition)
		}
		if _, duplicate := seen[provider]; duplicate {
			return fmt.Errorf("readiness: condition %q repeats provider %q", condition, provider)
		}
		seen[provider] = struct{}{}

		n, exists := g.nodes[provider]
		if !exists {
			return fmt.Errorf("readiness: condition %q has undeclared provider %q", condition, provider)
		}
		providerNodes = append(providerNodes, n)
	}

	g.conditions[condition] = &conditionState{
		condition: condition,
		providers: providerNodes,
		ready:     newLevel(),
	}
	return nil
}

func (g *Graph) recomputeConditionsLocked(changed *Component) {
	for _, condition := range g.conditions {
		usesChanged := false
		allReady := true
		for _, provider := range condition.providers {
			if provider.component == changed {
				usesChanged = true
			}
			if !provider.ready.current() {
				allReady = false
			}
		}
		if usesChanged {
			condition.ready.set(allReady)
		}
	}
}

// Await blocks until every provider of condition is ready or ctx ends.
func (g *Graph) Await(ctx context.Context, condition Condition) error {
	g.mu.Lock()
	state, exists := g.conditions[condition]
	g.mu.Unlock()
	if !exists {
		return fmt.Errorf("readiness: condition %q is not declared", condition)
	}
	return state.ready.wait(ctx)
}

// Ready reports the current value of a condition.
func (g *Graph) Ready(condition Condition) (bool, error) {
	g.mu.Lock()
	state, exists := g.conditions[condition]
	g.mu.Unlock()
	if !exists {
		return false, fmt.Errorf("readiness: condition %q is not declared", condition)
	}
	return state.ready.current(), nil
}
