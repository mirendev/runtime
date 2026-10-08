package coordinate

import (
	"encoding/json"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/clusternetwork"
	"miren.dev/runtime/pkg/ipdiscovery"
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

		// IANA special-purpose space Go's predicates call global unicast.
		{"0.1.2.3", false},
		{"192.0.0.9", false},
		{"198.18.0.1", false},
		{"198.19.255.255", false},
		{"240.0.0.1", false},
		{"255.255.255.255", false},
		{"64:ff9b::808:808", false},

		// An IPv4-mapped IPv6 address is judged as the IPv4 it carries.
		{"::ffff:10.0.0.5", false},
		{"::ffff:203.0.113.10", true},
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

func TestAddressRange(t *testing.T) {
	tests := []struct{ addr, want string }{
		{"203.0.113.10", clusternetwork.RangePublic},
		{"2001:db8::10", clusternetwork.RangePublic},
		{"10.0.0.5", clusternetwork.RangePrivate},
		{"192.168.1.1", clusternetwork.RangePrivate},
		{"100.107.209.9", clusternetwork.RangeShared},
		{"fd7a:115c:a1e0::1", clusternetwork.RangeULA},
		{"fd00::1", clusternetwork.RangeULA},
		{"198.18.0.1", clusternetwork.RangeSpecial},
		{"127.0.0.1", rangeLoopback},
		{"169.254.1.1", rangeLinkLocal},
		{"fe80::1", rangeLinkLocal},
		{"::", rangeUnspecified},
		{"ff02::1", rangeMulticast},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			assert.Equal(t, tt.want, addressRange(net.ParseIP(tt.addr)))
		})
	}
}

func TestClassOf(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		iface    string
		linkType string
		p2p      bool
		want     string
	}{
		{"public NIC", "203.0.113.10", "eth0", "device", false, clusternetwork.ClassPublic},
		{"home LAN", "192.168.1.20", "eth0", "device", false, clusternetwork.ClassLAN},
		{"ULA on a NIC is a LAN", "fd00::20", "eth0", "device", false, clusternetwork.ClassLAN},

		// The case Tailscale's own code gets wrong: CGNAT space on an
		// ordinary NIC is the ISP's, not an overlay.
		{"ISP CGNAT on a NIC", "100.70.1.2", "eth0", "device", false, clusternetwork.ClassLAN},

		{"tailnet v4 on tun", "100.107.209.9", "tailscale0", "tuntap", true, clusternetwork.ClassOverlay},
		{"tailnet v6 on tun", "fd7a:115c:a1e0::1", "tailscale0", "tuntap", true, clusternetwork.ClassOverlay},
		{"wireguard mesh", "10.99.0.2", "wg0", "wireguard", true, clusternetwork.ClassOverlay},
		{"zerotier is a tap", "10.147.17.5", "ztabcdef12", "tuntap", false, clusternetwork.ClassOverlay},
		{"tailscale0 without netlink", "100.107.209.9", "tailscale0", "", false, clusternetwork.ClassOverlay},

		// Point-to-point only counts when the link type is unknown: PPPoE
		// sets it too, and ISP CGNAT over ppp0 is not an overlay.
		{"ISP CGNAT over PPPoE", "100.70.1.2", "ppp0", "ppp", true, clusternetwork.ClassLAN},
		{"p2p tunnel without netlink", "10.99.0.2", "tun9", "", true, clusternetwork.ClassOverlay},

		// A bridge holding the host's own address (Proxmox vmbr0) is the
		// LAN; only the container bridges we know by name are local.
		{"host LAN on a bridge", "192.168.1.20", "vmbr0", "bridge", false, clusternetwork.ClassLAN},
		{"docker bridge", "172.17.0.1", "docker0", "bridge", false, clusternetwork.ClassContainerBridge},
		{"miren bridge", "10.8.45.1", "rt0", "bridge", false, clusternetwork.ClassContainerBridge},

		// No link facts at all (configured or observed addresses): the
		// range decides, and non-routable space defaults to the LAN.
		{"explicit public", "203.0.113.10", "", "", false, clusternetwork.ClassPublic},
		{"explicit CGNAT", "100.107.209.9", "", "", false, clusternetwork.ClassLAN},

		{"loopback", "127.0.0.1", "lo", "device", false, ""},
		{"link-local", "169.254.1.1", "eth0", "device", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, classOf(net.ParseIP(tt.addr), tt.iface, tt.linkType, tt.p2p))
		})
	}
}

