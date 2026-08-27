//go:build linux

package server

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"

	"golang.org/x/sync/errgroup"
	"miren.dev/runtime/pkg/grunge"
	"miren.dev/runtime/pkg/netdb"
	"miren.dev/runtime/pkg/readiness"
)

type networkBootInputs struct {
	dataPath   string
	etcdPrefix string
	group      *errgroup.Group
}

type networkBootOutput struct {
	subnet        *netdb.Subnet
	routerAddress netip.Addr
	ipv4Routable  netip.Prefix
}

type networkBoot struct {
	component     *readiness.Component
	inputs        networkBootInputs
	etcd          *etcdBoot
	observability *observabilityBoot
	result        networkBootOutput
}

func networkInputs(options StartOptions) networkBootInputs {
	return networkBootInputs{
		dataPath:   options.Config.Server.GetDataPath(),
		etcdPrefix: options.Config.Etcd.GetPrefix() + "/sub/flannel",
		group:      options.Group,
	}
}

func newNetworkBoot(inputs networkBootInputs, etcd *etcdBoot, observability *observabilityBoot) *networkBoot {
	b := &networkBoot{inputs: inputs, etcd: etcd, observability: observability}
	b.component = readiness.NewComponent("network", readiness.Spec{
		Dependencies: []readiness.Dependency{
			readiness.ReadyDep(etcd.component),
			readiness.ReadyDep(observability.component),
		},
		Start: b.start,
	})
	return b
}

func (b *networkBoot) output() networkBootOutput {
	return b.result
}

func (b *networkBoot) start(ctx context.Context, _ readiness.Reporter) error {
	if err := os.MkdirAll(b.inputs.dataPath, 0755); err != nil {
		return fmt.Errorf("creating data directory %s: %w", b.inputs.dataPath, err)
	}
	database, err := netdb.New(filepath.Join(b.inputs.dataPath, "net.db"))
	if err != nil {
		return fmt.Errorf("opening netdb: %w", err)
	}
	previousSubnet, err := database.GetLeasedSubnet()
	if err != nil {
		return fmt.Errorf("reading leased subnet from netdb: %w", err)
	}

	etcd := b.etcd.output()
	config := grunge.NetworkOptions{
		EtcdEndpoints: etcd.endpoints,
		EtcdPrefix:    b.inputs.etcdPrefix,
		PrevIPv4:      previousSubnet,
	}
	if etcd.tls != nil {
		config.TLSCertFile = etcd.tls.ClientCertFile
		config.TLSKeyFile = etcd.tls.ClientKeyFile
		config.TLSCAFile = etcd.tls.CAFile
	}

	network, err := grunge.NewNetwork(b.observability.output().log, config)
	if err != nil {
		return fmt.Errorf("creating grunge network: %w", err)
	}
	if err := network.SetupConfig(ctx,
		netip.MustParsePrefix("10.8.0.0/16"),
		netip.MustParsePrefix("fd47:ace::/64"),
	); err != nil {
		return fmt.Errorf("setting up grunge network: %w", err)
	}
	if err := network.Start(ctx, b.inputs.group); err != nil {
		return fmt.Errorf("starting grunge network: %w", err)
	}

	lease := network.Lease()
	b.result.ipv4Routable = lease.IPv4()
	b.observability.output().log.Info("leased IP prefixes", "ipv4", lease.IPv4().String(), "ipv6", lease.IPv6().String())
	if err := database.SetLeasedSubnet(lease.IPv4()); err != nil {
		return fmt.Errorf("persisting leased subnet: %w", err)
	}
	b.result.subnet, err = database.Subnet(lease.IPv4().String())
	if err != nil {
		return fmt.Errorf("creating subnet: %w", err)
	}
	b.result.routerAddress = b.result.subnet.Router().Addr()
	return nil
}
