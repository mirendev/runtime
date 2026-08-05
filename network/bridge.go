//go:build linux

package network

import (
	"crypto/sha512"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/utils/sysctl"
	"github.com/coreos/go-iptables/iptables"
	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"
	"go4.org/netipx"
)

func netnsPath(pid int) string {
	return fmt.Sprintf("/proc/%d/ns/net", pid)
}

func BridgeByName(name string) (*netlink.Bridge, error) {
	l, err := netlink.LinkByName(name)
	if err != nil {
		return nil, fmt.Errorf("could not lookup %q: %v", name, err)
	}
	br, ok := l.(*netlink.Bridge)
	if !ok {
		return nil, fmt.Errorf("%q already exists but is not a bridge", name)
	}
	return br, nil
}

func ensureBridge(brName string, mtu int, promiscMode, vlanFiltering bool) (*netlink.Bridge, error) {
	br := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name: brName,
			MTU:  mtu,
			// Let kernel use default txqueuelen; leaving it unset
			// means 0, and a zero-length TX queue messes up FIFO
			// traffic shapers which use TX queue length as the
			// default packet limit
			TxQLen: -1,
		},
	}
	if vlanFiltering {
		br.VlanFiltering = &vlanFiltering
	}

	err := netlink.LinkAdd(br)
	if err != nil && err != syscall.EEXIST {
		return nil, fmt.Errorf("could not add %q: %v", brName, err)
	}

	if promiscMode {
		if err := netlink.SetPromiscOn(br); err != nil {
			return nil, fmt.Errorf("could not set promiscuous mode on %q: %v", brName, err)
		}
	}

	// Re-fetch link to read all attributes and if it already existed,
	// ensure it's really a bridge with similar configuration
	br, err = BridgeByName(brName)
	if err != nil {
		return nil, err
	}

	// we want to own the routes for this interface
	_, _ = sysctl.Sysctl(fmt.Sprintf("net/ipv6/conf/%s/accept_ra", brName), "0")

	return br, nil
}

func SetupVeth(netns ns.NetNS, br *netlink.Bridge, ifName string, mtu int, hairpinMode bool, vlanID int, mac string) (*current.Interface, *current.Interface, error) {
	contIface := &current.Interface{}
	hostIface := &current.Interface{}

	err := netns.Do(func(hostNS ns.NetNS) error {
		// create the veth pair in the container and move host end into host netns
		hostVeth, containerVeth, err := ip.SetupVeth(ifName, mtu, mac, hostNS)
		if err != nil {
			return err
		}
		contIface.Name = containerVeth.Name
		contIface.Mac = containerVeth.HardwareAddr.String()
		contIface.Sandbox = netns.Path()
		hostIface.Name = hostVeth.Name
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// need to lookup hostVeth again as its index has changed during ns move
	hostVeth, err := netlink.LinkByName(hostIface.Name)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to lookup %q: %v", hostIface.Name, err)
	}
	hostIface.Mac = hostVeth.Attrs().HardwareAddr.String()

	// connect host veth end to the bridge
	if err := netlink.LinkSetMaster(hostVeth, br); err != nil {
		return nil, nil, fmt.Errorf("failed to connect %q to bridge %v: %v", hostVeth.Attrs().Name, br.Attrs().Name, err)
	}

	// set hairpin mode
	if err = netlink.LinkSetHairpin(hostVeth, hairpinMode); err != nil {
		return nil, nil, fmt.Errorf("failed to setup hairpin mode for %v: %v", hostVeth.Attrs().Name, err)
	}

	if vlanID != 0 {
		err = netlink.BridgeVlanAdd(hostVeth, uint16(vlanID), true, true, false, true)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to setup vlan tag on interface %q: %v", hostIface.Name, err)
		}
	}

	// Set the bridge's MAC to itself. Otherwise, the bridge will take the
	// lowest-numbered mac on the bridge, and will change as ifs churn
	// NOTE This is only done after an interface is added, otherwise the bridge will go
	// into a status == unknown state and not forward traffic.
	if err := netlink.LinkSetHardwareAddr(br, br.Attrs().HardwareAddr); err != nil {
		return nil, nil, fmt.Errorf("could not set bridge's mac: %v (%v)", err, br.Attrs().HardwareAddr)
	}

	// Now that the bridge has an interface, let's bring it up.
	if err := netlink.LinkSetUp(br); err != nil {
		return nil, nil, err
	}

	return hostIface, contIface, nil
}

