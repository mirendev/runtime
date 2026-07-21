package postgresql

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

	err := RegisterRotateSharedUserSaga(registry, fw, rc)
	require.NoError(t, err)

	def, ok := registry.Get("rotate-shared-postgresql-user")
	require.True(t, ok)
	assert.Len(t, def.Actions, 5)

	indexOf := orderIndexer(t, def.ExecutionOrder())
	assert.Less(t, indexOf("decode-shared-attrs"), indexOf("lookup-shared-server"),
		"must decode the server ref before looking it up")
	assert.Less(t, indexOf("lookup-shared-server"), indexOf("alter-shared-user-password"),
		"must resolve the superuser connection before altering the user")
}

// TestRotateSharedSuperuserSagaOrder pins the safety ordering enforced by edges:
// record the (password-derived) disk name before the password changes, and only
// record the new password on the entity once the engine has actually taken it.
func TestRotateSharedSuperuserSagaOrder(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}
	rc := &rotateCapture{}

	err := RegisterRotateSharedSuperuserSaga(registry, fw, rc)
	require.NoError(t, err)

	def, ok := registry.Get("rotate-shared-postgresql-superuser")
	require.True(t, ok)
	assert.Len(t, def.Actions, 6)

	indexOf := orderIndexer(t, def.ExecutionOrder())
	assert.Less(t, indexOf("backfill-superuser-disk-name"), indexOf("alter-superuser-password"),
		"must record the disk name before the superuser password changes")
	assert.Less(t, indexOf("alter-superuser-password"), indexOf("update-superuser-entity"),
		"entity must record the new password only after the engine takes it")
}
