package coordinate

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsPubliclyRoutable(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"203.0.113.10", true},
		{"2001:db8::10", true},

		// CGNAT (RFC 6598) is global-unicast and not private as far as Go
		// is concerned, so it needs naming explicitly. The /10 boundaries
		// are worth pinning: the mask is easy to write as a /8 by mistake.
		{"100.64.0.0", false},
		{"100.107.209.9", false},
		{"100.127.255.255", false},
		{"100.63.255.255", true},
		{"100.128.0.0", true},

		{"fd7a:115c:a1e0::2801:9641", false}, // tailscale ULA
		{"fd00::1", false},                   // any other ULA
		{"10.0.0.5", false},
		{"172.17.0.1", false},
		{"192.168.1.1", false},
		{"127.0.0.1", false},
		{"::1", false},
		{"0.0.0.0", false},
		{"169.254.1.1", false},
		{"fe80::1", false},
		{"224.0.0.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			assert.Equal(t, tt.want, isPubliclyRoutable(net.ParseIP(tt.addr)))
		})
	}
}

func TestIsContainerBridge(t *testing.T) {
	for _, iface := range []string{
		"rt0", "flannel.1", "flannel-wg", "docker0", "docker1",
		"br-9f2a1c", "cni0", "cni-podman0", "cbr0", "virbr0", "podman0",
	} {
		assert.True(t, isContainerBridge(iface), "expected %q to be a container bridge", iface)
	}

	for _, iface := range []string{"eth0", "eno1", "wlan0", "tailscale0", "ens18", "lo"} {
		assert.False(t, isContainerBridge(iface), "expected %q not to be a container bridge", iface)
	}

	// An unknown interface must never be filtered: dropping the only
	// address that works is far worse than advertising a dead one.
	assert.False(t, isContainerBridge(""))
}

func TestIPSetKeepsInterfaceOnPromotion(t *testing.T) {
	// An explicit IP carries no interface of its own. When it duplicates a
	// discovered entry the name has to survive, or bridge filtering would
	// stop being able to see it.
	s := NewIPSet()
	s.AddDiscoveredFrom(net.ParseIP("10.8.45.1"), "rt0")
	s.AddExplicit(net.ParseIP("10.8.45.1"))

	entries := s.All()
	assert.Len(t, entries, 1)
	assert.True(t, entries[0].Explicit)
	assert.Equal(t, "rt0", entries[0].Interface)
}

func TestClassifyOnNamesOverlayAndBridge(t *testing.T) {
	// Display-only labels, but they're what an operator reads out of
	// `miren debug advertise` when two addresses got the same treatment.
	assert.Equal(t, "tailnet", classifyOn(net.ParseIP("100.107.209.9"), "tailscale0"))
	assert.Equal(t, "tailnet", classifyOn(net.ParseIP("fd7a:115c:a1e0::1"), ""))
	assert.Equal(t, "container-bridge", classifyOn(net.ParseIP("172.17.0.1"), "docker0"))
	assert.Equal(t, "private", classifyOn(net.ParseIP("10.50.1.170"), "eth0"))
	assert.Equal(t, "global-unicast", classifyOn(net.ParseIP("203.0.113.10"), "eth0"))
}
