package rpc

import (
	"net"
	"time"
)

// Connection budgets, split by how far away the target is.
//
// quic-go's HandshakeIdleTimeout default is 5s, which is a sensible number for
// the open internet and an absurd one for loopback, where a round trip takes
// microseconds. Since the whole budget is what a user stares at when something
// is wrong, a local cluster that isn't running should fail fast, while a remote
// one keeps enough tolerance that a slow or lossy link is never mistaken for an
// unreachable server.
//
// The lookup budget covers dialing as well as the request, so each one has to
// clear its own handshake timeout and still leave room for a round trip against
// what is, on the server, an in-memory map lookup.
const (
	localHandshakeTimeout  = 2 * time.Second
	remoteHandshakeTimeout = 5 * time.Second

	localLookupTimeout  = 4 * time.Second
	remoteLookupTimeout = 8 * time.Second
)

// handshakeTimeoutFor returns how long to wait for a QUIC handshake with addr.
func handshakeTimeoutFor(addr string) time.Duration {
	if isLoopbackTarget(addr) {
		return localHandshakeTimeout
	}
	return remoteHandshakeTimeout
}

// lookupTimeoutFor returns the total budget for resolving a capability at addr.
//
// It is deliberately shorter than DefaultQUICConfig.MaxIdleTimeout, so we give
// up before the transport does. That means a lookup against a server that froze
// mid-request reports "never answered" rather than "went silent": at this point
// we genuinely cannot tell those apart, and waiting 30s to find out is a far
// worse deal for the user than a slightly hedged message.
func lookupTimeoutFor(addr string) time.Duration {
	if isLoopbackTarget(addr) {
		return localLookupTimeout
	}
	return remoteLookupTimeout
}

// isLoopbackTarget reports whether addr refers to this machine. A hostname we
// can't resolve locally is treated as remote, which is the safe direction to be
// wrong in: the cost is waiting longer, not failing a working connection.
func isLoopbackTarget(addr string) bool {
	if addr == "" {
		return false
	}

	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}

	// "localhost" is the common spelling and doesn't parse as an IP.
	if host == "localhost" {
		return true
	}

	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
