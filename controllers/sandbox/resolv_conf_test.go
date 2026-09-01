package sandbox

import (
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/network"
)

func TestWriteResolve(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolv.conf")
	ep := &network.EndpointConfig{
		Bridge: &network.BridgeConfig{
			Addresses: []netip.Prefix{
				netip.MustParsePrefix("10.8.95.1/24"),
				netip.MustParsePrefix("fd00::1/64"),
			},
		},
	}

	err := (&SandboxController{}).writeResolve(path, ep)
	require.NoError(t, err)
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "search app.miren\n"+
		"options timeout:2 attempts:3\n"+
		"nameserver 10.8.95.1\n"+
		"nameserver fd00::1\n", string(contents))
}

func TestWriteResolveRequiresNameserver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolv.conf")
	ep := &network.EndpointConfig{Bridge: &network.BridgeConfig{}}

	err := (&SandboxController{}).writeResolve(path, ep)

	assert.EqualError(t, err, "no nameservers available in bridge config")
}
