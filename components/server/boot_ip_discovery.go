//go:build linux

package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/pkg/ipdiscovery"
	"miren.dev/runtime/pkg/readiness"
)

type ipDiscoveryBootInputs struct {
	log           *slog.Logger
	additionalIPs []string
}

type ipDiscoveryBootOutput struct {
	ipSet *coordinate.IPSet
}

type ipDiscoveryBoot struct {
	component *readiness.Component
	inputs    ipDiscoveryBootInputs
	result    ipDiscoveryBootOutput
}

func ipDiscoveryInputs(options StartOptions) ipDiscoveryBootInputs {
	return ipDiscoveryBootInputs{
		log:           options.Log,
		additionalIPs: append([]string(nil), options.Config.TLS.AdditionalIPs...),
	}
}

func newIPDiscoveryBoot(inputs ipDiscoveryBootInputs) *ipDiscoveryBoot {
	b := &ipDiscoveryBoot{inputs: inputs}
	b.component = readiness.NewComponent("ip-discovery", readiness.Spec{Start: b.start})
	return b
}

func (b *ipDiscoveryBoot) output() ipDiscoveryBootOutput {
	return b.result
}

func (b *ipDiscoveryBoot) start(context.Context, readiness.Reporter) error {
	b.result.ipSet = coordinate.NewIPSet()

	discovery, err := ipdiscovery.DiscoverWithTimeout(10*time.Second, b.inputs.log, ipdiscovery.Options{
		NetcheckURL: coordinate.DefaultCloudURL,
	})
	if err != nil {
		b.inputs.log.Warn("failed to discover IPs", "error", err)
	} else {
		for _, addr := range discovery.Addresses {
			ip := net.ParseIP(addr.IP)
			if ip != nil && !ip.IsLinkLocalUnicast() {
				b.result.ipSet.AddDiscoveredFrom(ip, addr.Interface)
			}
		}
		b.inputs.log.Info("discovered IPs", "addresses", len(discovery.Addresses))
	}

	for _, value := range b.inputs.additionalIPs {
		ip := net.ParseIP(value)
		if ip == nil {
			return fmt.Errorf("failed to parse additional IP %s", value)
		}
		b.result.ipSet.AddExplicit(ip)
	}
	return nil
}
