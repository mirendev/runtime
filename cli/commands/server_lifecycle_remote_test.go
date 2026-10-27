package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/server/server_v1alpha"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/serverlifecycle"
)

func TestTargetsLocalServer(t *testing.T) {
	local := func(address string) *Context {
		c := &Context{}
		c.Config.ServerAddress = address
		return c
	}
	require.True(t, local("127.0.0.1:8443").targetsLocalServer())
	require.True(t, local("localhost:8443").targetsLocalServer())
	require.True(t, local("[::1]:8443").targetsLocalServer())
	require.False(t, local("10.0.0.5:8443").targetsLocalServer())

	remote := local("127.0.0.1:8443")
	remote.ClusterConfig = &clientconfig.ClusterConfig{Hostname: "prod.example.com:8443"}
	require.False(t, remote.targetsLocalServer(), "a configured remote cluster is not this host, whatever the default address says")
	localCluster := local("10.0.0.5:8443")
	localCluster.ClusterConfig = &clientconfig.ClusterConfig{Hostname: "localhost:8443"}
	require.True(t, localCluster.targetsLocalServer())
	viaCloud := local("127.0.0.1:8443")
	viaCloud.ClusterConfig = &clientconfig.ClusterConfig{ViaCloud: true}
	require.False(t, viaCloud.targetsLocalServer(), "a cloud-routed cluster has no hostname and is not this host")
	nameless := local("127.0.0.1:8443")
	nameless.ClusterConfig = &clientconfig.ClusterConfig{}
	require.False(t, nameless.targetsLocalServer())
}

func TestAnsweredTellsServerErrorsFromUnreachable(t *testing.T) {
	require.True(t, answered(cond.NotFound("lifecycle operation", "x")))
	require.True(t, answered(cond.RemoteError("generic", "unknown", "boom")))
	require.True(t, answered(cond.RemoteError("permission", "forbidden", "denied")))
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		err := rpc.NewResolveStatusError(serverLifecycleService, "localhost:8443", status)
		require.True(t, answered(fmt.Errorf("connecting: %w", err)))
	}
	require.False(t, answered(rpc.NewResolveStatusError(serverLifecycleService, "localhost:8443", http.StatusServiceUnavailable)))
	require.False(t, answered(rpc.NewResolveHTTPError(errors.New("connection reset"), "restarting")))
	require.False(t, answered(errors.New("dial tcp: connection refused")))
}

type lifecycleDenyAuthorizer struct{}

func (lifecycleDenyAuthorizer) Authorize(context.Context, *rpc.Identity, string, string) error {
	return errors.New("lifecycle access denied")
}

func TestFollowRemoteOperationStopsOnHTTPDenial(t *testing.T) {
	ss, err := rpc.NewState(t.Context(), rpc.WithSkipVerify,
		rpc.WithAuthenticator(&rpc.NoOpAuthenticator{}), rpc.WithAuthorizer(lifecycleDenyAuthorizer{}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ss.Close() })
	// Authorization must reject Get before the unimplemented handler is reached.
	ss.Server().ExposeValue(serverLifecycleService, server_v1alpha.AdaptServerLifecycle(nil))

	deadline, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var output bytes.Buffer
	ctx := &Context{Context: deadline, Log: slog.Default(), Stdout: &output}
	ctx.Config.ServerAddress = ss.LoopbackAddr()
	op := &serverlifecycle.Operation{ID: "denied-op", Phase: serverlifecycle.PhaseRestarting}
	latest, err := followRemoteOperation(ctx, op)
	require.Equal(t, op, latest)
	require.Equal(t, cond.ErrRemote{Category: "permission", Code: "forbidden", Message: "lifecycle access denied"}, err)
	require.NotContains(t, output.String(), "server unreachable")
	require.NoError(t, deadline.Err(), "denial must stop following before the polling deadline")
}
