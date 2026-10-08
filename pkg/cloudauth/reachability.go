package cloudauth

// ReachabilityVerdict is a compact, agent-computed explanation of the
// cluster's inbound reachability, synthesized from the netcheck trace at
// report time. It exists so the dashboard can name the culprit ("found
// 159.195.16.123 but UDP 8443 (QUIC) unreachable") instead of showing a
// generic "Not reachable" with static guess-at-the-fix copy.
type ReachabilityVerdict struct {
	// Reachable is true when netcheck confirmed at least one reachable port
	// on a public source address. Deliberately no omitempty: a false value is
	// the interesting case and must travel on the wire.
	Reachable bool `json:"reachable"`
	// PublicAddress is the public source address netcheck observed (the IP the
	// cluster appears as from the internet), e.g. "159.195.16.123".
	PublicAddress string `json:"public_address,omitempty"`
	// UnreachablePorts lists the ports that failed the check, present only
	// when Reachable is false.
	UnreachablePorts []UnreachablePort `json:"unreachable_ports,omitempty"`
}

// UnreachablePort names a single port/protocol that netcheck could not reach,
// with its transport spelled out so the dashboard can say "UDP 8443 (QUIC)".
type UnreachablePort struct {
	Port      int    `json:"port"`
	Protocol  string `json:"protocol"`  // "http3", "https", "http"
	Transport string `json:"transport"` // "UDP" (QUIC/http3) or "TCP"
}
