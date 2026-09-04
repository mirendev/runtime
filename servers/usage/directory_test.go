package usage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/entity"
)

// matchNode accepts any of four identifiers because operators type a name while
// scripts hold an id. That flexibility creates a collision risk: one node's name
// can equal another's runner id. Ranging the map and taking the first match made
// the answer depend on Go's randomized iteration order, so a refreshing view
// could alternate between two hosts for the same argument.
func TestMatchNodeResolvesByPrecedenceNotMapOrder(t *testing.T) {
	// "shared" is the display name of one node and the runner id of another.
	nodes := map[entity.Id]*nodeInfo{
		"node/a": {id: "node/a", runnerID: "runner-a", name: "shared"},
		"node/b": {id: "node/b", runnerID: "shared", name: "b-name"},
	}

	// Runner id outranks name, so the answer is node/b every time. Repeated
	// because a single call can pass by luck under map randomization.
	for i := 0; i < 50; i++ {
		got := matchNode(nodes, "shared")
		require.NotNil(t, got)
		assert.Equal(t, entity.Id("node/b"), got.id,
			"runner id must outrank display name, on every call")
	}
}

func TestMatchNodeAcceptsEveryIdentifier(t *testing.T) {
	nodes := map[entity.Id]*nodeInfo{
		"node/a": {id: "node/a", runnerID: "runner-a", name: "alpha"},
	}

	r := require.New(t)
	r.Equal(entity.Id("node/a"), matchNode(nodes, "node/a").id)
	r.Equal(entity.Id("node/a"), matchNode(nodes, "runner-a").id)
	r.Equal(entity.Id("node/a"), matchNode(nodes, "alpha").id)

	r.Nil(matchNode(nodes, "nope"))
	r.Nil(matchNode(nodes, ""), "an empty query is not a match against an unnamed node")
}
