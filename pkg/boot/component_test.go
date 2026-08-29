package boot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateRejectsCycle(t *testing.T) {
	g := NewGraph()
	a, aOutput := Provide0("a", func(context.Context) (struct{}, error) { return struct{}{}, nil })
	b, bOutput := Provide0("b", func(context.Context) (struct{}, error) { return struct{}{}, nil })
	a.inputs = []input{bOutput}
	b.inputs = []input{aOutput}
	require.NoError(t, g.Add(a))
	require.NoError(t, g.Add(b))
	require.ErrorContains(t, g.Validate(), "dataflow cycle detected")
}

func TestAddRejectsDuplicateComponentName(t *testing.T) {
	g := NewGraph()
	require.NoError(t, g.Add(Run0("same", func(context.Context) error { return nil })))
	require.ErrorContains(t,
		g.Add(Run0("same", func(context.Context) error { return nil })),
		`component "same" is already declared`,
	)
}

func TestComponentOwnsItsInputList(t *testing.T) {
	producer, output := Provide0("producer", func(context.Context) (int, error) { return 1, nil })
	inputs := []input{output}
	component := newComponent("component", inputs, nil, nil)
	inputs[0] = ResolvedOutput(2)
	require.Same(t, producer, component.inputs[0].producerComponent())
}
