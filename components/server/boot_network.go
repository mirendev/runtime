//go:build linux

package server

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"

	"golang.org/x/sync/errgroup"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/grunge"
	"miren.dev/runtime/pkg/netdb"
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
	component *boot.Component
	inputs    networkBootInputs
	database  *netdb.NetDB
	output    boot.Output[networkBootOutput]
}

func networkInputs(options StartOptions) networkBootInputs {
	return networkBootInputs{
		dataPath:   options.Config.Server.GetDataPath(),
		etcdPrefix: options.Config.Etcd.GetPrefix() + "/sub/flannel",
		group:      options.Group,
	}
}

func newNetworkBoot(inputs networkBootInputs, etcd boot.Output[etcdBootOutput], observability boot.Output[observabilityBootOutput]) *networkBoot {
	b := &networkBoot{inputs: inputs}
	b.component, b.output = boot.Provide2("network", etcd, observability, b.start,
		boot.WithStop(b.stop, componentStopTimeout))
	return b
}

func (b *networkBoot) start(ctx context.Context, etcd etcdBootOutput, observability observabilityBootOutput) (_ networkBootOutput, retErr error) {
	if err := os.MkdirAll(b.inputs.dataPath, 0755); err != nil {
		return networkBootOutput{}, fmt.Errorf("creating data directory %s: %w", b.inputs.dataPath, err)
	}
	database, err := netdb.New(filepath.Join(b.inputs.dataPath, "net.db"))
	if err != nil {
		return networkBootOutput{}, fmt.Errorf("opening netdb: %w", err)
	}
	b.database = database
	defer func() {
		if retErr == nil {
			return
		}
		retErr = errors.Join(retErr, b.closeDatabase())
	}()
	previousSubnet, err := database.GetLeasedSubnet()
	if err != nil {
		return networkBootOutput{}, fmt.Errorf("reading leased subnet from netdb: %w", err)
	}

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

	log := observability.log
	network, err := grunge.NewNetwork(log, config)
	if err != nil {
		return networkBootOutput{}, fmt.Errorf("creating grunge network: %w", err)
	}
	if err := network.SetupConfig(ctx,
		netip.MustParsePrefix("10.8.0.0/16"),
		netip.MustParsePrefix("fd47:ace::/64"),
	); err != nil {
		return networkBootOutput{}, fmt.Errorf("setting up grunge network: %w", err)
	}
	if err := network.Start(ctx, b.inputs.group); err != nil {
		return networkBootOutput{}, fmt.Errorf("starting grunge network: %w", err)
	}

	lease := network.Lease()
	result := networkBootOutput{ipv4Routable: lease.IPv4()}
	log.Info("leased IP prefixes", "ipv4", lease.IPv4().String(), "ipv6", lease.IPv6().String())
	if err := database.SetLeasedSubnet(lease.IPv4()); err != nil {
		return networkBootOutput{}, fmt.Errorf("persisting leased subnet: %w", err)
	}
	result.subnet, err = database.Subnet(lease.IPv4().String())
	if err != nil {
		return networkBootOutput{}, fmt.Errorf("creating subnet: %w", err)
	}
	result.routerAddress = result.subnet.Router().Addr()
	return result, nil
}

func (b *networkBoot) stop(context.Context) error {
	return b.closeDatabase()
}

func (b *networkBoot) closeDatabase() error {
	if b.database == nil {
		return nil
	}
	err := b.database.Close()
	b.database = nil
	if err != nil {
		return fmt.Errorf("closing netdb: %w", err)
	}
	return nil
}

func serviceNetworkPrefixes() []netip.Prefix {
	return []netip.Prefix{
		netip.MustParsePrefix("10.10.0.0/16"),
		netip.MustParsePrefix("fd47:cafe:d00d::/64"),
	}
}
