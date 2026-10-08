package coordinate

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/clusternetwork"
	"miren.dev/runtime/pkg/ipdiscovery"
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

	// LinkType and PointToPoint describe the link, when discovery could
	// tell (see ipdiscovery.Address). They are what separates an overlay
	// address from a LAN one in the same range.
	LinkType     string
	PointToPoint bool
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
			s.entries[i].LinkType = sip.LinkType
			s.entries[i].PointToPoint = sip.PointToPoint
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

// AddDiscoveredAddress records an address from ipdiscovery with everything
// discovery learned about its link. Addresses netcheck observed carry a
// placeholder interface name, which is dropped rather than passed off as
// a real link. Reports whether the address was usable.
func (s *IPSet) AddDiscoveredAddress(a ipdiscovery.Address) bool {
	ip := net.ParseIP(a.IP)
	if ip == nil {
		return false
	}
	sip := SourcedIP{IP: ip, Interface: a.Interface, LinkType: a.LinkType, PointToPoint: a.PointToPoint}
	if a.Interface == ipdiscovery.NetcheckInterface {
		sip.Interface = ""
	}
	s.Add(sip)
	return true
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
	Source    string // clusternetwork.Source*
	HostPort  string
	IP        net.IP
	Interface string // discovering interface, when known
	Range     string // clusternetwork.Range*, or loopback / link-local / unspecified / multicast
	Class     string // clusternetwork.Class*; empty for addresses never advertised
	Label     string // vendor hint, never a decision input
	Included  bool
	Reason    string
}

// Wire renders an advertised candidate as it goes to cloud.
func (c AdvertiseCandidate) Wire() clusternetwork.AdvertisedAddress {
	return clusternetwork.AdvertisedAddress{
		Transport: clusternetwork.TransportIP,
		Address:   c.HostPort,
		Class:     c.Class,
		Range:     c.Range,
		Interface: c.Interface,
		Source:    c.Source,
		Label:     c.Label,
	}
}

// WireDetails renders advertised candidates as the report's
// api_address_details. The result is never nil, so advertising nothing
// goes out as [] rather than null (see clusternetwork.Report).
func WireDetails(cands []AdvertiseCandidate) []clusternetwork.AdvertisedAddress {
	out := make([]clusternetwork.AdvertisedAddress, len(cands))
	for i, c := range cands {
		out[i] = c.Wire()
	}
	return out
}

// HostPorts extracts the flat address list from advertised candidates.
func HostPorts(cands []AdvertiseCandidate) []string {
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.HostPort)
	}
	return out
}

// linkFor returns the entry in ips for ip, so a candidate known by another
// route (the listen address, a netcheck observation) can carry the link
// facts discovery found for it. The zero value when no entry matches.
func linkFor(ips []SourcedIP, ip net.IP) SourcedIP {
	for _, sip := range ips {
		if sip.IP.Equal(ip) {
			return sip
		}
	}
	return SourcedIP{}
}

// newCandidate fills in everything about ip that doesn't depend on the
// filtering decision.
func newCandidate(source, hostPort string, ip net.IP, sip SourcedIP) AdvertiseCandidate {
	return AdvertiseCandidate{
		Source:    source,
		HostPort:  hostPort,
		IP:        ip,
		Interface: sip.Interface,
		Range:     addressRange(ip),
		Class:     classOf(ip, sip.Interface, sip.LinkType, sip.PointToPoint),
		Label:     labelOf(ip, sip.Interface),
	}
}

