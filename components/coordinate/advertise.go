package coordinate

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"miren.dev/runtime/pkg/cloudauth"
)

// SourcedIP is an IP address tagged with how it was obtained. Explicit IPs
// (user-configured via AdditionalIPs or the server config) always pass
// through to the advertised list. Discovered IPs (auto-scanned from local
// interfaces) are subject to netcheck pruning, bridge filtering, etc.
type SourcedIP struct {
	IP       net.IP
	Explicit bool // true = user-configured, false = auto-discovered

	// Interface is the name of the link the IP was discovered on, when
	// known. Empty for explicit IPs and for callers that don't track it.
	// Used to tell a host's real NICs apart from the container bridges
	// Miren and Docker create, which are never reachable from a client.
	Interface string
}

// IPSet is an ordered, de-duplicated collection of SourcedIP entries.
// When a duplicate IP is added, the Explicit flag is sticky: adding an
// IP as explicit promotes a previously-discovered entry, but adding it
// as discovered never demotes an explicit one. Iteration order matches
// first-insertion order.
type IPSet struct {
	entries []SourcedIP
	index   map[string]int // IP string → index into entries
}

// NewIPSet creates an empty IPSet.
func NewIPSet() *IPSet {
	return &IPSet{index: make(map[string]int)}
}

// Add inserts an IP. If the IP already exists and the new entry is
// explicit, it promotes the existing entry. Discovered duplicates are
// silently ignored.
func (s *IPSet) Add(sip SourcedIP) {
	if sip.IP == nil {
		return
	}
	key := sip.IP.String()
	if i, ok := s.index[key]; ok {
		if sip.Explicit && !s.entries[i].Explicit {
			s.entries[i].Explicit = true
		}
		// Keep whichever entry actually knows the interface: an explicit
		// IP that duplicates a discovered one carries no interface of its
		// own, and losing the name would hide it from bridge filtering.
		if s.entries[i].Interface == "" && sip.Interface != "" {
			s.entries[i].Interface = sip.Interface
		}
		return
	}
	s.index[key] = len(s.entries)
	s.entries = append(s.entries, sip)
}

// AddDiscovered is a convenience for Add(SourcedIP{IP: ip, Explicit: false}).
func (s *IPSet) AddDiscovered(ip net.IP) {
	s.Add(SourcedIP{IP: ip, Explicit: false})
}

// AddDiscoveredFrom records a discovered IP along with the interface it was
// found on, so bridge filtering can tell a real NIC from a container bridge.
func (s *IPSet) AddDiscoveredFrom(ip net.IP, iface string) {
	s.Add(SourcedIP{IP: ip, Explicit: false, Interface: iface})
}

// AddExplicit is a convenience for Add(SourcedIP{IP: ip, Explicit: true}).
func (s *IPSet) AddExplicit(ip net.IP) {
	s.Add(SourcedIP{IP: ip, Explicit: true})
}

// All returns the entries in insertion order. The returned slice is a
// copy — callers may not modify it. Safe to call on a nil receiver.
func (s *IPSet) All() []SourcedIP {
	if s == nil {
		return nil
	}
	out := make([]SourcedIP, len(s.entries))
	copy(out, s.entries)
	return out
}

// RawIPs extracts just the net.IP values in insertion order.
// Safe to call on a nil receiver.
func (s *IPSet) RawIPs() []net.IP {
	if s == nil {
		return nil
	}
	out := make([]net.IP, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, e.IP)
	}
	return out
}

// Len returns the number of unique IPs in the set.
// Safe to call on a nil receiver.
func (s *IPSet) Len() int {
	if s == nil {
		return 0
	}
	return len(s.entries)
}

// AdvertiseInput is the raw input for computing the set of API addresses
// the server should advertise to clients and to miren.cloud.
type AdvertiseInput struct {
	// ListenAddr is the server's own listen address (e.g. "0.0.0.0:8443").
	// Included in the advertised list only if it has a literal,
	// non-loopback, non-unspecified IP.
	ListenAddr string

	// IPs is the unified list of all candidate IP addresses, each
	// tagged as explicit (user-configured) or discovered (interface scan).
	// Explicit IPs bypass all filtering except loopback / unspecified.
	// Discovered IPs are subject to container-bridge filtering, netcheck,
	// and other pruning.
	IPs []SourcedIP

	// Netcheck is the result of the dual-stack netcheck, if one has run.
	// A nil pointer means netcheck never ran / failed entirely.
	Netcheck *cloudauth.NetcheckDualStackResult

	// Port is the port to append to bare IPs (defaults to 8443).
	Port int
}

