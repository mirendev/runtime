package compute_v1alpha

import (
	"strings"

	entity "miren.dev/runtime/pkg/entity"
)

// nodeIdPrefix is the human-id prefix for the compute node kind (KindNode,
// "dev.miren.compute/kind.node"). This is the one and only place the prefix
// is written; every node id is built through NewNodeId.
const nodeIdPrefix = "node/"

// NodeId is the entity ID of a compute node, always in canonical
// "node/<raw>" form. Construct one only via NewNodeId, which is the single
// sanctioned way to apply the "node/" prefix; that discipline is what keeps
// a raw identifier from ever being mistaken for a prefixed one. Convert back
// to a plain entity.Id with Id() when handing it to the entity store.
type NodeId entity.Id

// NewNodeId builds a NodeId from a raw node identifier (a UUID, or "miren"
// for the primary node), normalizing so the "node/" prefix is present
// exactly once regardless of whether the input already carried it.
func NewNodeId(raw string) NodeId {
	// Strip any number of leading "node/" segments so a raw id, an already
	// prefixed id, and an accidentally double-prefixed one all collapse to
	// exactly one prefix.
	for strings.HasPrefix(raw, nodeIdPrefix) {
		raw = raw[len(nodeIdPrefix):]
	}
	return NodeId(nodeIdPrefix + raw)
}

// Id returns the node id as a plain entity.Id, for use with the entity store
// and any API that speaks entity.Id.
func (n NodeId) Id() entity.Id {
	return entity.Id(n)
}

// String returns the canonical string form, e.g. "node/abc123".
func (n NodeId) String() string {
	return string(n)
}

// Matches reports whether id refers to this node. Use it to compare a NodeId
// against a bare entity.Id read from an entity field (e.g. a disk volume's
// NodeId) without re-prefixing either side.
func (n NodeId) Matches(id entity.Id) bool {
	return entity.Id(n) == id
}
