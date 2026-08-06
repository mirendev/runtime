package saga

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Execution IDs are the handle an operator types into `miren debug saga show`,
// and they should look like every other entity ID in the system rather than the
// bare "sagaCdjk…" blob they used to be. Pin the shape so it does not drift.
func TestGeneratedExecutionIDShape(t *testing.T) {
	id := generateID()

	kind, name, ok := strings.Cut(id, "/")
	require.True(t, ok, "id %q has no kind namespace", id)
	assert.Equal(t, "saga", kind)

	prefix, body, ok := strings.Cut(name, "-")
	require.True(t, ok, "id %q has no separator before the base58 body", id)
	assert.Equal(t, "sg", prefix)
	assert.NotEmpty(t, body)

	assert.NotEqual(t, generateID(), id, "generated IDs must be unique")
}

func TestDerivedChildIDShape(t *testing.T) {
	id := deriveChildID("saga/sg-Parent1", "child-saga", "parent-step")

	assert.True(t, strings.HasPrefix(id, "saga/sg-"), "derived id %q", id)

	// Determinism is what makes nested-saga recovery idempotent, so the shape
	// change must not have made it depend on anything but its inputs.
	assert.Equal(t, id, deriveChildID("saga/sg-Parent1", "child-saga", "parent-step"))
	assert.NotEqual(t, id, deriveChildID("saga/sg-Parent2", "child-saga", "parent-step"))
}
