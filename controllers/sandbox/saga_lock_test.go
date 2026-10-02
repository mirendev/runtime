package sandbox

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
	require.NoError(t, registerCreateSandboxSaga(registry, nil, nil, nil, nil, "", nil))

	sagalock.Check(t, registry)
}