// ComputeAdvertise is the single source of truth for computing the addresses
// the server advertises. It returns the ordered list of candidates (including
// rejected ones, so callers can explain why) and the advertised subset, in
// order and de-duplicated by host:port.
//
// The returned list is intended for clusternetwork.Report.APIAddresses, i.e. the
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
//     c. IANA special-purpose addresses (198.18/15, 240/4, NAT64, …) are
//     dropped — no other host can reach them.
//     d. Addresses the internet can't route to (LAN, CGNAT, ULA, and so any
//     overlay network) are kept, since they may be how this client
//     reaches us.
//     e. Internet-routable IPs are dropped if netcheck ran for that address
//     family and proved the family unreachable or found reachable
//     addresses (replaced by netcheck-confirmed ones).
//     f. Otherwise kept as a fallback.
//
//  4. Netcheck public addresses: included when reachable on at least one port.
func ComputeAdvertise(in AdvertiseInput) ([]AdvertiseCandidate, []AdvertiseCandidate) {
	port := in.Port
	if port == 0 {
		port = 8443
	}
	portStr := strconv.Itoa(port)

	var cands []AdvertiseCandidate
	var final []AdvertiseCandidate
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
		final = append(final, c)
	}

	// 1. Listen address.
	if in.ListenAddr != "" {
		host, _, err := net.SplitHostPort(in.ListenAddr)
		ip := net.ParseIP(host)
		switch {
		case err != nil || ip == nil:
			add(AdvertiseCandidate{
				Source:   clusternetwork.SourceListen,
				HostPort: in.ListenAddr,
				Included: false,
				Reason:   "not a literal IP host",
			})
		default:
			// The listen address is usually also on an interface; borrow
			// what discovery knows about that link, so an overlay listen
			// address isn't mistaken for a LAN one.
			cand := newCandidate(clusternetwork.SourceListen, in.ListenAddr, ip, linkFor(in.IPs, ip))
			switch {
			case ip.IsUnspecified():
				cand.Reason = "unspecified address (0.0.0.0 / ::) is not routable"
			case ip.IsLoopback():
				cand.Reason = "loopback is not reachable from remote clients"
			default:
				cand.Included = true
				cand.Reason = "server listen address"
			}
			add(cand)
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

		source := clusternetwork.SourceDiscovered
		if sip.Explicit {
			source = clusternetwork.SourceExplicit
		}

		cand := newCandidate(source, hp, ip, sip)

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

		// Special-purpose space is reachable from nowhere, not even the
		// LAN: a VPN client's 198.18/15 "fake IP" on its tun device, say.
		// Advertising it would only hand clients a timeout.
		if cand.Range == clusternetwork.RangeSpecial {
			cand.Included = false
			cand.Reason = "special-purpose address is not reachable from other hosts"
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
		// On a host with its public address assigned directly to a NIC,
		// netcheck's observation is that same address, and the link it
		// sits on is still worth reporting. Behind NAT nothing matches
		// and the interface stays empty.
		cand := newCandidate(clusternetwork.SourceNetcheck, hp, ip, linkFor(in.IPs, ip))
		cand.Included = true
		cand.Reason = "netcheck confirmed reachable"
		add(cand)
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

// Ranges that only matter to the advertise logic itself. Addresses in them
// are never advertised, so they have no wire constant.
const (
	rangeLoopback    = "loopback"
	rangeLinkLocal   = "link-local"
	rangeUnspecified = "unspecified"
	rangeMulticast   = "multicast"
)

var (
	cgnatNet = mustCIDR("100.64.0.0/10") // RFC 6598
	ulaNet   = mustCIDR("fc00::/7")      // RFC 4193

	// specialNets is the slice of the IANA special-purpose registries that
	// Go's net.IP predicates don't already cover and that no client could
	// reach us at. 198.18.0.0/15 earns its place in practice: VPN clients
	// with a "fake IP" mode hand it out on a local tun device. Documentation
	// ranges (TEST-NET, 2001:db8::/32) are deliberately absent; they never
	// appear on a real host, and our own tests use them as stand-ins for
	// public addresses.
	specialNets = []*net.IPNet{
		mustCIDR("0.0.0.0/8"),      // "this network"
		mustCIDR("192.0.0.0/24"),   // IETF protocol assignments
		mustCIDR("198.18.0.0/15"),  // benchmarking
		mustCIDR("240.0.0.0/4"),    // reserved, and limited broadcast
		mustCIDR("64:ff9b::/96"),   // NAT64 well-known prefix
		mustCIDR("64:ff9b:1::/48"), // NAT64 local-use
		mustCIDR("100::/64"),       // discard-only
	}

	// tailscaleULA is the /48 Tailscale assigns its IPv6 node addresses from.
	tailscaleULA = mustCIDR("fd7a:115c:a1e0::/48")
)

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// isCGNAT reports whether ip falls in the 100.64.0.0/10 Carrier-Grade NAT
// range (RFC 6598).
func isCGNAT(ip net.IP) bool {
	return cgnatNet.Contains(ip)
}

// addressRange places ip in the IANA registries. It is a pure function of the
// address and makes no guess about what network the address belongs to;
// that judgment is classOf's.
func addressRange(ip net.IP) string {
	switch {
	case ip == nil:
		return ""
	case ip.IsUnspecified():
		return rangeUnspecified
	case ip.IsLoopback():
		return rangeLoopback
	case ip.IsLinkLocalUnicast():
		return rangeLinkLocal
	case ip.IsMulticast():
		return rangeMulticast
	case isCGNAT(ip):
		return clusternetwork.RangeShared
	case ulaNet.Contains(ip):
		return clusternetwork.RangeULA
	case ip.IsPrivate():
		return clusternetwork.RangePrivate
	}
	for _, n := range specialNets {
		if n.Contains(ip) {
			return clusternetwork.RangeSpecial
		}
	}
	return clusternetwork.RangePublic
}

// isPubliclyRoutable reports whether the public internet can reach ip. It is
// the runtime-side twin of the cloud's netaddr.IsPubliclyRoutable, and the
// only classification the advertise rules need: overlay networks have to live
// in non-routable space to be overlays, so Tailscale, iroh, Nebula and the
// rest are all covered without naming any of them.
func isPubliclyRoutable(ip net.IP) bool {
	return addressRange(ip) == clusternetwork.RangePublic
}

// classOf judges what network a client has to be on to reach ip. The range
// decides public versus not; for the rest, the link the address sits on
// tells an overlay or container bridge apart from the LAN.
//
// The range alone is never enough to call something an overlay. CGNAT
// space on an ordinary NIC is an ISP's or Starlink's NAT, and a ULA on one
// is a home or cloud LAN; Tailscale's own code drops such addresses as its
// own and then needs a pile of exceptions to undo the damage.
//
// Returns "" for addresses that are never advertised (loopback and the like).
func classOf(ip net.IP, iface, linkType string, pointToPoint bool) string {
	r := addressRange(ip)
	switch r {
	case "", rangeLoopback, rangeLinkLocal, rangeUnspecified, rangeMulticast:
		return ""
	}
	if isContainerBridge(iface) {
		return clusternetwork.ClassContainerBridge
	}
	if r == clusternetwork.RangePublic {
		return clusternetwork.ClassPublic
	}
	if isOverlayLink(iface, linkType, pointToPoint) {
		return clusternetwork.ClassOverlay
	}
	return clusternetwork.ClassLAN
}

// isOverlayLink reports whether a link is a tunnel into some network other
// than the one the host is plugged into. The kernel's link type is the
// evidence. The point-to-point flag stands in only when the type is unknown,
// because PPPoE and raw-IP cellular links set it too, and an ISP's CGNAT
// address arriving over ppp0 is a LAN address, not an overlay. The name is
// only trusted for the one overlay whose interface is reliably named for it,
// so a host without netlink (or a test) still recognizes tailscale0.
//
// A bridge is not an overlay, and is deliberately not treated as a container
// bridge either: Proxmox and plenty of bonded setups put the host's own LAN
// address on one (vmbr0, br0). Container bridges are recognized by name.
func isOverlayLink(iface, linkType string, pointToPoint bool) bool {
	if strings.HasPrefix(iface, "tailscale") {
		return true
	}
	switch linkType {
	case "tuntap", "tun", "wireguard":
		return true
	case "":
		return pointToPoint
	}
	return false
}

// labelOf names the vendor or technology behind an address when it is
// recognizable. It never feeds a decision, so a guess that ages badly costs
// nothing but a label. CGNAT space alone earns no label: on eth0 it is an
// ISP's, not Tailscale's.
func labelOf(ip net.IP, iface string) string {
	switch {
	case strings.HasPrefix(iface, "tailscale"), tailscaleULA.Contains(ip):
		return "tailscale"
	case strings.HasPrefix(iface, "zt"):
		return "zerotier"
	case strings.HasPrefix(iface, "wt"):
		return "netbird"
	case strings.HasPrefix(iface, "wg"):
		return "wireguard"
	}
	return ""
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
