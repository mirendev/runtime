package grunge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/flannel-io/flannel/pkg/backend"
	"github.com/flannel-io/flannel/pkg/ip"
	"github.com/flannel-io/flannel/pkg/ipmatch"
	"github.com/flannel-io/flannel/pkg/lease"
	"github.com/flannel-io/flannel/pkg/subnet"
	fetcd "github.com/flannel-io/flannel/pkg/subnet/etcd"
	"github.com/flannel-io/flannel/pkg/trafficmngr/nftables"
	mvccpb "go.etcd.io/etcd/api/v3/mvccpb"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go4.org/netipx"
	"golang.org/x/sync/errgroup"

	_ "github.com/flannel-io/flannel/pkg/backend/alloc"
	_ "github.com/flannel-io/flannel/pkg/backend/extension"
	_ "github.com/flannel-io/flannel/pkg/backend/hostgw"
	_ "github.com/flannel-io/flannel/pkg/backend/ipip"
	_ "github.com/flannel-io/flannel/pkg/backend/ipsec"
	_ "github.com/flannel-io/flannel/pkg/backend/udp"
	_ "github.com/flannel-io/flannel/pkg/backend/vxlan"
	_ "github.com/flannel-io/flannel/pkg/backend/wireguard"
)

type Network struct {
	NetworkOptions
	log *slog.Logger

	lease *lease.Lease

	ec *clientv3.Client
}

type NetworkOptions struct {
	EtcdEndpoints []string
	EtcdPrefix    string
	Interface     string
	BackendType   string

	PrevIPv4 netip.Prefix
	PrevIPv6 netip.Prefix
}

func NewNetwork(log *slog.Logger, opts NetworkOptions) (*Network, error) {
	ec, err := clientv3.New(clientv3.Config{
		Endpoints: opts.EtcdEndpoints,
	})
	if err != nil {
		return nil, err
	}

	return &Network{
		NetworkOptions: opts,
		log:            log,
		ec:             ec,
	}, nil
}

func (n *Network) SetupConfig(ctx context.Context, v4, v6 netip.Prefix) error {
	var vxlanBackend struct {
		Type string
	}

	vxlanBackend.Type = "vxlan"

	var config subnet.Config
	config.EnableIPv4 = true
	//config.EnableIPv6 = true

	config.Network = ip.FromIPNet(netipx.PrefixIPNet(v4))
	//config.IPv6Network = ip.FromIP6Net(netipx.PrefixIPNet(v6))

	data, err := json.Marshal(vxlanBackend)
	if err != nil {
		n.log.Error("Failed to marshal config", "error", err)
		return err
	}

	config.Backend = data

	cfg, err := json.Marshal(config)
	if err != nil {
		n.log.Error("Failed to marshal config", "error", err)
		return err
	}

	key := path.Join(n.EtcdPrefix, "config")

	n.log.Info("Setting up config", "key", key, "value", string(cfg))

	_, err = n.ec.Put(ctx, key, string(cfg))
	return err
}

type Lease struct {
	lease *lease.Lease
}

func (l *Lease) IPv4() netip.Prefix {
	if l.lease == nil {
		return netip.Prefix{}
	}

	pr, ok := netipx.FromStdIPNet(l.lease.Subnet.ToIPNet())
	if !ok {
		return netip.Prefix{}
	}

	return pr
}

func (l *Lease) IPv6() netip.Prefix {
	if l.lease == nil || l.lease.IPv6Subnet.Empty() {
		return netip.Prefix{}
	}

	pr, ok := netipx.FromStdIPNet(l.lease.IPv6Subnet.ToIPNet())
	if !ok {
		return netip.Prefix{}
	}

	return pr
}

func (n *Network) Lease() *Lease {
	return &Lease{n.lease}
}

func kvToIPLease(kv *mvccpb.KeyValue, ttl int64) (*lease.Lease, error) {
	sn, tsn6 := subnet.ParseSubnetKey(string(kv.Key))
	if sn == nil {
		return nil, fmt.Errorf("failed to parse subnet key %s", kv.Key)
	}

	var sn6 ip.IP6Net
	if tsn6 != nil {
		sn6 = *tsn6
	}

	attrs := &lease.LeaseAttrs{}
	if err := json.Unmarshal([]byte(kv.Value), attrs); err != nil {
		return nil, err
	}

	exp := time.Now().Add(time.Duration(ttl) * time.Second)

	lease := lease.Lease{
		EnableIPv4: true,
		EnableIPv6: !sn6.Empty(),
		Subnet:     *sn,
		IPv6Subnet: sn6,
		Attrs:      *attrs,
		Expiration: exp,
		Asof:       kv.ModRevision,
	}

	return &lease, nil
}