func TestLabelOf(t *testing.T) {
	assert.Equal(t, "tailscale", labelOf(net.ParseIP("100.107.209.9"), "tailscale0"))
	assert.Equal(t, "tailscale", labelOf(net.ParseIP("fd7a:115c:a1e0::1"), ""))
	assert.Equal(t, "zerotier", labelOf(net.ParseIP("10.147.17.5"), "ztabcdef12"))
	assert.Equal(t, "netbird", labelOf(net.ParseIP("100.90.1.1"), "wt0"))
	assert.Equal(t, "wireguard", labelOf(net.ParseIP("10.99.0.2"), "wg0"))

	// CGNAT space alone is not evidence of Tailscale.
	assert.Equal(t, "", labelOf(net.ParseIP("100.70.1.2"), "eth0"))
	assert.Equal(t, "", labelOf(net.ParseIP("100.70.1.2"), ""))
}

func TestAddDiscoveredAddress(t *testing.T) {
	s := NewIPSet()
	assert.True(t, s.AddDiscoveredAddress(ipdiscovery.Address{
		Interface: "tailscale0", IP: "100.107.209.9", LinkType: "tuntap", PointToPoint: true,
	}))
	// netcheck's placeholder interface must not reach the wire as a link.
	assert.True(t, s.AddDiscoveredAddress(ipdiscovery.Address{
		Interface: ipdiscovery.NetcheckInterface, IP: "203.0.113.10",
	}))
	assert.False(t, s.AddDiscoveredAddress(ipdiscovery.Address{Interface: "eth0", IP: "nonsense"}))

	entries := s.All()
	assert.Len(t, entries, 2)
	assert.Equal(t, SourcedIP{
		IP: net.ParseIP("100.107.209.9"), Interface: "tailscale0", LinkType: "tuntap", PointToPoint: true,
	}, entries[0])
	assert.Equal(t, "", entries[1].Interface)
}

func TestIPSetKeepsLinkFactsOnPromotion(t *testing.T) {
	s := NewIPSet()
	s.AddExplicit(net.ParseIP("100.107.209.9"))
	s.Add(SourcedIP{IP: net.ParseIP("100.107.209.9"), Interface: "tailscale0", LinkType: "tuntap", PointToPoint: true})

	e := s.All()[0]
	assert.True(t, e.Explicit)
	assert.Equal(t, "tailscale0", e.Interface)
	assert.Equal(t, "tuntap", e.LinkType)
	assert.True(t, e.PointToPoint)
}

func TestComputeAdvertiseDetails(t *testing.T) {
	cands, advertised := ComputeAdvertise(AdvertiseInput{
		ListenAddr: "0.0.0.0:8443",
		IPs: []SourcedIP{
			{IP: net.ParseIP("192.168.1.20"), Interface: "eth0", LinkType: "device"},
			{IP: net.ParseIP("100.107.209.9"), Interface: "tailscale0", LinkType: "tuntap", PointToPoint: true},
			{IP: net.ParseIP("fd7a:115c:a1e0::1"), Interface: "tailscale0", LinkType: "tuntap", PointToPoint: true},
			{IP: net.ParseIP("172.17.0.1"), Interface: "docker0", LinkType: "bridge"},
			{IP: net.ParseIP("100.70.1.2"), Explicit: true},
		},
		Netcheck: &cloudauth.NetcheckDualStackResult{
			IPv4: &cloudauth.NetcheckResponse{
				SourceAddress: "203.0.113.10",
				Results:       []cloudauth.NetcheckResult{{Port: 8443, Protocol: "https", Reachable: true}},
			},
		},
	})
	assert.Len(t, cands, 7, "every candidate, advertised or not, is reported")

	want := []clusternetwork.AdvertisedAddress{
		{Transport: "ip", Address: "192.168.1.20:8443", Class: "lan", Range: "private", Interface: "eth0", Source: "discovered"},
		{Transport: "ip", Address: "100.107.209.9:8443", Class: "overlay", Range: "shared", Interface: "tailscale0", Source: "discovered", Label: "tailscale"},
		{Transport: "ip", Address: "[fd7a:115c:a1e0::1]:8443", Class: "overlay", Range: "ula", Interface: "tailscale0", Source: "discovered", Label: "tailscale"},
		{Transport: "ip", Address: "100.70.1.2:8443", Class: "lan", Range: "shared", Source: "explicit"},
		{Transport: "ip", Address: "203.0.113.10:8443", Class: "public", Range: "public", Source: "netcheck"},
	}
	assert.Equal(t, want, WireDetails(advertised))

	// The flat list cloud already reads is the same addresses, same order.
	assert.Equal(t, []string{
		"192.168.1.20:8443", "100.107.209.9:8443", "[fd7a:115c:a1e0::1]:8443",
		"100.70.1.2:8443", "203.0.113.10:8443",
	}, HostPorts(advertised))
}