func SetupBridge(n *BridgeConfig) (*netlink.Bridge, error) {
	vlanFiltering := n.Vlan != 0

	// create bridge if necessary
	br, err := ensureBridge(n.Name, n.MTU, n.PromiscMode, vlanFiltering)
	if err != nil {
		return nil, fmt.Errorf("failed to create bridge %q: %w", n.Name, err)
	}

	err = enableForwarding(br)
	if err != nil {
		return nil, fmt.Errorf("failed to enable forwarding on bridge %q: %w", n.Name, err)
	}

	err = enableBridgeInputRules(br, n.APIPort)
	if err != nil {
		return nil, fmt.Errorf("failed to enable input rules for bridge %q: %w", n.Name, err)
	}

	return br, nil
}

const (
	// Note: use slash as separator so we can have dots in interface name (VLANs)
	DisableIPv6SysctlTemplate = "net/ipv6/conf/%s/disable_ipv6"
)

func TeardownBridge(name string) error {
	br, err := BridgeByName(name)
	if err != nil {
		return fmt.Errorf("failed to lookup bridge %q: %v", name, err)
	}

	// Delete the bridge
	if err = netlink.LinkDel(br); err != nil {
		return fmt.Errorf("failed to delete bridge %q: %v", name, err)
	}

	return nil
}

func enableIPv6(ifName string) error {
	// Enabled IPv6 for loopback "lo" and the interface
	// being configured
	for _, iface := range [2]string{"lo", ifName} {
		ipv6SysctlValueName := fmt.Sprintf(DisableIPv6SysctlTemplate, iface)

		// Read current sysctl value
		value, err := sysctl.Sysctl(ipv6SysctlValueName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ipam_linux: failed to read sysctl %q: %v\n", ipv6SysctlValueName, err)
			continue
		}
		if value == "0" {
			continue
		}

		// Write sysctl to enable IPv6
		_, err = sysctl.Sysctl(ipv6SysctlValueName, "0")
		if err != nil {
			return fmt.Errorf("failed to enable IPv6 for interface %q (%s=%s): %v", iface, ipv6SysctlValueName, value, err)
		}
	}

	return nil
}

func ConfigureIface(log *slog.Logger, ifName string, nc *EndpointConfig) error {
	_, _ = sysctl.Sysctl(fmt.Sprintf("net/ipv6/conf/%s/accept_dad", ifName), "0")
	_, _ = sysctl.Sysctl(fmt.Sprintf("net/ipv4/conf/%s/arp_notify", ifName), "1")

	link, err := netlink.LinkByName(ifName)
	if err != nil {
		return fmt.Errorf("failed to lookup %q: %v", ifName, err)
	}

	err = enableIPv6(ifName)
	if err != nil {
		return errors.Wrapf(err, "unable to enable ipv6")
	}

	for _, ac := range nc.Addresses {
		addr := &netlink.Addr{
			IPNet: netipx.PrefixIPNet(ac),
			Label: "",
		}
		if err = netlink.AddrAdd(link, addr); err != nil {
			return fmt.Errorf("failed to add IP addr %v to %q: %v", ac, ifName, err)
		}

		log.Debug("added address", "address", ac.String(), "interface", ifName)
	}

	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to set %q UP: %v", ifName, err)
	}

	ip.SettleAddresses(ifName, 10)

	for _, r := range nc.Routes {
		route := netlink.Route{
			Dst:       netipx.PrefixIPNet(r.Dest),
			LinkIndex: link.Attrs().Index,
			Gw:        r.Via.AsSlice(),
		}

		if err = netlink.RouteAddEcmp(&route); err != nil {
			return fmt.Errorf("failed to add route '%v via %v dev %v': %v", r.Dest, r.Via, ifName, err)
		}
	}

	return nil
}

func ensureAddr(br netlink.Link, family int, ipn *net.IPNet, forceAddress bool) error {
	addrs, err := netlink.AddrList(br, family)
	if err != nil && err != syscall.ENOENT {
		return fmt.Errorf("could not get list of IP addresses: %v", err)
	}

	ipnStr := ipn.String()
	for _, a := range addrs {

		// string comp is actually easiest for doing IPNet comps
		if a.IPNet.String() == ipnStr {
			continue
		}

		// Multiple addresses are allowed on the bridge if the
		// corresponding subnets do not overlap. For IPv4 or for
		// overlapping IPv6 subnets, reconfigure the IP address if
		// forceAddress is true, otherwise throw an error.
		if a.Contains(ipn.IP) || ipn.Contains(a.IP) {
			if forceAddress {
				if err = deleteAddr(br, a.IPNet); err != nil {
					return err
				}
			} else {
				return fmt.Errorf("%q already has an IP address different from %v (%v, %v)", br.Attrs().Name, ipnStr, a.IP.String(), a.IPNet.String())
			}
		}
	}

	addr := &netlink.Addr{IPNet: ipn, Label: ""}
	if err := netlink.AddrAdd(br, addr); err != nil && err != syscall.EEXIST {
		return fmt.Errorf("could not add IP address to %q: %v", br.Attrs().Name, err)
	}

	return nil
}

