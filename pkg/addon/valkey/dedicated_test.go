package valkey

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/saga"
)

func TestRegisterDedicatedSaga(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}

	err := RegisterDedicatedSaga(registry, fw)
	require.NoError(t, err)

	def, ok := registry.Get("provision-dedicated-valkey")
	require.True(t, ok)
	assert.Equal(t, "provision-dedicated-valkey", def.Name)
	assert.Len(t, def.Actions, 7)
}

func TestRegisterDeprovisionDedicatedSaga(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}

	err := RegisterDeprovisionDedicatedSaga(registry, fw)
	require.NoError(t, err)

	def, ok := registry.Get("deprovision-dedicated-valkey")
	require.True(t, ok)
	assert.Equal(t, "deprovision-dedicated-valkey", def.Name)
	assert.Len(t, def.Actions, 5)
}

func TestDeprovisionDedicatedSagaOrder(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}

	err := RegisterDeprovisionDedicatedSaga(registry, fw)
	require.NoError(t, err)

	def, ok := registry.Get("deprovision-dedicated-valkey")
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

	assert.Less(t, indexOf("decode-dedicated-attrs"), indexOf("lookup-dedicated-server"),
		"decode-dedicated-attrs must come before lookup-dedicated-server")
	assert.Less(t, indexOf("lookup-dedicated-server"), indexOf("delete-dedicated-service"),
		"lookup-dedicated-server must come before delete-dedicated-service")
	assert.Less(t, indexOf("lookup-dedicated-server"), indexOf("delete-dedicated-pool"),
		"lookup-dedicated-server must come before delete-dedicated-pool")
	assert.Less(t, indexOf("delete-dedicated-pool"), indexOf("delete-dedicated-server-entity"),
		"delete-dedicated-pool must come before delete-dedicated-server-entity")
}

func TestDedicatedSagaActionOrder(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}

	err := RegisterDedicatedSaga(registry, fw)
	require.NoError(t, err)

	def, ok := registry.Get("provision-dedicated-valkey")
	require.True(t, ok)

	// Order comes from the data dependencies between actions, not from the order
	// they were registered in. Worth pinning rather than just asserting
	// membership: deleting an action, as dropping the result-building step did,
	// changes the graph the topological sort reads.
	expectedOrder := []string{
		"generate-credentials",
		"create-dedicated-pool",
		"create-dedicated-service",
		"create-valkey-server",
		"wait-for-dedicated-pool",
		"wait-for-dedicated-service",
		"update-dedicated-server",
	}

	assert.Equal(t, expectedOrder, def.ExecutionOrder())
}
