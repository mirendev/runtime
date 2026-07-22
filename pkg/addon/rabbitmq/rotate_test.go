package rabbitmq

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/saga"
)

// TestRotateDedicatedSagaOrder pins the rotation ordering: resolve the pool and
// connection details, change the password inside the container, then record it
// on the entity only once the engine has taken it (gated by an edge on the
// change).
func TestRotateDedicatedSagaOrder(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}
	rc := &rotateCapture{}

	require.NoError(t, RegisterRotateDedicatedSaga(registry, fw, rc))

	def, ok := registry.Get("rotate-dedicated-rabbitmq")
	require.True(t, ok)
	assert.Len(t, def.Actions, 5)

	order := def.ExecutionOrder()
	indexOf := func(name string) int {
		for i, n := range order {
			if n == name {
				return i
			}
		}
		t.Fatalf("action %q not found in order %v", name, order)
		return -1
	}

	assert.Less(t, indexOf("decode-dedicated-attrs"), indexOf("load-rotation-state"),
		"must decode the server ref before looking it up")
	assert.Less(t, indexOf("load-rotation-state"), indexOf("change-password"),
		"must resolve the pool before changing the password")
	assert.Less(t, indexOf("change-password"), indexOf("update-server-password"),
		"entity must record the new password only after the engine takes it")
}
