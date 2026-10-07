//go:build linux

package network

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// The token server and the metrics push relay listen on the bridge router, so
// on a host whose INPUT policy drops by default, sandboxes can only reach them
// if the bridge rules open their port. This holds whether or not the API is on
// this host.
func TestBridgeInputPortsOpenTokenServer(t *testing.T) {
	for _, apiPort := range []int{0, 8443} {
		i := slices.IndexFunc(bridgeInputPorts(apiPort), func(p bridgeInputPort) bool {
			return p.port == TokenServerPort
		})
		require.GreaterOrEqual(t, i, 0, "token server port missing with apiPort=%d", apiPort)
		require.Equal(t, []string{"tcp"}, bridgeInputPorts(apiPort)[i].proto)
	}
}
