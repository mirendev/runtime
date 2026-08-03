package testutils

import (
	"net"
	"sync"
	"testing"
)

// handedOutPorts remembers every port GetFreePort has returned in this process,
// so a later call never returns the same number. GetFreePort releases the port
// before returning it (see below), so without this two calls in quick
// succession can hand back the same port — the cause of the etcd
// client/peer-port collision flake (MIR-720).
var (
	handedOutMu    sync.Mutex
	handedOutPorts = map[int]struct{}{}
)

// GetFreePort returns a port that is free for both TCP and UDP, and distinct
// from any port already handed out by GetFreePort in this process.
// This is important for QUIC-based servers (like the RPC server) which bind both protocols.
// The function binds both protocols to verify availability, then releases them.
// Fails the test if a suitable port cannot be obtained after several attempts.
func GetFreePort(t testing.TB) int {
	t.Helper()

	var duplicates int

	for range 20 {
		// First, get an available TCP port. Bind the wildcard address, not
		// loopback: callers such as the etcd component run with host
		// networking and bind 0.0.0.0, and a port already held on a
		// non-loopback address (an outbound connection's source port, say)
		// passes a 127.0.0.1 check but fails a 0.0.0.0 bind. Checking the
		// wildcard is strictly more conservative, so loopback-only callers
		// are still safe.
		tcpListener, err := net.Listen("tcp", "0.0.0.0:0")
		if err != nil {
			continue
		}
		port := tcpListener.Addr().(*net.TCPAddr).Port

		// Try to bind UDP to the same port
		udpAddr := &net.UDPAddr{IP: net.IPv4zero, Port: port}
		udpConn, err := net.ListenUDP("udp", udpAddr)
		if err != nil {
			tcpListener.Close()
			continue
		}

		// Reject a port we've already handed out. Because the listeners are
		// released before returning, the OS can re-offer a just-freed port to
		// the next caller; rebinding on the next iteration rotates to a fresh
		// ephemeral port.
		handedOutMu.Lock()
		_, seen := handedOutPorts[port]
		if !seen {
			handedOutPorts[port] = struct{}{}
		}
		handedOutMu.Unlock()
		if seen {
			duplicates++
			tcpListener.Close()
			udpConn.Close()
			continue
		}

		// Both succeeded and the port is new - close and return the port
		tcpListener.Close()
		udpConn.Close()
		return port
	}

	// Split out the two rejection reasons: a budget eaten by duplicates means
	// this process has exhausted the ephemeral range, which is a very
	// different problem from TCP/UDP contention.
	t.Fatalf("failed to find a free port after 20 attempts (%d rejected as already handed out to this process)", duplicates)
	return 0
}
