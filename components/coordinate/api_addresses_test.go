package coordinate

import (
	"log/slog"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"miren.dev/runtime/pkg/cloudauth"
)

func explicit(ip string) SourcedIP   { return SourcedIP{IP: net.ParseIP(ip), Explicit: true} }
func discovered(ip string) SourcedIP { return SourcedIP{IP: net.ParseIP(ip), Explicit: false} }

// onIface is discovered() with the interface the address was found on, which
// is what bridge filtering keys off.
func onIface(ip, iface string) SourcedIP {
	return SourcedIP{IP: net.ParseIP(ip), Interface: iface}
}

func makeIPSet(entries ...SourcedIP) *IPSet {
	s := NewIPSet()
	for _, e := range entries {
		s.Add(e)
	}
	return s
}

func TestApiAddresses(t *testing.T) {
	const wildcardListen = "0.0.0.0:8443"

	// Addresses that must never appear in the advertised list for any
	// case, since a remote client reached via miren.cloud can't use them.
	nonRoutable := []string{
		"0.0.0.0:8443",
		"[::]:8443",
		"127.0.0.1:8443",
		"[::1]:8443",
	}

	tests := []struct {
		name           string
		listenAddr     string
		ips            *IPSet
		netcheckResult *cloudauth.NetcheckDualStackResult
		wantContains   []string
		wantExcludes   []string
	}{
		{
			name:         "no netcheck with discovered public IPs",
			ips:          makeIPSet(discovered("203.0.113.10"), discovered("10.0.0.5")),
			wantContains: []string{"203.0.113.10:8443", "10.0.0.5:8443"},
		},
		{
			name: "netcheck proved IPv4 unreachable drops discovered public IP",
			ips:  makeIPSet(discovered("203.0.113.10"), discovered("10.0.0.5")),
			netcheckResult: &cloudauth.NetcheckDualStackResult{
				IPv4: &cloudauth.NetcheckResponse{
					SourceAddress: "203.0.113.10",
					Results: []cloudauth.NetcheckResult{
						{Port: 8443, Protocol: "tcp", Reachable: false},
					},
				},
			},
			wantContains: []string{"10.0.0.5:8443"},
			wantExcludes: []string{"203.0.113.10:8443"},
		},
		{
			// Explicit IPs are never pruned by netcheck — the user asked
			// for them specifically, even if netcheck says unreachable.
			name: "explicit IP survives negative netcheck",
			ips:  makeIPSet(explicit("203.0.113.10"), discovered("10.0.0.5")),
			netcheckResult: &cloudauth.NetcheckDualStackResult{
				IPv4: &cloudauth.NetcheckResponse{
					SourceAddress: "203.0.113.10",
					Results: []cloudauth.NetcheckResult{
						{Port: 8443, Protocol: "tcp", Reachable: false},
					},
				},
			},
			wantContains: []string{"203.0.113.10:8443", "10.0.0.5:8443"},
		},
		{
			name:           "netcheck not run keeps discovered public IPs",
			ips:            makeIPSet(discovered("203.0.113.10"), discovered("10.0.0.5")),
			netcheckResult: nil,
			wantContains:   []string{"203.0.113.10:8443", "10.0.0.5:8443"},
		},
		{
			// MIR-1509: CGNAT used to be dropped here, which left a
			// tailnet-only cluster with nothing usable to advertise. The
			// internet can't route to it, so it's kept for clients that
			// share the overlay.
			name:         "CGNAT discovered IP is kept for overlay clients",
			ips:          makeIPSet(discovered("100.107.209.9"), discovered("10.0.0.5")),
			wantContains: []string{"100.107.209.9:8443", "10.0.0.5:8443"},
		},
		{
			// The IPv6 half of a tailnet has to get the same answer as the
			// IPv4 half; disagreeing is what made this fail in the field.
			name:         "tailscale ULA and CGNAT are treated alike",
			ips:          makeIPSet(discovered("100.107.209.9"), discovered("fd7a:115c:a1e0::2801:9641")),
			wantContains: []string{"100.107.209.9:8443", "[fd7a:115c:a1e0::2801:9641]:8443"},
		},
		{
			// A negative netcheck says nothing about overlay reachability,
			// so it must not prune the tailnet address with the public one.
			name: "netcheck failure does not drop CGNAT",
			ips:  makeIPSet(discovered("203.0.113.10"), discovered("100.107.209.9")),
			netcheckResult: &cloudauth.NetcheckDualStackResult{
				IPv4: &cloudauth.NetcheckResponse{
					SourceAddress: "203.0.113.10",
					Results: []cloudauth.NetcheckResult{
						{Port: 8443, Protocol: "tcp", Reachable: false},
					},
				},
			},
			wantContains: []string{"100.107.209.9:8443"},
			wantExcludes: []string{"203.0.113.10:8443"},
		},
		{
			// Explicit CGNAT is kept — the user asked for it.
			name:         "CGNAT explicit IP is kept",
			ips:          makeIPSet(explicit("100.107.209.9")),
			wantContains: []string{"100.107.209.9:8443"},
		},
		{
			// Container bridges only ever route to workloads on this host,
			// so they're noise in every cluster's advertised list.
			name: "container bridge addresses are dropped",
			ips: makeIPSet(
				onIface("172.17.0.1", "docker0"),
				onIface("10.8.45.1", "rt0"),
				onIface("10.8.45.0", "flannel.1"),
				onIface("10.50.1.170", "eth0"),
			),
			wantContains: []string{"10.50.1.170:8443"},
			wantExcludes: []string{"172.17.0.1:8443", "10.8.45.1:8443", "10.8.45.0:8443"},
		},
		{
			// An explicit IP is user intent and outranks bridge filtering.
			name:         "explicit IP on a bridge interface is still kept",
			ips:          makeIPSet(SourcedIP{IP: net.ParseIP("172.17.0.1"), Explicit: true, Interface: "docker0"}),
			wantContains: []string{"172.17.0.1:8443"},
		},
		{
			// Ani's cluster: the only reachable address is the tailnet one,
			// and everything else on the box is a bridge or unroutable.
			name: "tailnet-only host advertises its tailnet addresses",
			ips: makeIPSet(
				onIface("100.107.209.9", "tailscale0"),
				onIface("fd7a:115c:a1e0::2801:9641", "tailscale0"),
				onIface("172.17.0.1", "docker0"),
				onIface("172.18.0.1", "br-9f2a"),
			),
			wantContains: []string{"100.107.209.9:8443", "[fd7a:115c:a1e0::2801:9641]:8443"},
			wantExcludes: []string{"172.17.0.1:8443", "172.18.0.1:8443"},
		},
		{
			name: "netcheck ran and found reachable addresses",
			ips:  makeIPSet(discovered("203.0.113.10"), discovered("10.0.0.5")),
			netcheckResult: &cloudauth.NetcheckDualStackResult{
				IPv4: &cloudauth.NetcheckResponse{
					SourceAddress: "203.0.113.10",
					Results: []cloudauth.NetcheckResult{
						{Port: 8443, Protocol: "tcp", Reachable: true},
					},
				},
			},
			wantContains: []string{"10.0.0.5:8443", "203.0.113.10:8443"},
		},
		{
			name:         "no IPs and no netcheck yields empty list",
			wantContains: nil,
			wantExcludes: nonRoutable,
		},
		{
			name: "netcheck replaces discovered public IP with different source",
			ips:  makeIPSet(discovered("203.0.113.10"), discovered("10.0.0.5")),
			netcheckResult: &cloudauth.NetcheckDualStackResult{
				IPv4: &cloudauth.NetcheckResponse{
					SourceAddress: "198.51.100.1",
					Results: []cloudauth.NetcheckResult{
						{Port: 8443, Protocol: "tcp", Reachable: true},
					},
				},
			},
			wantContains: []string{"198.51.100.1:8443", "10.0.0.5:8443"},
			wantExcludes: []string{"203.0.113.10:8443"},
		},
		{
			name: "dual-stack netcheck with both families reachable",
			ips:  makeIPSet(discovered("203.0.113.10"), discovered("10.0.0.5")),
			netcheckResult: &cloudauth.NetcheckDualStackResult{
				IPv4: &cloudauth.NetcheckResponse{
					SourceAddress: "203.0.113.10",
					Results: []cloudauth.NetcheckResult{
						{Port: 8443, Protocol: "https", Reachable: true},
					},
				},
				IPv6: &cloudauth.NetcheckResponse{
					SourceAddress: "2001:db8::1",
					Results: []cloudauth.NetcheckResult{
						{Port: 8443, Protocol: "https", Reachable: true},
					},
				},
			},
			wantContains: []string{"203.0.113.10:8443", "[2001:db8::1]:8443", "10.0.0.5:8443"},
		},
		{
			name: "dual-stack netcheck with only IPv4 reachable",
			ips:  makeIPSet(discovered("203.0.113.10"), discovered("10.0.0.5")),
			netcheckResult: &cloudauth.NetcheckDualStackResult{
				IPv4: &cloudauth.NetcheckResponse{
					SourceAddress: "203.0.113.10",
					Results: []cloudauth.NetcheckResult{
						{Port: 8443, Protocol: "https", Reachable: true},
					},
				},
				IPv6: nil,
			},
			wantContains: []string{"203.0.113.10:8443", "10.0.0.5:8443"},
		},
		{
			name: "explicit IPs always included even with netcheck",
			ips:  makeIPSet(explicit("203.0.113.10"), discovered("10.0.0.5")),
			netcheckResult: &cloudauth.NetcheckDualStackResult{
				IPv4: &cloudauth.NetcheckResponse{
					SourceAddress: "198.51.100.1",
					Results: []cloudauth.NetcheckResult{
						{Port: 8443, Protocol: "tcp", Reachable: true},
					},
				},
			},
			wantContains: []string{"203.0.113.10:8443", "198.51.100.1:8443", "10.0.0.5:8443"},
		},
		{
			name: "mixed-family: IPv4 reachable, discovered IPv6 preserved",
			ips:  makeIPSet(discovered("203.0.113.10"), discovered("2001:db8::10"), discovered("10.0.0.5")),
			netcheckResult: &cloudauth.NetcheckDualStackResult{
				IPv4: &cloudauth.NetcheckResponse{
					SourceAddress: "203.0.113.10",
					Results: []cloudauth.NetcheckResult{
						{Port: 8443, Protocol: "https", Reachable: true},
					},
				},
				IPv6: nil,
			},
			wantContains: []string{"203.0.113.10:8443", "[2001:db8::10]:8443", "10.0.0.5:8443"},
		},
		{
			name:         "explicit non-wildcard listen address is advertised",
			listenAddr:   "198.51.100.7:8443",
			wantContains: []string{"198.51.100.7:8443"},
		},
		{
			name:         "loopback explicit IP is dropped",
			ips:          makeIPSet(explicit("127.0.0.1"), explicit("::1"), explicit("0.0.0.0"), explicit("203.0.113.10")),
			wantContains: []string{"203.0.113.10:8443"},
			wantExcludes: nonRoutable,
		},
		{
			// IPSet dedup: when an IP is discovered first and then added
			// again as explicit, it promotes to explicit and bypasses
			// netcheck filtering.
			name: "discovered IP promoted to explicit by IPSet dedup",
			ips: func() *IPSet {
				s := NewIPSet()
				s.AddDiscovered(net.ParseIP("203.0.113.10"))
				s.AddExplicit(net.ParseIP("203.0.113.10"))
				s.AddDiscovered(net.ParseIP("10.0.0.5"))
				return s
			}(),
			netcheckResult: &cloudauth.NetcheckDualStackResult{
				IPv4: &cloudauth.NetcheckResponse{
					SourceAddress: "203.0.113.10",
					Results: []cloudauth.NetcheckResult{
						{Port: 8443, Protocol: "tcp", Reachable: false},
					},
				},
			},
			// 203.0.113.10 was promoted from discovered → explicit, so
			// it survives the negative netcheck.
			wantContains: []string{"203.0.113.10:8443", "10.0.0.5:8443"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listen := tt.listenAddr
			if listen == "" {
				listen = wildcardListen
			}
			c := NewCloudControl(&Foundation{
				CoordinatorConfig: CoordinatorConfig{
					Address: listen,
					IPs:     tt.ips,
				},
				Log:            slog.Default(),
				netcheckResult: tt.netcheckResult,
			})

			got := c.apiAddresses()

			for _, nr := range nonRoutable {
				assert.NotContains(t, got, nr, "non-routable address %q must never be advertised", nr)
			}
			for _, want := range tt.wantContains {
				assert.Contains(t, got, want, "expected %q in result", want)
			}
			for _, excluded := range tt.wantExcludes {
				assert.NotContains(t, got, excluded, "expected %q to be excluded", excluded)
			}
		})
	}
}
