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
	"miren.dev/runtime/pkg/ui"
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

type lifecycleMissingOperation struct{ server_v1alpha.ServerLifecycle }

func (lifecycleMissingOperation) Get(_ context.Context, call *server_v1alpha.ServerLifecycleGet) error {
	return cond.NotFound("lifecycle operation", call.Args().Id())
}

type lifecycleRejectAuthenticator struct{}

func (lifecycleRejectAuthenticator) Authenticate(context.Context, *rpc.Credentials) (*rpc.Identity, error) {
	return nil, errors.New("credentials rejected")
}

func TestFollowRemoteOperationStopsOnServerAnswer(t *testing.T) {
	for _, failure := range []string{"call denial", "not found", "lookup denial"} {
		t.Run(failure, func(t *testing.T) {
			var authorizer rpc.Authorizer
			if failure == "call denial" {
				authorizer = lifecycleDenyAuthorizer{}
			}
			var authenticator rpc.Authenticator = &rpc.NoOpAuthenticator{}
			if failure == "lookup denial" {
				authenticator = lifecycleRejectAuthenticator{}
			}
			ss, err := rpc.NewState(t.Context(), rpc.WithSkipVerify,
				rpc.WithAuthenticator(authenticator), rpc.WithAuthorizer(authorizer))
			require.NoError(t, err)
			t.Cleanup(func() { _ = ss.Close() })
			ss.Server().ExposeValue(serverLifecycleService, server_v1alpha.AdaptServerLifecycle(lifecycleMissingOperation{}))

			deadline, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			var output bytes.Buffer
			ctx := &Context{Context: deadline, Log: slog.Default(), Stdout: &output}
			ctx.Config.ServerAddress = ss.LoopbackAddr()
			op := &serverlifecycle.Operation{ID: "pending-op", Phase: serverlifecycle.PhaseRestarting}
			latest, err := followRemoteOperation(ctx, op)
			require.Equal(t, op, latest)
			switch failure {
			case "call denial":
				var permission cond.ErrRemote
				require.ErrorAs(t, err, &permission)
				require.Equal(t, cond.ErrRemote{Category: "permission", Code: "forbidden", Message: "lifecycle access denied"}, permission)
			case "not found":
				require.ErrorIs(t, err, cond.ErrNotFound{})
			case "lookup denial":
				var resolve *rpc.ResolveError
				require.ErrorAs(t, err, &resolve)
				require.Equal(t, http.StatusUnauthorized, resolve.StatusCode)
			}
			var rendered bytes.Buffer
			if terminal, ok := errors.AsType[ui.SeverityTerminalError](err); ok {
				terminal.WriteWithSeverity(&rendered, ui.SeverityError)
			} else {
				fmt.Fprint(&rendered, err)
			}
			require.Contains(t, rendered.String(), "may still be running")
			require.Contains(t, rendered.String(), "miren server operations show pending-op")
			require.NotContains(t, output.String(), "server unreachable")
			require.NoError(t, deadline.Err(), "server answer must stop following before the polling deadline")
		})
	}
}
