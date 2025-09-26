//go:build linux
// +build linux

package network

import (
	"crypto/sha512"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
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

	if err := netlink.LinkSetUp(br); err != nil {
		return nil, err
	}

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
			return nil
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

	// Set the bridge's MAC to itself. Otherwise, the bridge will take the
	// lowest-numbered mac on the bridge, and will change as ifs churn
	if err := netlink.LinkSetHardwareAddr(br, br.Attrs().HardwareAddr); err != nil {
		return fmt.Errorf("could not set bridge's mac: %v (%v)", err, br.Attrs().HardwareAddr)
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

	err = ipt4.AppendUnique("filter", "FORWARD", "-i", br.Attrs().Name, "-j", "ACCEPT")
	if err != nil {
		return err
	}

	err = ipt4.AppendUnique("filter", "FORWARD", "-o", br.Attrs().Name, "-j", "ACCEPT")
	if err != nil {
		return err
	}

	ipt6, err := iptables.NewWithProtocol(iptables.ProtocolIPv6)
	if err != nil {
		return err
	}

	err = ipt6.AppendUnique("filter", "FORWARD", "-i", br.Attrs().Name, "-j", "ACCEPT")
	if err != nil {
		return err
	}

	err = ipt6.AppendUnique("filter", "FORWARD", "-o", br.Attrs().Name, "-j", "ACCEPT")
	if err != nil {
		return err
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

func MasqueradeEndpoint(ec *EndpointConfig) error {
	chain := formatChain(ec.Bridge.Name)
	comment := fmt.Sprintf("id: %q", ec.Bridge.Name)

	for _, ac := range ec.Addresses {
		if err := ip.SetupIPMasq(netipx.PrefixIPNet(ac), chain, comment); err != nil {
			return err
		}
	}

	return nil
}
