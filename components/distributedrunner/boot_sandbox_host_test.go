//go:build linux

package distributedrunner

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/components/runner"
)

func TestRegistryResolvesOverWireGuardNotPublicCoordinator(t *testing.T) {
	boot := &sandboxHostBoot{inputs: sandboxHostBootInputs{
		log: testLogger(), coordinator: "198.51.100.9:8443",
	}}
	var deps runner.RunnerDeps
	require.NoError(t, boot.prepareNetworkDeps(&deps, netip.MustParseAddr("10.8.42.1")))
	addr, err := deps.Resolver.LookupHost("cluster.local")
	require.NoError(t, err)
	require.Equal(t, netip.MustParseAddr("10.8.42.1"), addr)
	require.Equal(t, "198.51.100.9:8443", deps.ApiAddress)
}

func TestRegistryAddressRequiredBeforeRunnerStarts(t *testing.T) {
	boot := &sandboxHostBoot{inputs: sandboxHostBootInputs{log: testLogger()}}
	var deps runner.RunnerDeps
	require.ErrorContains(t, boot.prepareNetworkDeps(&deps, netip.Addr{}), "internal WireGuard address is unavailable")
}
