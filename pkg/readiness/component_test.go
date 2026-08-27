package readiness

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateRejectsCycle(t *testing.T) {
	g := NewGraph()
	a := NewComponent("a", Spec{})
	b := NewComponent("b", Spec{})
	a.spec.Dependencies = []Dependency{ReadyDep(b)}
	b.spec.Dependencies = []Dependency{ReadyDep(a)}
	require.NoError(t, g.Add(a))
	require.NoError(t, g.Add(b))
	require.ErrorContains(t, g.Validate(), "dependency cycle detected")
}

func TestAddRejectsDuplicateComponentName(t *testing.T) {
	g := NewGraph()
	require.NoError(t, g.Add(NewComponent("same", Spec{})))
	require.ErrorContains(t, g.Add(NewComponent("same", Spec{})), `component "same" is already declared`)
}

func TestNewComponentOwnsItsDependencyList(t *testing.T) {
	first := NewComponent("first", Spec{})
	second := NewComponent("second", Spec{})
	dependencies := []Dependency{ReadyDep(first)}
	component := NewComponent("component", Spec{Dependencies: dependencies})

	dependencies[0] = ReadyDep(second)
	require.Same(t, first, component.spec.Dependencies[0].component)
}