// AdvertiseCandidate describes one candidate address the advertise logic
// considered, and whether it ended up in the final advertised set. Used by
// both production (building the final list) and debug tooling (explaining
// the decision for every IP).
type AdvertiseCandidate struct {
	Source         string // "listen", "explicit", "discovered", "netcheck"
	HostPort       string
	IP             net.IP
	Interface      string // discovering interface, when known
	Classification string // tailnet / container-bridge / loopback / link-local / private / global-unicast / other
	Included       bool
	Reason         string
}

// ComputeAdvertise is the single source of truth for computing the addresses
// the server advertises. It returns the ordered list of candidates (including
// rejected ones, so callers can explain why) and the final list of advertised
// host:port strings.
//
// The returned list is intended for StatusReport.APIAddresses, i.e. the
// addresses miren.cloud hands out to clients that want to reach this
// cluster. Loopback and unspecified (0.0.0.0, ::) addresses are never
// included — a client coming in through miren.cloud is by definition not
// running on the same host, so those entries would only produce failed
// connection attempts.
//
// Filtering rules:
//
//  1. Listen address: included if it parses as host:port with a literal,
//     non-loopback, non-unspecified IP.
//
//  2. Explicit IPs (user-configured): always included, except loopback
//     and unspecified which are dropped with a reason.
//
//  3. Discovered IPs (auto-scanned from interfaces):
//     a. Loopback and unspecified are dropped.
//     b. Addresses on a container bridge (docker0, flannel.1, rt0, …) are
//     dropped — they exist only for workloads on this host.
//     c. Addresses the internet can't route to (LAN, CGNAT, ULA, and so any
//     overlay network) are kept, since they may be how this client
//     reaches us.
//     d. Internet-routable IPs are dropped if netcheck ran for that address
//     family and proved the family unreachable or found reachable
//     addresses (replaced by netcheck-confirmed ones).
//     e. Otherwise kept as a fallback.
//
//  4. Netcheck public addresses: included when reachable on at least one port.
func ComputeAdvertise(in AdvertiseInput) ([]AdvertiseCandidate, []string) {
	port := in.Port
	if port == 0 {
		port = 8443
	}
	portStr := strconv.Itoa(port)

	var cands []AdvertiseCandidate
	var final []string
	seen := make(map[string]struct{})

	add := func(c AdvertiseCandidate) {
		cands = append(cands, c)
		if !c.Included {
			return
		}
		if _, ok := seen[c.HostPort]; ok {
			return
		}
		seen[c.HostPort] = struct{}{}
		final = append(final, c.HostPort)
	}

	// 1. Listen address.
	if in.ListenAddr != "" {
		host, _, err := net.SplitHostPort(in.ListenAddr)
		ip := net.ParseIP(host)
		switch {
		case err != nil || ip == nil:
			add(AdvertiseCandidate{
				Source:   "listen",
				HostPort: in.ListenAddr,
				Included: false,
				Reason:   "not a literal IP host",
			})
		case ip.IsUnspecified():
			add(AdvertiseCandidate{
				Source:         "listen",
				HostPort:       in.ListenAddr,
				IP:             ip,
				Classification: "unspecified",
				Included:       false,
				Reason:         "unspecified address (0.0.0.0 / ::) is not routable",
			})
		case ip.IsLoopback():
			add(AdvertiseCandidate{
				Source:         "listen",
				HostPort:       in.ListenAddr,
				IP:             ip,
				Classification: "loopback",
				Included:       false,
				Reason:         "loopback is not reachable from remote clients",
			})
		default:
			add(AdvertiseCandidate{
				Source:         "listen",
				HostPort:       in.ListenAddr,
				IP:             ip,
				Classification: classify(ip),
				Included:       true,
				Reason:         "server listen address",
			})
		}
	}

	// Compute per-family netcheck state.
	v4State := netcheckFamilyState(familyIPv4, in.Netcheck)
	v6State := netcheckFamilyState(familyIPv6, in.Netcheck)

	// 2 & 3. IPs — explicit pass through, discovered are filtered.
	for _, sip := range in.IPs {
		ip := sip.IP
		if ip == nil {
			continue
		}
		hp := net.JoinHostPort(ip.String(), portStr)

		source := "discovered"
		if sip.Explicit {
			source = "explicit"
		}

		cand := AdvertiseCandidate{
			Source:         source,
			HostPort:       hp,
			IP:             ip,
			Interface:      sip.Interface,
			Classification: classifyOn(ip, sip.Interface),
		}

		// Loopback / unspecified always rejected regardless of source.
		if ip.IsUnspecified() {
			cand.Included = false
			cand.Reason = "unspecified address is not routable"
			add(cand)
			continue
		}
		if ip.IsLoopback() {
			cand.Included = false
			cand.Reason = "loopback is not reachable from remote clients"
			add(cand)
			continue
		}

		// Explicit IPs pass through with no further filtering.
		if sip.Explicit {
			cand.Included = true
			cand.Reason = "user-configured"
			add(cand)
			continue
		}

		// --- Discovered IP filtering below ---

		// Container bridges (Miren's own rt0/flannel, plus docker0 and
		// friends) carry addresses that only ever route to workloads on
		// this host, so advertising them just buys clients a timeout.
		if isContainerBridge(sip.Interface) {
			cand.Included = false
			cand.Reason = fmt.Sprintf("container bridge %q is local to this host", sip.Interface)
			add(cand)
			continue
		}

		// Anything the internet can't route to is kept as a candidate. It
		// may be exactly how this client reaches us — over the LAN, or
		// over an overlay like Tailscale — and the client probes every
		// advertised address in parallel and takes the first that answers,
		// so a candidate it can't use costs it nothing.
		//
		// This is the one rule, applied to both families. Singling out
		// IPv4 CGNAT while the matching IPv6 ULA sailed through as
		// "private" is what left tailnet-only clusters advertising the
		// half that rarely works and dropping the half that does.
		if !isPubliclyRoutable(ip) {
			cand.Included = true
			cand.Reason = "not internet-routable, kept for LAN and overlay clients"
			add(cand)
			continue
		}

		state := v4State
		if ip.To4() == nil {
			state = v6State
		}
		switch state {
		case netcheckReachable:
			cand.Included = false
			cand.Reason = "replaced by netcheck-confirmed public address"
		case netcheckUnreachable:
			cand.Included = false
			cand.Reason = "address family proven unreachable by netcheck"
		case netcheckNotRun:
			// No netcheck result yet; keep the candidate.
			fallthrough
		default:
			cand.Included = true
			cand.Reason = "no netcheck override"
		}
		add(cand)
	}

	// 4. Netcheck public addresses.
	for _, hp := range publicAddressesFromNetcheck(in.Netcheck) {
		host, _, _ := net.SplitHostPort(hp)
		ip := net.ParseIP(host)
		add(AdvertiseCandidate{
			Source:         "netcheck",
			HostPort:       hp,
			IP:             ip,
			Classification: classify(ip),
			Included:       true,
			Reason:         "netcheck confirmed reachable",
		})
	}

	return cands, final
}

