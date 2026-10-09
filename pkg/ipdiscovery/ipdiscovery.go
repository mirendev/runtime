package ipdiscovery

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"miren.dev/runtime/pkg/cloudauth"
)

// Discovery holds information about discovered IP addresses
type Discovery struct {
	Addresses []Address `json:"addresses"`
}

// NetcheckInterface is the Interface recorded on addresses netcheck observed
// rather than read off a local link. It names no real interface.
const NetcheckInterface = "netcheck"

// Address represents an IP address associated with a network interface
type Address struct {
	Interface string `json:"interface"`
	IP        string `json:"ip"`
	Network   string `json:"network"`
	IsIPv6    bool   `json:"is_ipv6"`

	// LinkType is the kernel's name for the kind of link the address sits
	// on ("device", "tuntap", "wireguard", "bridge", …), empty where the
	// platform can't say. Tun and tap devices both report "tuntap".
	LinkType string `json:"link_type,omitempty"`
	// PointToPoint is set for links with no broadcast domain, which is how
	// tunnels (tun devices, WireGuard) present themselves.
	PointToPoint bool `json:"point_to_point,omitempty"`
}

// Options configures IP discovery behavior.
type Options struct {
	// NetcheckURL, when set, enables public IP discovery via a zero-port
	// netcheck request to the given URL. On cloud providers the external IP
	// isn't on any local interface, so this is the only way to find it.
	NetcheckURL string
}

// Discover gathers IP addresses from local interfaces and, when configured,
// from an external netcheck service for public IP discovery.
func Discover(ctx context.Context, log *slog.Logger, opts Options) (*Discovery, error) {
	discovery := &Discovery{
		Addresses: []Address{},
	}

	// Get local interface addresses
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to get interfaces: %w", err)
	}

	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		linkType := linkTypeOf(iface.Name)
		p2p := iface.Flags&net.FlagPointToPoint != 0

		for _, addr := range addrs {
			var ip net.IP
			var network string

			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
				network = v.String()
			case *net.IPAddr:
				ip = v.IP
				network = v.String()
			default:
				continue
			}

			if ip.IsLoopback() {
				continue
			}

			address := Address{
				Interface: iface.Name,
				IP:        ip.String(),
				Network:   network,
				IsIPv6:    ip.To4() == nil,

				LinkType:     linkType,
				PointToPoint: p2p,
			}

			discovery.Addresses = append(discovery.Addresses, address)
		}
	}

	// Discover public IPs via netcheck if configured
	if opts.NetcheckURL != "" {
		result, err := cloudauth.NetcheckDualStack(ctx, opts.NetcheckURL, nil)
		if err != nil {
			log.Warn("public IP discovery failed", "error", err)
		} else {
			for _, resp := range []*cloudauth.NetcheckResponse{result.IPv4, result.IPv6} {
				if resp == nil {
					continue
				}
				ip := net.ParseIP(resp.SourceAddress)
				if ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() {
					log.Info("discovered public IP via netcheck", "ip", ip)
					discovery.Addresses = append(discovery.Addresses, Address{
						Interface: NetcheckInterface,
						IP:        ip.String(),
						IsIPv6:    ip.To4() == nil,
					})
				}
			}
		}
	}

	return discovery, nil
}

// DiscoverWithTimeout is a convenience function that adds a timeout to Discover
func DiscoverWithTimeout(timeout time.Duration, log *slog.Logger, opts Options) (*Discovery, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return Discover(ctx, log, opts)
}
