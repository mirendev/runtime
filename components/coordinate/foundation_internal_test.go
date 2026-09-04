package coordinate

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/rpc"
)

func TestFoundationStopClearsReleasedState(t *testing.T) {
	state, err := rpc.NewState(t.Context(), rpc.WithSkipVerify)
	require.NoError(t, err)
	foundation := &Foundation{state: state}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	require.NoError(t, foundation.Stop(shutdownCtx))
	require.Nil(t, foundation.state)
	require.Nil(t, foundation.etcdClient)
	require.Nil(t, foundation.store)
	require.Nil(t, foundation.etcdStore)
	require.Nil(t, foundation.eac)
	require.NoError(t, foundation.Stop(shutdownCtx))
}
