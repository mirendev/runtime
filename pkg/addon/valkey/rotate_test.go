package valkey

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/saga"
)

func TestRegisterRotateDedicatedSaga(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}
	rc := &rotateCapture{}

	err := RegisterRotateDedicatedSaga(registry, fw, rc)
	require.NoError(t, err)

	def, ok := registry.Get("rotate-dedicated-valkey")
	require.True(t, ok)
	assert.Equal(t, "rotate-dedicated-valkey", def.Name)
	assert.Len(t, def.Actions, 8)
}

// TestRotateDedicatedSagaOrder pins the safety-critical ordering: the disk is
// single-attach, so the old pool must scale down before the new one is created,
// and the old pool must not be deleted until the new one is confirmed ready.
// Both are enforced by saga edges, not data dependencies.
func TestRotateDedicatedSagaOrder(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}
	rc := &rotateCapture{}

	err := RegisterRotateDedicatedSaga(registry, fw, rc)
	require.NoError(t, err)

	def, ok := registry.Get("rotate-dedicated-valkey")
	require.True(t, ok)

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

	assert.Less(t, indexOf("decode-valkey-server-ref"), indexOf("load-valkey-rotation-state"),
		"must decode the server ref before loading it")
	assert.Less(t, indexOf("load-valkey-rotation-state"), indexOf("scale-down-old-valkey-pool"),
		"must load the pool ref before scaling it down")
	assert.Less(t, indexOf("scale-down-old-valkey-pool"), indexOf("swap-valkey-pool"),
		"must free the single-attach disk before the new pool attaches it")
	assert.Less(t, indexOf("swap-valkey-pool"), indexOf("wait-valkey-pool-ready"),
		"must create the new pool before waiting on it")
	assert.Less(t, indexOf("wait-valkey-pool-ready"), indexOf("delete-old-valkey-pool"),
		"must confirm the new pool is ready before deleting the old one")
}
