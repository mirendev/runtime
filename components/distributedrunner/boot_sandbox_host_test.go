//go:build linux

package distributedrunner

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/pkg/boot"
)

func TestNetworkDepsReadyBeforeNodeStorage(t *testing.T) {
	access := &runner.ClusterAccess{}
	boot := newNetworkDepsBoot(sandboxHostBootInputs{
		log: testLogger(), coordinator: "198.51.100.9:8443",
	}, boot.ResolvedOutput(clusterAccessBootOutput{access: access}))
	deps, err := boot.start(t.Context(), clusterAccessBootOutput{access: access})
	require.NoError(t, err)
	require.NotNil(t, deps.Resolver)
	addr, err := deps.Resolver.LookupHost("cluster.local")
	require.NoError(t, err)
	require.Equal(t, netip.MustParseAddr("198.51.100.9"), addr)
}

func TestRegistryResolvesOverWireGuardNotPublicCoordinator(t *testing.T) {
	inputs := sandboxHostBootInputs{
		log: testLogger(), coordinator: "198.51.100.9:8443",
	}
	var deps runner.RunnerDeps
	require.NoError(t, inputs.prepareNetworkDeps(&deps, netip.MustParseAddr("10.8.42.1")))
	addr, err := deps.Resolver.LookupHost("cluster.local")
	require.NoError(t, err)
	require.Equal(t, netip.MustParseAddr("10.8.42.1"), addr)
	require.Equal(t, "198.51.100.9:8443", deps.ApiAddress)
}

func TestLegacyCoordinatorRegistryUsesAPIAddress(t *testing.T) {
	inputs := sandboxHostBootInputs{
		log: testLogger(), coordinator: "198.51.100.9:8443",
	}
	var deps runner.RunnerDeps
	require.NoError(t, inputs.prepareNetworkDeps(&deps, netip.Addr{}))
	addr, err := deps.Resolver.LookupHost("cluster.local")
	require.NoError(t, err)
	require.Equal(t, netip.MustParseAddr("198.51.100.9"), addr)
	require.Equal(t, "198.51.100.9:8443", deps.ApiAddress)
}