func (n *Network) AllLeases(ctx context.Context) ([]lease.Lease, error) {
	key := path.Join(n.EtcdPrefix, "subnets")
	resp, err := n.ec.Get(ctx, key, clientv3.WithPrefix())
	if err != nil {
		if err == rpctypes.ErrGRPCKeyNotFound {
			// key not found: treat it as empty set
			return []lease.Lease{}, nil
		}
		return nil, err
	}

	leases := []lease.Lease{}
	for _, kv := range resp.Kvs {
		ttlresp, err := n.ec.TimeToLive(ctx, clientv3.LeaseID(kv.Lease))
		if err != nil {
			continue
		}
		l, err := kvToIPLease(kv, ttlresp.TTL)
		if err != nil {
			continue
		}

		leases = append(leases, *l)
	}

	return leases, nil
}

func (n *Network) Start(ctx context.Context, eg *errgroup.Group) error {
	cfg := &fetcd.EtcdConfig{
		Endpoints: n.EtcdEndpoints,
		Prefix:    n.EtcdPrefix,
	}

	var (
		prevSubnet     ip.IP4Net
		prevIPv6Subnet ip.IP6Net
	)

	if n.PrevIPv4.IsValid() {
		prevSubnet = ip.FromIPNet(netipx.PrefixIPNet(n.PrevIPv4))
	}

	if n.PrevIPv6.IsValid() {
		prevIPv6Subnet = ip.FromIP6Net(netipx.PrefixIPNet(n.PrevIPv6))
	}

	sm, err := fetcd.NewLocalManager(ctx, cfg, prevSubnet, prevIPv6Subnet, 60)
	if err != nil {
		n.log.Error("Failed to create subnet manager", "error", err)
		return err
	}

	config, err := sm.GetNetworkConfig(ctx)
	if err != nil {
		n.log.Error("Failed to get network config", "error", err)
		return err
	}

	v6iface, _ := ip.GetDefaultV6GatewayInterface()

	extIface, err := n.extIface(ctx, sm, v6iface != nil)
	if err != nil {
		n.log.Error("Failed to get external interface", "error", err)
		return err
	}

	bm := backend.NewManager(ctx, sm, extIface)
	be, err := bm.GetBackend("vxlan")
	if err != nil {
		n.log.Error("Failed to get backend", "error", err)
		return err
	}

	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(ctx)

	bn, err := be.RegisterNetwork(ctx, &wg, config)
	if err != nil {
		cancel()
		n.log.Error("Failed to register network", "error", err)
		return err
	}

	//Create TrafficManager and instantiate it based on whether we use iptables or nftables
	trafficMngr := &nftables.NFTablesManager{}
	err = trafficMngr.Init(ctx, &wg)
	if err != nil {
		cancel()
		n.log.Error("Failed to initialize traffic manager", "error", err)
		return err
	}

	n.lease = bn.Lease()

	eg.Go(func() error {
		defer cancel()
		// Start "Running" the backend network. This will block until the context is done so run in another goroutine.
		n.log.Info("Running backend.")
		wg.Add(1)
		go func() {
			bn.Run(ctx)
			wg.Done()
		}()

		err = sm.CompleteLease(ctx, bn.Lease(), &wg)
		if err != nil {
			n.log.Error("CompleteLease execute error err", "error", err)
			if strings.EqualFold(err.Error(), "interrupted") {
				// The lease was "revoked" - shut everything down
				cancel()
			}
		}

		// Block waiting for all the goroutines to finish.
		wg.Wait()

		return err
	})

	return nil
}

func (n *Network) extIface(ctx context.Context, sm subnet.Manager, v6 bool) (*backend.ExternalInterface, error) {
	ipStack, stackErr := ipmatch.GetIPFamily(true, v6)
	if stackErr != nil {
		n.log.Error("Failed to get IP stack", "error", stackErr)
		return nil, stackErr
	}

	var extIface *backend.ExternalInterface

	annotatedPublicIP, annotatedPublicIPv6 := sm.GetStoredPublicIP(ctx)

	optsPublicIP := ipmatch.PublicIPOpts{
		PublicIP:   annotatedPublicIP,
		PublicIPv6: annotatedPublicIPv6,
	}

	var err error

	// Check the default interface only if no interfaces are specified
	if n.Interface == "" {
		if annotatedPublicIP != "" {
			extIface, err = ipmatch.LookupExtIface(annotatedPublicIP, "", "", ipStack, optsPublicIP)
		} else {
			extIface, err = ipmatch.LookupExtIface(annotatedPublicIPv6, "", "", ipStack, optsPublicIP)
		}

		if err != nil {
			return nil, fmt.Errorf("Failed to find interface matching %s: %s", n.Interface, err)
		}
	} else {
		// Check explicitly specified interfaces
		extIface, err = ipmatch.LookupExtIface(n.Interface, "", "", ipStack, optsPublicIP)
		if err != nil {
			return nil, fmt.Errorf("Failed to find interface matching %s: %s", n.Interface, err)
		}

		if extIface == nil {
			return nil, fmt.Errorf("Failed to find interface matching %s", n.Interface)
		}
	}

	return extIface, nil
}
