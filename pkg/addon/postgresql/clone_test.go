package postgresql

import (
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/saga"
)

func TestCloneSagaGraphs(t *testing.T) {
	fw := &addon.ProviderFramework{}
	require.NoError(t, registerCloneSharedSaga(saga.NewRegistry(), fw))
	require.NoError(t, registerCloneDedicatedSaga(saga.NewRegistry(), fw))
}