type netcheckFamily int

const (
	familyIPv4 netcheckFamily = iota
	familyIPv6
)

type netcheckStatus int

const (
	netcheckNotRun netcheckStatus = iota
	netcheckUnreachable
	netcheckReachable
)

// netcheckFamilyState returns what we know about reachability for one address
// family. A nil NetcheckDualStackResult or a nil family response means "not
// run". A response with a non-public/invalid source address is also treated
// as not run (same rule runNetcheck applies). A response with a valid source
// but zero reachable ports is "proven unreachable".
func netcheckFamilyState(fam netcheckFamily, result *cloudauth.NetcheckDualStackResult) netcheckStatus {
	if result == nil {
		return netcheckNotRun
	}
	var resp *cloudauth.NetcheckResponse
	switch fam {
	case familyIPv4:
		resp = result.IPv4
	case familyIPv6:
		resp = result.IPv6
	}
	if resp == nil {
		return netcheckNotRun
	}
	src := net.ParseIP(resp.SourceAddress)
	if src == nil || !src.IsGlobalUnicast() || src.IsPrivate() {
		return netcheckNotRun
	}
	for _, r := range resp.Results {
		if r.Reachable {
			return netcheckReachable
		}
	}
	return netcheckUnreachable
}

// publicAddressesFromNetcheck returns netcheck-confirmed reachable host:port
// strings.
func publicAddressesFromNetcheck(result *cloudauth.NetcheckDualStackResult) []string {
	if result == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var addrs []string
	for _, resp := range []*cloudauth.NetcheckResponse{result.IPv4, result.IPv6} {
		if resp == nil || resp.SourceAddress == "" {
			continue
		}
		src := net.ParseIP(resp.SourceAddress)
		if src == nil || !src.IsGlobalUnicast() || src.IsPrivate() {
			continue
		}
		for _, r := range resp.Results {
			if !r.Reachable {
				continue
			}
			hp := net.JoinHostPort(resp.SourceAddress, strconv.Itoa(r.Port))
			if _, ok := seen[hp]; ok {
				continue
			}
			seen[hp] = struct{}{}
			addrs = append(addrs, hp)
		}
	}
	return addrs
}

