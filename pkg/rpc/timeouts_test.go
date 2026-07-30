package rpc

import (
	"testing"
	"time"
)

func TestIsLoopbackTarget(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"localhost:8443", true},
		{"localhost", true},
		{"127.0.0.1:8443", true},
		{"127.0.0.1", true},
		{"[::1]:8443", true},
		{"::1", true},
		{"cluster.example.com:8443", false},
		{"10.0.0.5:8443", false},
		{"192.168.1.10:8443", false},
		// A LAN address is not loopback: it's another machine, and deserves
		// the remote budget.
		{"miren.host:8443", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			if got := isLoopbackTarget(tt.addr); got != tt.want {
				t.Errorf("isLoopbackTarget(%q) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}

// The point of splitting the budgets is that a local cluster fails fast while a
// remote one keeps its tolerance for slow links.
func TestTimeoutBudgetsByLocality(t *testing.T) {
	if got := handshakeTimeoutFor("localhost:8443"); got != localHandshakeTimeout {
		t.Errorf("local handshake budget = %s, want %s", got, localHandshakeTimeout)
	}
	if got := handshakeTimeoutFor("cluster.example.com:8443"); got != remoteHandshakeTimeout {
		t.Errorf("remote handshake budget = %s, want %s", got, remoteHandshakeTimeout)
	}
	if got := lookupTimeoutFor("localhost:8443"); got != localLookupTimeout {
		t.Errorf("local lookup budget = %s, want %s", got, localLookupTimeout)
	}
	if got := lookupTimeoutFor("cluster.example.com:8443"); got != remoteLookupTimeout {
		t.Errorf("remote lookup budget = %s, want %s", got, remoteLookupTimeout)
	}
}

// Two invariants hold the design together. The lookup budget must exceed its
// own handshake budget, or an unreachable server would trip our deadline before
// the transport could report it and the classification would go
// non-deterministic. And it must stay under MaxIdleTimeout, or a wedged server
// costs the user the full idle timeout.
func TestTimeoutBudgetsAreOrdered(t *testing.T) {
	cases := []struct {
		name      string
		handshake time.Duration
		lookup    time.Duration
	}{
		{"local", localHandshakeTimeout, localLookupTimeout},
		{"remote", remoteHandshakeTimeout, remoteLookupTimeout},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.lookup <= tc.handshake {
				t.Errorf("lookup budget %s must exceed the handshake budget %s", tc.lookup, tc.handshake)
			}
			if tc.lookup >= DefaultQUICConfig.MaxIdleTimeout {
				t.Errorf("lookup budget %s must stay under MaxIdleTimeout %s", tc.lookup, DefaultQUICConfig.MaxIdleTimeout)
			}
		})
	}
}
