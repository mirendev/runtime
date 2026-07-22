package mysql

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/saga"
)

func orderIndexer(t *testing.T, order []string) func(string) int {
	return func(name string) int {
		for i, n := range order {
			if n == name {
				return i
			}
		}
		t.Fatalf("action %q not found in order %v", name, order)
		return -1
	}
}

func TestRegisterRotateSharedUserSaga(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}
	rc := &rotateCapture{}

	require.NoError(t, RegisterRotateSharedUserSaga(registry, fw, rc))

	def, ok := registry.Get("rotate-shared-mysql-user")
	require.True(t, ok)
	assert.Len(t, def.Actions, 5)

	indexOf := orderIndexer(t, def.ExecutionOrder())
	assert.Less(t, indexOf("decode-shared-attrs"), indexOf("lookup-shared-server"),
		"must decode the server ref before looking it up")
	assert.Less(t, indexOf("lookup-shared-server"), indexOf("alter-shared-user-password"),
		"must resolve the root connection before altering the user")
}

// TestRotateSharedRootSagaOrder pins the safety ordering enforced by edges:
// record the (password-derived) disk name before the password changes, and only
// record the new password on the entity once the engine has actually taken it.
func TestRotateSharedRootSagaOrder(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}
	rc := &rotateCapture{}

	require.NoError(t, RegisterRotateSharedRootSaga(registry, fw, rc))

	def, ok := registry.Get("rotate-shared-mysql-root")
	require.True(t, ok)
	assert.Len(t, def.Actions, 6)

	indexOf := orderIndexer(t, def.ExecutionOrder())
	assert.Less(t, indexOf("backfill-root-disk-name"), indexOf("alter-shared-root-password"),
		"must record the disk name before the root password changes")
	assert.Less(t, indexOf("alter-shared-root-password"), indexOf("update-shared-root-entity"),
		"entity must record the new password only after the engine takes it")
}

func TestRotateDedicatedUserSagaOrder(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}
	rc := &rotateCapture{}

	require.NoError(t, RegisterRotateDedicatedUserSaga(registry, fw, rc))

	def, ok := registry.Get("rotate-dedicated-mysql-user")
	require.True(t, ok)
	assert.Len(t, def.Actions, 5)

	indexOf := orderIndexer(t, def.ExecutionOrder())
	assert.Less(t, indexOf("decode-dedicated-attrs"), indexOf("load-dedicated-rotation-state"),
		"must decode the server ref before looking it up")
	assert.Less(t, indexOf("load-dedicated-rotation-state"), indexOf("alter-dedicated-user-password"),
		"must resolve the root connection before altering the user")
	assert.Less(t, indexOf("capture-dedicated-conn-info"), indexOf("alter-dedicated-user-password"),
		"must know the role before altering")
}

// TestRotateDedicatedRootSagaOrder pins that the entity only records the new root
// password after the engine has taken it (gated by an edge on the ALTER).
func TestRotateDedicatedRootSagaOrder(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}
	rc := &rotateCapture{}

	require.NoError(t, RegisterRotateDedicatedRootSaga(registry, fw, rc))

	def, ok := registry.Get("rotate-dedicated-mysql-root")
	require.True(t, ok)
	assert.Len(t, def.Actions, 5)

	indexOf := orderIndexer(t, def.ExecutionOrder())
	assert.Less(t, indexOf("load-dedicated-rotation-state"), indexOf("alter-dedicated-root-password"),
		"must resolve the connection before altering root")
	assert.Less(t, indexOf("alter-dedicated-root-password"), indexOf("update-dedicated-root-entity"),
		"entity must record the new password only after the engine takes it")
}
