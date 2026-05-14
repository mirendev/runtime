package coordinate

import (
	"log/slog"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"miren.dev/runtime/pkg/cloudauth"
)

func TestApiAddresses(t *testing.T) {
	const listenAddr = "0.0.0.0:8443"

	localhost := []string{"0.0.0.0:8443", "127.0.0.1:8443", "[::1]:8443"}

	publicIPv4 := net.ParseIP("203.0.113.10")
	publicIPv6 := net.ParseIP("2001:db8::10")
	privateIP := net.ParseIP("10.0.0.5")

	tests := []struct {
		name           string
		additionalIPs  []net.IP
		discoveredIPs  []net.IP
		netcheckResult *cloudauth.NetcheckDualStackResult
		wantContains   []string
		wantExcludes   []string
	}{
		{
			// Public discovered IPs are never advertised: an interface IP or
			// startup-netcheck WAN echo doesn't prove port 8443 is reachable
			// from outside. Only the coordinator's port-probing netcheck can.
			name:          "no netcheck: public DiscoveredIPs are not advertised, private flows through",
			discoveredIPs: []net.IP{publicIPv4, privateIP},
			wantContains:  append(localhost, "10.0.0.5:8443"),
			wantExcludes:  []string{"203.0.113.10:8443"},
		},
		{
			// MIR-1139 repro: cluster is firewalled (port 8443 closed from outside).
			// Netcheck ran, got the WAN source, but every probe failed. Today's bug:
			// apiAddresses() falls back to DiscoveredIPs and advertises the unreachable
			// WAN IP. After fix: public DiscoveredIPs stay suppressed.
			name:          "netcheck ran, zero reachable: public DiscoveredIP not advertised (MIR-1139)",
			discoveredIPs: []net.IP{publicIPv4, privateIP},
			netcheckResult: &cloudauth.NetcheckDualStackResult{
				IPv4: &cloudauth.NetcheckResponse{
					SourceAddress: "203.0.113.10",
					Results: []cloudauth.NetcheckResult{
						{Port: 8443, Protocol: "tcp", Reachable: false},
					},
				},
			},
			wantContains: append(localhost, "10.0.0.5:8443"),
			wantExcludes: []string{"203.0.113.10:8443"},
		},
		{
			name:          "netcheck ran and found reachable addresses",
			discoveredIPs: []net.IP{publicIPv4, privateIP},
			netcheckResult: &cloudauth.NetcheckDualStackResult{
				IPv4: &cloudauth.NetcheckResponse{
					SourceAddress: "203.0.113.10",
					Results: []cloudauth.NetcheckResult{
						{Port: 8443, Protocol: "tcp", Reachable: true},
					},
				},
			},
			wantContains: append(localhost, "10.0.0.5:8443", "203.0.113.10:8443"),
		},
		{
			name:         "no IPs and no netcheck",
			wantContains: localhost,
		},
		{
			name:          "netcheck replaces discovered public IP with different source",
			discoveredIPs: []net.IP{publicIPv4, privateIP},
			netcheckResult: &cloudauth.NetcheckDualStackResult{
				IPv4: &cloudauth.NetcheckResponse{
					SourceAddress: "198.51.100.1",
					Results: []cloudauth.NetcheckResult{
						{Port: 8443, Protocol: "tcp", Reachable: true},
					},
				},
			},
			wantContains: append(localhost, "198.51.100.1:8443", "10.0.0.5:8443"),
			wantExcludes: []string{"203.0.113.10:8443"},
		},
		{
			name:          "dual-stack netcheck with both families reachable",
			discoveredIPs: []net.IP{publicIPv4, privateIP},
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
			wantContains: append(localhost, "203.0.113.10:8443", "[2001:db8::1]:8443", "10.0.0.5:8443"),
		},
		{
			name:          "dual-stack netcheck with only IPv4 reachable",
			discoveredIPs: []net.IP{publicIPv4, privateIP},
			netcheckResult: &cloudauth.NetcheckDualStackResult{
				IPv4: &cloudauth.NetcheckResponse{
					SourceAddress: "203.0.113.10",
					Results: []cloudauth.NetcheckResult{
						{Port: 8443, Protocol: "https", Reachable: true},
					},
				},
				IPv6: nil,
			},
			wantContains: append(localhost, "203.0.113.10:8443", "10.0.0.5:8443"),
		},
		{
			name:          "user-provided AdditionalIPs always included even with netcheck",
			additionalIPs: []net.IP{publicIPv4},
			discoveredIPs: []net.IP{privateIP},
			netcheckResult: &cloudauth.NetcheckDualStackResult{
				IPv4: &cloudauth.NetcheckResponse{
					SourceAddress: "198.51.100.1",
					Results: []cloudauth.NetcheckResult{
						{Port: 8443, Protocol: "tcp", Reachable: true},
					},
				},
			},
			// User-provided public IP is always kept, netcheck address is also added
			wantContains: append(localhost, "203.0.113.10:8443", "198.51.100.1:8443", "10.0.0.5:8443"),
		},
		{
			name:          "mixed-family: IPv4 reachable, discovered IPv6 preserved",
			discoveredIPs: []net.IP{publicIPv4, publicIPv6, privateIP},
			netcheckResult: &cloudauth.NetcheckDualStackResult{
				IPv4: &cloudauth.NetcheckResponse{
					SourceAddress: "203.0.113.10",
					Results: []cloudauth.NetcheckResult{
						{Port: 8443, Protocol: "https", Reachable: true},
					},
				},
				IPv6: nil,
			},
			// IPv4 is replaced by netcheck, but discovered IPv6 is still public
			// and gets dropped because netcheck had reachable results. This is
			// acceptable: the IPv6 wasn't verified reachable, so we don't report it.
			wantContains: append(localhost, "203.0.113.10:8443", "10.0.0.5:8443"),
			wantExcludes: []string{"[2001:db8::10]:8443"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Coordinator{
				CoordinatorConfig: CoordinatorConfig{
					Address:       listenAddr,
					AdditionalIPs: tt.additionalIPs,
					DiscoveredIPs: tt.discoveredIPs,
				},
				Log:            slog.Default(),
				netcheckResult: tt.netcheckResult,
			}

			got := c.apiAddresses()

			for _, want := range tt.wantContains {
				assert.Contains(t, got, want, "expected %q in result", want)
			}
			for _, excluded := range tt.wantExcludes {
				assert.NotContains(t, got, excluded, "expected %q to be excluded", excluded)
			}
		})
	}
}
