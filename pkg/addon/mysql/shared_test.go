package mysql

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/saga"
)

func TestRegisterSharedSaga(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}

	err := RegisterSharedSaga(registry, fw)
	require.NoError(t, err)

	def, ok := registry.Get("provision-shared-mysql")
	require.True(t, ok)
	assert.Equal(t, "provision-shared-mysql", def.Name)
	assert.Len(t, def.Actions, 5)
}

func TestRegisterEnsureSharedServerSaga(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}

	err := RegisterEnsureSharedServerSaga(registry, fw)
	require.NoError(t, err)

	def, ok := registry.Get("ensure-shared-mysql-server")
	require.True(t, ok)
	assert.Equal(t, "ensure-shared-mysql-server", def.Name)
	assert.Len(t, def.Actions, 6)
}

func TestRegisterDeprovisionSharedSaga(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}

	err := RegisterDeprovisionSharedSaga(registry, fw)
	require.NoError(t, err)

	def, ok := registry.Get("deprovision-shared-mysql")
	require.True(t, ok)
	assert.Equal(t, "deprovision-shared-mysql", def.Name)
	assert.Len(t, def.Actions, 7)
}

func TestQuoteIdentifier(t *testing.T) {
	assert.Equal(t, "`mydb`", quoteIdentifier("mydb"))
	assert.Equal(t, "`my``db`", quoteIdentifier("my`db"))
	assert.Equal(t, "`my-app`", quoteIdentifier("my-app"))
}
