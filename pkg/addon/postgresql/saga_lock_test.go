package postgresql

import (
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/saga"
	"miren.dev/runtime/pkg/saga/sagalock"
)

// TestSagaDefinitionsLocked fails when one of this package's sagas changes
// shape without a version decision. See pkg/saga/sagalock.
func TestSagaDefinitionsLocked(t *testing.T) {
	registry := saga.NewRegistry()
	require.NoError(t, RegisterDedicatedSaga(registry, nil))
	require.NoError(t, RegisterDeprovisionDedicatedSaga(registry, nil))
	require.NoError(t, RegisterSharedSaga(registry, nil))
	require.NoError(t, RegisterDeprovisionSharedSaga(registry, nil))
	require.NoError(t, RegisterRotateSharedUserSaga(registry, nil, nil))
	require.NoError(t, RegisterRotateSharedSuperuserSaga(registry, nil, nil))
	require.NoError(t, RegisterRotateDedicatedSaga(registry, nil, nil))

	sagalock.Check(t, registry)
}