func deleteAddr(br netlink.Link, ipn *net.IPNet) error {
	addr := &netlink.Addr{IPNet: ipn, Label: ""}

	if err := netlink.AddrDel(br, addr); err != nil {
		return fmt.Errorf("could not remove IP address from %q: %v", br.Attrs().Name, err)
	}

	return nil
}

func enableForwarding(br netlink.Link) error {
	ipt4, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err != nil {
		return err
	}

	// Insert at position 1 (beginning) to ensure rules take precedence over
	// restrictive REJECT rules that may exist (e.g., Oracle Cloud default firewall)
	err = ipt4.InsertUnique("filter", "FORWARD", 1, "-i", br.Attrs().Name, "-j", "ACCEPT")
	if err != nil {
		return err
	}

	err = ipt4.InsertUnique("filter", "FORWARD", 1, "-o", br.Attrs().Name, "-j", "ACCEPT")
	if err != nil {
		return err
	}

	ipt6, err := iptables.NewWithProtocol(iptables.ProtocolIPv6)
	if err != nil {
		return err
	}

	err = ipt6.InsertUnique("filter", "FORWARD", 1, "-i", br.Attrs().Name, "-j", "ACCEPT")
	if err != nil {
		return err
	}

	err = ipt6.InsertUnique("filter", "FORWARD", 1, "-o", br.Attrs().Name, "-j", "ACCEPT")
	if err != nil {
		return err
	}

	return nil
}

// bridgeInputPort is a host port reachable from the bridge.
type bridgeInputPort struct {
	port  int
	proto []string
	why   string
}

// bridgeInputPorts lists the host ports sandboxes may reach, beyond which the
// host is closed to them.
func bridgeInputPorts(apiPort int) []bridgeInputPort {
	ports := []bridgeInputPort{
		{53, []string{"udp", "tcp"}, "container DNS resolution"},
		{5000, []string{"tcp"}, "buildkit pushing images to the registry"},
	}

	if apiPort > 0 {
		// Both protocols: the RPC transport is HTTP/3 over QUIC, which is UDP.
		// A tcp-only rule appears to work wherever the host's INPUT policy is
		// permissive and fails everywhere it is not.
		ports = append(ports, bridgeInputPort{apiPort, []string{"udp", "tcp"}, "cluster API access using workload identity"})
	}

	return ports
}

// enableBridgeInputRules adds INPUT rules to allow traffic from bridge to host services
func enableBridgeInputRules(br netlink.Link, apiPort int) error {
	ipt4, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err != nil {
		return err
	}

	ipt6, err := iptables.NewWithProtocol(iptables.ProtocolIPv6)
	if err != nil {
		return err
	}

	// The rulespec is what InsertUnique matches on, so it must stay byte-identical
	// across releases: changing it for an existing rule stops matching the rule
	// already installed and inserts a duplicate on every restart.
	for _, ipt := range []*iptables.IPTables{ipt4, ipt6} {
		for _, p := range bridgeInputPorts(apiPort) {
			for _, proto := range p.proto {
				err = ipt.InsertUnique("filter", "INPUT", 1,
					"-i", br.Attrs().Name, "-p", proto, "--dport", strconv.Itoa(p.port), "-j", "ACCEPT")
				if err != nil {
					return fmt.Errorf("allowing %s/%d from bridge (%s): %w", proto, p.port, p.why, err)
				}
			}
		}
	}

	return nil
}

func ConfigureGW(br netlink.Link, ec *EndpointConfig) error {
	for _, ac := range ec.Bridge.Addresses {
		gwIP := netipx.PrefixIPNet(ac)

		var family int

		if gwIP.IP.To4() != nil {
			family = netlink.FAMILY_V4
		} else {
			family = netlink.FAMILY_V6
		}

		err := ensureAddr(br, family, gwIP, false)
		if err != nil {
			return err
		}
		if family == netlink.FAMILY_V4 {
			err = ip.EnableIP4Forward()
		} else {
			err = ip.EnableIP6Forward()
		}

		if err != nil {
			return err
		}
	}

	return nil
}

