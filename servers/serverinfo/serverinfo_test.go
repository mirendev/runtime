package serverinfo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/server/server_v1alpha"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/serverinfo"
)

type noopCall struct{}

func (noopCall) NewClient(*rpc.Capability) rpc.Client         { return nil }
func (noopCall) Args(any)                                     {}
func (noopCall) Results(any)                                  {}
func (noopCall) NewCapability(*rpc.Interface) *rpc.Capability { return nil }
func (noopCall) IsAuthenticated() bool                        { return false }

func TestVersionReportsSourceSnapshot(t *testing.T) {
	source := serverinfo.New()
	srv := NewServer(source)

	call := func() *server_v1alpha.ServerInfoVersionResults {
		state := &server_v1alpha.ServerInfoVersion{Call: noopCall{}}
		require.NoError(t, srv.Version(context.Background(), state))
		return state.Results()
	}

	before := call()
	data, err := before.MarshalJSON()
	require.NoError(t, err)
	require.Contains(t, string(data), source.InstanceID())
	require.Contains(t, string(data), `"ready":false`)

	source.MarkReady()
	after := call()
	data, err = after.MarshalJSON()
	require.NoError(t, err)
	require.Contains(t, string(data), `"ready":true`)
	require.NotContains(t, string(data), `"started_at":null`)
}
