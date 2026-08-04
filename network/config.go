package network

import (
	"fmt"
	"log/slog"
	"net/netip"

	"github.com/vishvananda/netlink"
	"miren.dev/runtime/pkg/netdb"
)

type Route struct {
	Dest netip.Prefix
	Via  netip.Addr
}

type EndpointConfig struct {
	Addresses []netip.Prefix

	Routes []*Route

	Bridge *BridgeConfig
}

var (
	V4all = netip.MustParsePrefix("0.0.0.0/0")
	V6all = netip.MustParsePrefix("::/0")
)

func (e *EndpointConfig) FindRoute(dest netip.Addr) *Route {
	for _, r := range e.Routes {
		if r.Dest.Contains(dest) {
			return r
		}
	}

	return nil
}

func (e *EndpointConfig) DeriveDefaultGateway() error {
	var setIPv4, setIPv6 bool

	for _, addr := range e.Addresses {
		if addr.Addr().Is4() {
			if setIPv4 {
				continue
			}

			setIPv4 = true

			gw := addr.Masked().Addr().Next()

			e.Routes = append(e.Routes, &Route{
				Dest: V4all,
				Via:  gw,
			})
		} else {
			if setIPv6 {
				continue
			}

			setIPv6 = true

			gw := addr.Masked().Addr().Next()

			e.Routes = append(e.Routes, &Route{
				Dest: V6all,
				Via:  gw,
			})
		}
	}

	return nil
}

type BridgeConfig struct {
	Name      string
	Addresses []netip.Prefix

	MTU         int
	Vlan        int
	PromiscMode bool

	// APIPort is the port the cluster API listens on, opened to the bridge so
	// sandboxes can reach it with their workload identity token. Zero leaves the
	// port closed, which is correct when no API runs on this host.
	APIPort int
}

// InUseFunc returns the set of IP addresses currently live on the bridge,
// determined independently of the netdb bookkeeping (e.g. from running
// containers). It lets AllocateOnBridge detect and skip an address that netdb
// believes is free but that a running sandbox is actually using — a bookkeeping
// divergence that would otherwise produce a duplicate assignment (MIR-1238).
type InUseFunc func() (map[netip.Addr]bool, error)

// allocBridgeMaxAttempts bounds how many live-but-unreserved addresses
// AllocateOnBridge will skip past before giving up.
const allocBridgeMaxAttempts = 32

// reserveConflictFree reserves an address from the subnet that is not already
// live on the bridge. netdb believes every address it hands out is free, but its
// bookkeeping can diverge from reality (MIR-1238); when Reserve returns an
// address that live shows in use, that reservation is simply retained (Reserve
// already marked it reserved, so it is quarantined and won't be handed out
// again) and the next address is tried.
func reserveConflictFree(log *slog.Logger, bridge string, subnet *netdb.Subnet, live map[netip.Addr]bool) (netip.Prefix, error) {
	for attempt := 0; ; attempt++ {
		ep, err := subnet.Reserve()
		if err != nil {
			return netip.Prefix{}, err
		}

		if live == nil || !live[ep.Addr()] {
			return ep, nil
		}

		log.Error("netdb allocated an address already live on the bridge; quarantining and retrying",
			"bridge", bridge, "addr", ep.Addr())

		if attempt+1 >= allocBridgeMaxAttempts {
			return netip.Prefix{}, fmt.Errorf("no conflict-free address available on bridge %s after %d attempts",
				bridge, allocBridgeMaxAttempts)
		}
	}
}

func AllocateOnBridge(log *slog.Logger, name string, subnet *netdb.Subnet, inUse InUseFunc) (*EndpointConfig, error) {
	if name == "" {
		return nil, fmt.Errorf("bridge name must be provided")
	}

	_, err := netlink.LinkByName(name)
	if err != nil {
		return nil, fmt.Errorf("failed to find bridge %s: %w", name, err)
	}

	bridge := subnet.Router()

	var live map[netip.Addr]bool
	if inUse != nil {
		live, err = inUse()
		if err != nil {
			// A failure to enumerate live addresses must not block allocation;
			// fall back to netdb-only behavior (the prior contract).
			log.Warn("failed to enumerate in-use bridge addresses; allocating without conflict check",
				"bridge", name, "error", err)
			live = nil
		}
	}

	ep, err := reserveConflictFree(log, name, subnet, live)
	if err != nil {
		return nil, err
	}

	ec := &EndpointConfig{
		Addresses: []netip.Prefix{ep},
		Bridge: &BridgeConfig{
			Name:      name,
			Addresses: []netip.Prefix{bridge},
		},
	}

	err = ec.DeriveDefaultGateway()
	if err != nil {
		return nil, err
	}

	return ec, nil
}

func SetupOnBridge(name string, subnet *netdb.Subnet, prefixes []netip.Prefix) (*EndpointConfig, error) {
	if name == "" {
		return nil, fmt.Errorf("bridge name must be provided")
	}

	if len(prefixes) == 0 {
		return nil, fmt.Errorf("at least one prefix must be provided")
	}

	_, err := netlink.LinkByName(name)
	if err != nil {
		return nil, fmt.Errorf("failed to find bridge %s: %w", name, err)
	}

	bridge := subnet.Router()

	// Re-reserve the provided IPs to ensure they are marked as allocated in the netdb
	var addresses []netip.Prefix
	for _, p := range prefixes {
		if err := subnet.ReserveSpecificAddr(p.Addr()); err != nil {
			return nil, fmt.Errorf("failed to re-reserve IP %s: %w", p.Addr(), err)
		}
		// Use the subnet's bit length for consistency
		addresses = append(addresses, netip.PrefixFrom(p.Addr(), subnet.Prefix().Bits()))
	}

	ec := &EndpointConfig{
		Addresses: addresses,
		Bridge: &BridgeConfig{
			Name:      name,
			Addresses: []netip.Prefix{bridge},
		},
	}

	err = ec.DeriveDefaultGateway()
	if err != nil {
		return nil, err
	}

	return ec, nil
}
