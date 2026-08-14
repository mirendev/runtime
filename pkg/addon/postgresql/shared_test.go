package postgresql

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/saga"
)

func TestRegisterEnsureSharedServerSaga(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}

	err := RegisterEnsureSharedServerSaga(registry, fw)
	require.NoError(t, err)

	def, ok := registry.Get("ensure-shared-server")
	require.True(t, ok)
	assert.Equal(t, "ensure-shared-server", def.Name)
	assert.Len(t, def.Actions, 6)

	expectedActions := []string{
		"create-shared-server-entity",
		"create-shared-pool",
		"wait-for-shared-pool",
		"create-shared-service",
		"wait-for-shared-service",
		"activate-shared-server",
	}

	for _, name := range expectedActions {
		_, exists := def.Actions[name]
		assert.True(t, exists, "expected action %q to exist", name)
	}
}

func TestRegisterSharedSaga(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}

	err := RegisterSharedSaga(registry, fw)
	require.NoError(t, err)

	def, ok := registry.Get("provision-shared-postgresql")
	require.True(t, ok)
	assert.Equal(t, "provision-shared-postgresql", def.Name)
	assert.Len(t, def.Actions, 5)
}

func TestRegisterDeprovisionSharedSaga(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}

	err := RegisterDeprovisionSharedSaga(registry, fw)
	require.NoError(t, err)

	def, ok := registry.Get("deprovision-shared-postgresql")
	require.True(t, ok)
	assert.Equal(t, "deprovision-shared-postgresql", def.Name)
	assert.Len(t, def.Actions, 7)
}

func TestRegisterSharedSaga_IncludesNestedSaga(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}

	err := RegisterSharedSaga(registry, fw)
	require.NoError(t, err)

	// The nested ensure-shared-server saga should also be registered
	def, ok := registry.Get("ensure-shared-server")
	require.True(t, ok)
	assert.Equal(t, "ensure-shared-server", def.Name)
	assert.Len(t, def.Actions, 6)
}

func TestSharedSagaActionOrder(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}

	err := RegisterSharedSaga(registry, fw)
	require.NoError(t, err)

	def, ok := registry.Get("provision-shared-postgresql")
	require.True(t, ok)

	// Verify create-shared-user runs before create-shared-database.
	// The database is created with OWNER set to the user role, so the
	// role must exist first. This is enforced by CreateSharedUser outputting
	// SharedUsername which CreateSharedDatabase consumes as input.
	order := def.ExecutionOrder()
	indexOf := func(name string) int {
		for i, n := range order {
			if n == name {
				return i
			}
		}
		t.Fatalf("action %q not found in execution order", name)
		return -1
	}

	assert.Greater(t, indexOf("create-shared-user"), indexOf("find-or-create-shared-server"))
	assert.Greater(t, indexOf("create-shared-user"), indexOf("generate-shared-credentials"))
	assert.Greater(t, indexOf("create-shared-database"), indexOf("create-shared-user"),
		"create-shared-database must run after create-shared-user")
}

func TestDeprovisionSharedSagaActions(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}

	err := RegisterDeprovisionSharedSaga(registry, fw)
	require.NoError(t, err)

	def, ok := registry.Get("deprovision-shared-postgresql")
	require.True(t, ok)

	expectedActions := []string{
		"decode-shared-attrs",
		"lookup-shared-server",
		"terminate-connections",
		"drop-shared-database",
		"drop-shared-user",
		"decrement-association-count",
		"cleanup-shared-server",
	}

	for _, name := range expectedActions {
		_, exists := def.Actions[name]
		assert.True(t, exists, "expected action %q to exist", name)
	}
}

func TestDeprovisionSharedSagaOrder(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}

	err := RegisterDeprovisionSharedSaga(registry, fw)
	require.NoError(t, err)

	def, ok := registry.Get("deprovision-shared-postgresql")
	require.True(t, ok)

	order := def.ExecutionOrder()
	indexOf := func(name string) int {
		for i, n := range order {
			if n == name {
				return i
			}
		}
		t.Fatalf("action %q not found in execution order", name)
		return -1
	}

	assert.Greater(t, indexOf("terminate-connections"), indexOf("lookup-shared-server"),
		"terminate-connections must run after lookup-shared-server")
	assert.Greater(t, indexOf("drop-shared-database"), indexOf("terminate-connections"),
		"drop-shared-database must run after terminate-connections")
	assert.Greater(t, indexOf("drop-shared-user"), indexOf("terminate-connections"),
		"drop-shared-user must run after terminate-connections")
	assert.Greater(t, indexOf("decrement-association-count"), indexOf("drop-shared-database"),
		"decrement-association-count must run after drop-shared-database")
	assert.Greater(t, indexOf("decrement-association-count"), indexOf("drop-shared-user"),
		"decrement-association-count must run after drop-shared-user")
	assert.Greater(t, indexOf("cleanup-shared-server"), indexOf("decrement-association-count"),
		"cleanup-shared-server must run after decrement-association-count")
}
