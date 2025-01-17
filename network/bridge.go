package network

import (
	"crypto/sha256"
	"fmt"
	"net"
	"net/netip"
	"os"
	"syscall"

	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/utils/sysctl"
	"github.com/coreos/go-iptables/iptables"
	"github.com/vishvananda/netlink"
	"go4.org/netipx"
)

func GWPrefix(pr netip.Prefix) netip.Prefix {
	return netip.PrefixFrom(pr.Masked().Addr().Next(), pr.Bits())
}

func SetupBridge(id, name string, addresses []netip.Prefix) error {
	br, err := setupBridge(name)
	if err != nil {
		return fmt.Errorf("failed to setup bridge %q: %w", name, err)
	}

	err = configureGW(br, addresses, id)
	if err != nil {
		return err
	}

	return nil
}

func DeleteBridge(name string) error {
	br, err := bridgeByName(name)
	if err != nil {
		return fmt.Errorf("failed to delete bridge %q: %w", name, err)
	}

	err = netlink.LinkDel(br)
	if err != nil {
		return fmt.Errorf("failed to delete bridge %q: %w", name, err)
	}

	return nil
}

func configureGW(br netlink.Link, addrs []netip.Prefix, id string) error {
	for _, ac := range addrs {
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

		if err = ip.SetupIPMasq(gwIP, id+"-EXT", id); err != nil {
			return err
		}
	}

	return nil
}

func bridgeByName(name string) (*netlink.Bridge, error) {
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

func idToMac(id string) string {
	h := sha256.New()
	h.Write([]byte(id))

	idBytes := h.Sum(nil)

	hwaddr := make(net.HardwareAddr, 6)
	copy(hwaddr, idBytes[2:])

	hwaddr[0] = hwaddr[0] & 0b11111110

	return hwaddr.String()
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
	br, err = bridgeByName(brName)
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

func setupBridge(name string) (*netlink.Bridge, error) {
	// create bridge if necessary
	br, err := ensureBridge(name, 0, false, false)
	if err != nil {
		return nil, fmt.Errorf("failed to create bridge %q: %w", name, err)
	}

	err = enableForwarding(br)
	if err != nil {
		return nil, fmt.Errorf("failed to enable forwarding on bridge %q: %w", name, err)
	}

	return br, nil
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

const (
	// Note: use slash as separator so we can have dots in interface name (VLANs)
	DisableIPv6SysctlTemplate = "net/ipv6/conf/%s/disable_ipv6"
)

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