func TestNetcheckAddressBorrowsLinkFacts(t *testing.T) {
	reachable := func(src string) *cloudauth.NetcheckDualStackResult {
		return &cloudauth.NetcheckDualStackResult{IPv4: &cloudauth.NetcheckResponse{
			SourceAddress: src,
			Results:       []cloudauth.NetcheckResult{{Port: 8443, Protocol: "https", Reachable: true}},
		}}
	}

	// Public address directly on the NIC: netcheck replaces the
	// discovered entry, and the replacement still knows it's on eth0.
	_, advertised := ComputeAdvertise(AdvertiseInput{
		IPs:      []SourcedIP{{IP: net.ParseIP("203.0.113.10"), Interface: "eth0", LinkType: "device"}},
		Netcheck: reachable("203.0.113.10"),
	})
	assert.Equal(t, []clusternetwork.AdvertisedAddress{
		{Transport: "ip", Address: "203.0.113.10:8443", Class: "public", Range: "public", Interface: "eth0", Source: "netcheck"},
	}, WireDetails(advertised))

	// Behind NAT the observed address is on no local link.
	_, advertised = ComputeAdvertise(AdvertiseInput{
		IPs:      []SourcedIP{{IP: net.ParseIP("10.0.0.5"), Interface: "eth0", LinkType: "device"}},
		Netcheck: reachable("198.51.100.1"),
	})
	assert.Equal(t, []clusternetwork.AdvertisedAddress{
		{Transport: "ip", Address: "10.0.0.5:8443", Class: "lan", Range: "private", Interface: "eth0", Source: "discovered"},
		{Transport: "ip", Address: "198.51.100.1:8443", Class: "public", Range: "public", Source: "netcheck"},
	}, WireDetails(advertised))
}

func TestSpecialPurposeAddressesNeverAdvertised(t *testing.T) {
	// A VPN client's fake-IP range on its tun device. Before special
	// ranges were recognized this was pruned only when netcheck happened
	// to prove the family reachable; now it is dropped either way.
	fakeIP := SourcedIP{IP: net.ParseIP("198.18.0.1"), Interface: "Meta", LinkType: "tuntap", PointToPoint: true}
	for name, nc := range map[string]*cloudauth.NetcheckDualStackResult{
		"netcheck reachable": {IPv4: &cloudauth.NetcheckResponse{
			SourceAddress: "203.0.113.10",
			Results:       []cloudauth.NetcheckResult{{Port: 8443, Protocol: "https", Reachable: true}},
		}},
		"no netcheck": nil,
	} {
		t.Run(name, func(t *testing.T) {
			_, advertised := ComputeAdvertise(AdvertiseInput{IPs: []SourcedIP{fakeIP}, Netcheck: nc})
			assert.NotContains(t, HostPorts(advertised), "198.18.0.1:8443")
		})
	}

	// Configured by hand, it still passes through like any explicit IP.
	_, advertised := ComputeAdvertise(AdvertiseInput{IPs: []SourcedIP{{IP: net.ParseIP("198.18.0.1"), Explicit: true}}})
	assert.Equal(t, []string{"198.18.0.1:8443"}, HostPorts(advertised))
}

func TestZeroAddressReportSendsEmptyDetails(t *testing.T) {
	// Advertising nothing must still send the field, as [] and not null
	// or absent: cloud reads its presence as "this runtime knows about
	// details", and absence as a runtime that predates them.
	_, advertised := ComputeAdvertise(AdvertiseInput{ListenAddr: "0.0.0.0:8443"})
	assert.Empty(t, advertised)

	b, err := json.Marshal(clusternetwork.Report{
		APIAddresses:      HostPorts(advertised),
		APIAddressDetails: WireDetails(advertised),
	})
	assert.NoError(t, err)
	assert.Contains(t, string(b), `"api_address_details":[]`)
}

func TestListenAddressBorrowsLinkFacts(t *testing.T) {
	_, advertised := ComputeAdvertise(AdvertiseInput{
		ListenAddr: "100.107.209.9:8443",
		IPs: []SourcedIP{
			{IP: net.ParseIP("100.107.209.9"), Interface: "tailscale0", LinkType: "tuntap", PointToPoint: true},
		},
	})
	assert.Len(t, advertised, 1)
	assert.Equal(t, clusternetwork.SourceListen, advertised[0].Source)
	assert.Equal(t, clusternetwork.ClassOverlay, advertised[0].Class)
	assert.Equal(t, "tailscale0", advertised[0].Interface)
}