func formatChain(id string) string {
	output := sha512.Sum512([]byte(id))
	return fmt.Sprintf("MIREN-%x", output)[:28]
}

type Subnet struct {
	Id     string
	IP     []netip.Prefix
	OSName string
}

func CalculateGateway(pr netip.Prefix) netip.Prefix {
	start := pr.Masked()
	return netip.PrefixFrom(start.Addr().Next(), start.Bits())
}

var retries = []int{0, 50, 500, 1000, 1000}

func CheckBridgeStatus(name string) error {
	for idx, sleep := range retries {
		time.Sleep(time.Duration(sleep) * time.Millisecond)

		hostVeth, err := netlink.LinkByName(name)
		if err != nil {
			return err
		}

		if hostVeth.Attrs().OperState == netlink.OperUp {
			break
		}

		if idx == len(retries)-1 {
			return fmt.Errorf("bridge port in error state: %s", hostVeth.Attrs().OperState)
		}
	}

	return nil
}

// ReconcileBridgeAddresses owns the per-bridge NAT chain shape and removes
// bridge addresses + POSTROUTING jumps that belong to subnets no longer in
// `desired`. It runs at sandbox controller init so the chain is in the
// right shape before any sandbox is created. Drift happens when a runner's
// flannel lease rotates (typically after the runner is offline long enough
// for its etcd lease to expire) and a fresh subnet is allocated; without
// this reconcile the host bridge accumulates stale addresses across lease
// eras, and the per-bridge MIREN-* chain accumulates rules that interfere
// with traffic on the new subnet (MIR-1108).
func ReconcileBridgeAddresses(log *slog.Logger, br netlink.Link, desired []netip.Prefix) error {
	bridgeName := br.Attrs().Name
	chain := formatChain(bridgeName)
	comment := fmt.Sprintf("id: %q", bridgeName)

	desiredSet := make(map[string]bool, len(desired))
	for _, p := range desired {
		desiredSet[p.String()] = true
	}

	addrs, err := netlink.AddrList(br, netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("listing addresses on %s: %w", bridgeName, err)
	}

	var stale []netip.Prefix
	for _, addr := range addrs {
		prefix, ok := netipx.FromStdIPNet(addr.IPNet)
		if !ok {
			continue
		}
		if !desiredSet[prefix.String()] {
			stale = append(stale, prefix)
		}
	}

	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err != nil {
		return fmt.Errorf("locating iptables: %w", err)
	}

	if len(stale) > 0 {
		log.Warn("removing stale bridge state",
			"bridge", bridgeName, "stale", stale, "desired", desired)

		for _, s := range stale {
			ipn := netipx.PrefixIPNet(s)
			if err := deleteAddr(br, ipn); err != nil {
				return fmt.Errorf("removing stale bridge address %s: %w", s, err)
			}
			log.Info("removed stale bridge address", "bridge", bridgeName, "address", s)

			if err := removeStalePostroutingJumps(log, ipt, chain, comment, s); err != nil {
				log.Warn("failed to clean POSTROUTING jumps",
					"bridge", bridgeName, "subnet", s, "error", err)
			}
		}
	}

	if err := reconcileMasqChain(ipt, chain, comment, desired); err != nil {
		return fmt.Errorf("reconciling chain on %s: %w", bridgeName, err)
	}

	return nil
}

// postroutingSourceIP extracts the source IP from a `-A POSTROUTING -s X ...` rule line.
var postroutingSourceIP = regexp.MustCompile(`-s\s+(\d+\.\d+\.\d+\.\d+)`)

func removeStalePostroutingJumps(log *slog.Logger, ipt *iptables.IPTables, chain, comment string, stale netip.Prefix) error {
	rules, err := ipt.List("nat", "POSTROUTING")
	if err != nil {
		return fmt.Errorf("listing POSTROUTING: %w", err)
	}

	target := "-j " + chain
	for _, rule := range rules {
		if !strings.Contains(rule, target) {
			continue
		}
		m := postroutingSourceIP.FindStringSubmatch(rule)
		if m == nil {
			continue
		}
		src, err := netip.ParseAddr(m[1])
		if err != nil || !stale.Contains(src) {
			continue
		}
		if err := ipt.DeleteIfExists("nat", "POSTROUTING",
			"-s", src.String(),
			"-m", "comment", "--comment", comment,
			"-j", chain,
		); err != nil {
			log.Warn("failed to delete stale POSTROUTING jump",
				"src", src, "chain", chain, "error", err)
		}
	}
	return nil
}