// isCGNAT reports whether ip falls in the 100.64.0.0/10 Carrier-Grade NAT
// range (RFC 6598).
func isCGNAT(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	return ip4[0] == 100 && ip4[1]&0xc0 == 0x40
}

// isPubliclyRoutable reports whether the public internet can reach ip. It is
// the runtime-side twin of the cloud's netaddr.IsPubliclyRoutable, and the
// only classification the advertise rules need: overlay networks have to live
// in non-routable space to be overlays, so Tailscale, iroh, Nebula and the
// rest are all covered without naming any of them.
func isPubliclyRoutable(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() ||
		ip.IsPrivate() {
		return false
	}
	return !isCGNAT(ip)
}

// looksLikeTailnet is a display-only hint for `miren debug advertise`, so an
// operator reading the table sees why two addresses on different families got
// the same treatment. It never gates a decision, so a guess that ages badly
// costs nothing but a label. fd7a:115c:a1e0::/48 is Tailscale's ULA prefix.
func looksLikeTailnet(ip net.IP, iface string) bool {
	if strings.HasPrefix(iface, "tailscale") {
		return true
	}
	if isCGNAT(ip) {
		return true
	}
	b := ip.To16()
	return ip.To4() == nil && b != nil &&
		b[0] == 0xfd && b[1] == 0x7a && b[2] == 0x11 &&
		b[3] == 0x5c && b[4] == 0xa1 && b[5] == 0xe0
}

// containerBridgeNames are interfaces whose addresses serve workloads on this
// host and nothing else. rt0 and flannel.* are Miren's own; the rest come
// from other container runtimes that may share the box.
var containerBridgeNames = []string{
	"rt0",      // Miren sandbox bridge
	"flannel.", // flannel VXLAN (flannel.1)
	"flannel-", // flannel wireguard backend
	"cni",      // cni0, cni-podman0
	"cbr0",
	"docker",  // docker0, docker1
	"br-",     // docker user-defined bridges
	"virbr",   // libvirt
	"podman",  // podman0
	"kube-br", // kubelet bridge
}

// isContainerBridge reports whether an interface name is a container bridge.
// An unknown (empty) name is never treated as one — better to advertise a
// useless address than to silently drop the only address that works.
func isContainerBridge(iface string) bool {
	if iface == "" {
		return false
	}
	for _, prefix := range containerBridgeNames {
		if strings.HasPrefix(iface, prefix) {
			return true
		}
	}
	return false
}

// classifyOn is classify with the discovering interface in hand, which lets
// it name the two cases the plain address can't reveal: an overlay address
// (indistinguishable from CGNAT or a ULA) and a container bridge.
func classifyOn(ip net.IP, iface string) string {
	if looksLikeTailnet(ip, iface) {
		return "tailnet"
	}
	if isContainerBridge(iface) {
		return "container-bridge"
	}
	return classify(ip)
}

// classify returns a short string describing the kind of address, for
// diagnostic output.
func classify(ip net.IP) string {
	if ip == nil {
		return "unknown"
	}
	switch {
	case ip.IsLoopback():
		return "loopback"
	case ip.IsLinkLocalUnicast():
		return "link-local"
	case ip.IsPrivate():
		return "private"
	case ip.IsGlobalUnicast():
		return "global-unicast"
	default:
		return "other"
	}
}
