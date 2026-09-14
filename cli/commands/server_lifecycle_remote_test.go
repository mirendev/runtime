package commands

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/cond"
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
	require.False(t, answered(errors.New("dial tcp: connection refused")))
}