// MasqueradeEndpoint adds a POSTROUTING jump for each address in `ec` to
// the per-bridge MIREN-* chain, so packets with that source pod IP get
// masqueraded on egress to non-pod-subnet destinations. The bridge-scope
// chain content (per-subnet ACCEPTs followed by the MASQUERADE catch-all)
// is owned by ReconcileBridgeAddresses, which runs at controller init
// before any sandbox is created.
func MasqueradeEndpoint(ec *EndpointConfig) error {
	if len(ec.Addresses) == 0 {
		return nil
	}

	chain := formatChain(ec.Bridge.Name)
	comment := fmt.Sprintf("id: %q", ec.Bridge.Name)

	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err != nil {
		return fmt.Errorf("locating iptables: %w", err)
	}

	for _, ac := range ec.Addresses {
		ipn := netipx.PrefixIPNet(ac)
		if err := ipt.AppendUnique("nat", "POSTROUTING",
			"-s", ipn.IP.String(),
			"-m", "comment", "--comment", comment,
			"-j", chain,
		); err != nil {
			return fmt.Errorf("adding POSTROUTING jump for %s: %w", ipn.IP, err)
		}
	}

	return nil
}

// reconcileMasqChain converges the per-bridge NAT chain to the desired shape:
// one ACCEPT per pod subnet followed by a single MASQUERADE catch-all. Order
// matters because MASQUERADE is terminal, so any ACCEPT after it would be
// unreachable. The previous implementation appended ACCEPT then MASQUERADE
// per call, which left later subnets' ACCEPTs stranded after the
// already-present MASQUERADE (MIR-1108).
//
// To stay idempotent under concurrent calls and on already-correct chains,
// the chain is only flushed and rebuilt when its current content does not
// already match the desired shape.
func reconcileMasqChain(ipt *iptables.IPTables, chain, comment string, addresses []netip.Prefix) error {
	chains, err := ipt.ListChains("nat")
	if err != nil {
		return fmt.Errorf("listing nat chains: %w", err)
	}
	if !slices.Contains(chains, chain) {
		if err := ipt.NewChain("nat", chain); err != nil {
			return fmt.Errorf("creating chain %s: %w", chain, err)
		}
	}

	desired := desiredMasqRules(comment, addresses)

	matches, err := chainMatchesDesired(ipt, chain, desired)
	if err != nil {
		return fmt.Errorf("inspecting chain %s: %w", chain, err)
	}
	if matches {
		return nil
	}

	if err := ipt.ClearChain("nat", chain); err != nil {
		return fmt.Errorf("clearing chain %s: %w", chain, err)
	}
	for _, rule := range desired {
		if err := ipt.Append("nat", chain, rule...); err != nil {
			return fmt.Errorf("appending rule to %s: %w", chain, err)
		}
	}
	return nil
}

// desiredMasqRules builds the chain content in the order it should appear:
// per-subnet ACCEPTs first, then the catch-all MASQUERADE last.
func desiredMasqRules(comment string, addresses []netip.Prefix) [][]string {
	rules := make([][]string, 0, len(addresses)+1)
	for _, ac := range addresses {
		ipn := netipx.PrefixIPNet(ac).String()
		rules = append(rules, []string{"-d", ipn, "-m", "comment", "--comment", comment, "-j", "ACCEPT"})
	}
	rules = append(rules, []string{"!", "-d", "224.0.0.0/4", "-m", "comment", "--comment", comment, "-j", "MASQUERADE"})
	return rules
}

// chainMatchesDesired returns true when the chain's existing rules already
// represent the desired shape: every desired rule exists, no extra rules
// exist, and the last rule is MASQUERADE (so all ACCEPTs precede it).
func chainMatchesDesired(ipt *iptables.IPTables, chain string, desired [][]string) (bool, error) {
	for _, rule := range desired {
		ok, err := ipt.Exists("nat", chain, rule...)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}

	rules, err := ipt.List("nat", chain)
	if err != nil {
		return false, err
	}
	// rules[0] is the "-N <chain>" header; remaining entries are "-A ..." rules.
	if len(rules)-1 != len(desired) {
		return false, nil
	}
	return strings.Contains(rules[len(rules)-1], "-j MASQUERADE"), nil
}
