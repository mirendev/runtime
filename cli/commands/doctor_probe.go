package commands

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"

	"miren.dev/runtime/pkg/rpc"
)

// probeOutcome is what a reachability probe learned. The distinction that
// matters is between "something actively said no" and "nothing said anything":
// a refusal proves the host is up and nothing is listening, while silence means
// either the host is gone or the traffic never arrived.
type probeOutcome int

const (
	probeSkipped probeOutcome = iota
	// probeOpen: something accepted the connection.
	probeOpen
	// probeRefused: the host answered with a refusal. Nothing is listening.
	probeRefused
	// probeSilent: no answer at all, within the timeout.
	probeSilent
	// probeFailed: the probe itself couldn't run (bad address, no socket).
	probeFailed
)

func (o probeOutcome) String() string {
	switch o {
	case probeOpen:
		return "open"
	case probeRefused:
		return "refused"
	case probeSilent:
		return "silent"
	case probeFailed:
		return "failed"
	case probeSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

type probe struct {
	Outcome probeOutcome
	Detail  string
}

const probeTimeout = 3 * time.Second

// probeTCP asks whether the host is reachable at all, over TCP.
//
// It deliberately targets the ingress port rather than the API port. The API is
// QUIC, which is UDP only, so nothing ever listens on TCP there and the probe
// would report a refusal even against a perfectly healthy cluster — useless as
// a discriminator. The ingress is the port most likely to have a TCP listener,
// which is what makes "the host answered on TCP" a meaningful fact.
//
// This is evidence, not a health check, and it is deliberately not displayed as
// a pass/fail row of its own. Its whole job is to disambiguate a failed QUIC
// connection: TCP answering while UDP stays silent means the host is up and
// something is eating the API's UDP traffic, a refusal means the host is up but
// serving nothing, and silence on both means we can't reach it at all.
func probeTCP(addr string) probe {
	addr = withIngressPort(addr)

	conn, err := net.DialTimeout("tcp", addr, probeTimeout)
	if err == nil {
		conn.Close()
		return probe{Outcome: probeOpen, Detail: "accepted"}
	}

	if errors.Is(err, syscall.ECONNREFUSED) {
		return probe{Outcome: probeRefused, Detail: "refused"}
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return probe{Outcome: probeSilent, Detail: "no response"}
	}

	return probe{Outcome: probeFailed, Detail: err.Error()}
}

// probeQUIC checks whether the cluster's API port answers a QUIC handshake.
// This is the transport every miren command actually uses.
func probeQUIC(addr string) probe {
	addr = withDefaultAPIPort(addr)

	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return probe{Outcome: probeFailed, Detail: fmt.Sprintf("resolve failed: %s", err)}
	}

	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv6zero, Port: 0})
	if err != nil {
		udpConn, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
		if err != nil {
			return probe{Outcome: probeFailed, Detail: fmt.Sprintf("local udp socket: %s", err)}
		}
	}
	defer udpConn.Close()

	transport := &quic.Transport{Conn: udpConn}
	defer transport.Close()

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	// Certificate verification is deliberately left on, even though clusters
	// commonly use self-signed certs and this probe has no roots to check them
	// against. The question here is "did anything answer", and a rejected
	// certificate answers it just as well as an accepted one: we only see a
	// certificate if the server sent one. Disabling the check would be a real
	// weakness in a tool people run to decide whether to trust a cluster, and
	// it buys nothing, because we never send or read anything over this
	// connection.
	conn, err := transport.Dial(ctx, udpAddr, &tls.Config{
		NextProtos: []string{http3.NextProtoH3},
		MinVersion: tls.VersionTLS13,
	}, &quic.Config{
		InitialPacketSize:    rpc.InitialPacketSize,
		HandshakeIdleTimeout: probeTimeout,
		MaxIdleTimeout:       probeTimeout,
	})
	if err != nil {
		// Typed errors rather than a substring match on the message. quic-go
		// reports both a never-started and a stalled handshake as timeouts, and
		// either way the observable fact is the same: nothing answered.
		var idle *quic.IdleTimeoutError
		var handshake *quic.HandshakeTimeoutError
		if errors.As(err, &idle) || errors.As(err, &handshake) || errors.Is(err, context.DeadlineExceeded) {
			return probe{Outcome: probeSilent, Detail: "no response"}
		}

		// Local socket failures surface through Dial too, and none of them
		// mean the peer said anything. A refusal is its own signal (the host
		// is up, nothing is bound); the rest are just a probe we couldn't run.
		if errors.Is(err, syscall.ECONNREFUSED) {
			return probe{Outcome: probeRefused, Detail: "refused"}
		}
		if errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) ||
			errors.Is(err, syscall.EPERM) || errors.Is(err, net.ErrClosed) {
			return probe{Outcome: probeFailed, Detail: err.Error()}
		}

		// What's left got far enough to fail on content rather than silence: a
		// rejected certificate, an ALPN mismatch, a version disagreement. All
		// of them require the peer to have said something.
		return probe{Outcome: probeOpen, Detail: "responded (certificate not verified)"}
	}

	_ = conn.CloseWithError(0, "doctor")
	return probe{Outcome: probeOpen, Detail: "handshake ok"}
}

// withDefaultAPIPort fills in the API port when the configured address omits it.
func withDefaultAPIPort(addr string) string {
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return net.JoinHostPort(addr, doctorAPIPort)
	}
	return addr
}

// withIngressPort replaces whatever port the cluster address carries with the
// ingress port, which is where a TCP listener actually lives.
func withIngressPort(addr string) string {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	return net.JoinHostPort(host, doctorIngressPort)
}

const (
	doctorAPIPort     = "8443"
	doctorIngressPort = "443"
)
