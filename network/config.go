package network

import (
	"fmt"
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
}

func AllocateOnBridge(name string, subnet *netdb.Subnet) (*EndpointConfig, error) {
	if name == "" {
		return nil, fmt.Errorf("bridge name must be provided")
	}

	_, err := netlink.LinkByName(name)
	if err != nil {
		return nil, fmt.Errorf("failed to find bridge %s: %w", name, err)
	}

	bridge := subnet.Router()

	ep, err := subnet.Reserve()
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

func SetupOnBridge(name string, subnet *netdb.Subnet, prefix []netip.Prefix) (*EndpointConfig, error) {
	if name == "" {
		return nil, fmt.Errorf("bridge name must be provided")
	}

	_, err := netlink.LinkByName(name)
	if err != nil {
		return nil, fmt.Errorf("failed to find bridge %s: %w", name, err)
	}

	bridge := subnet.Router()

	ep, err := subnet.Reserve()
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
