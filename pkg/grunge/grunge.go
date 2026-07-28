package grunge

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/flannel-io/flannel/pkg/backend"
	"github.com/flannel-io/flannel/pkg/ip"
	"github.com/flannel-io/flannel/pkg/ipmatch"
	"github.com/flannel-io/flannel/pkg/lease"
	"github.com/flannel-io/flannel/pkg/subnet"
	fetcd "github.com/flannel-io/flannel/pkg/subnet/etcd"
	"github.com/flannel-io/flannel/pkg/trafficmngr/nftables"
	"github.com/vishvananda/netlink"
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

// errNoBackend is returned rather than silently falling back to a default.
// The backend decides whether sandbox traffic between hosts is encrypted, so
// guessing here can hand an operator plaintext VXLAN on a cluster whose config
// asked for WireGuard, with nothing in the logs to say so. Runners carry the
// backend in the config written at join time; one that predates the field has
// to re-join to pick it up.
var errNoBackend = fmt.Errorf(
	"no network backend configured: set server.network_backend on the coordinator, " +
		"and re-join any runner whose config predates the setting")

// staleBackendDevices lists, per backend, the interfaces left behind by the
// other backends we support. Switching backends does not remove the previous
// backend's device, and its per-lease routes (one /24 per peer) are more
// specific than the new backend's route for the whole /16, so the kernel keeps
// steering remote sandbox traffic into the old device. Leftover VXLAN is the
// dangerous direction: the node reports WireGuard, logs that it is ignoring
// non-WireGuard leases, and still ships that peer's traffic in the clear.
var staleBackendDevices = map[string][]string{
	"vxlan":     {"flannel-wg", "flannel-wg-v6"},
	"wireguard": {"flannel.1"},
}

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

	// TLS configuration for etcd mTLS (optional, file paths)
	TLSCertFile string // Client certificate file path
	TLSKeyFile  string // Client private key file path
	TLSCAFile   string // CA certificate file path
}

func NewNetwork(log *slog.Logger, opts NetworkOptions) (*Network, error) {
	etcdConfig := clientv3.Config{
		Endpoints: opts.EtcdEndpoints,
	}

	// Configure TLS if certificate files are provided
	if opts.TLSCertFile != "" && opts.TLSKeyFile != "" && opts.TLSCAFile != "" {
		tlsConfig, err := buildTLSConfigFromFiles(opts.TLSCertFile, opts.TLSKeyFile, opts.TLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to build etcd TLS config: %w", err)
		}
		etcdConfig.TLS = tlsConfig
		log.Info("grunge using mTLS for etcd connection")
	}

	ec, err := clientv3.New(etcdConfig)
	if err != nil {
		return nil, err
	}

	return &Network{
		NetworkOptions: opts,
		log:            log,
		ec:             ec,
	}, nil
}

// buildTLSConfigFromFiles creates a tls.Config by reading PEM files from disk.
func buildTLSConfigFromFiles(certFile, keyFile, caFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificate: %w", err)
	}

	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
	}, nil
}

func (n *Network) SetupConfig(ctx context.Context, v4, v6 netip.Prefix) error {
	if n.BackendType == "" {
		return errNoBackend
	}

	var backend struct {
		Type string
	}
	backend.Type = n.BackendType

	var config subnet.Config
	config.EnableIPv4 = true
	//config.EnableIPv6 = true

	config.Network = ip.FromIPNet(netipx.PrefixIPNet(v4))
	//config.IPv6Network = ip.FromIP6Net(netipx.PrefixIPNet(v6))

	data, err := json.Marshal(backend)
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

// removeStaleBackendDevices deletes interfaces belonging to a backend other
// than the one about to register. Deleting a link takes its routes with it.
// A device we cannot remove is fatal rather than logged: continuing would mean
// running as if encrypted while a stale VXLAN path is still winning the route
// lookup, which is precisely the failure this is here to prevent.
func (n *Network) removeStaleBackendDevices(backendType string) error {
	for _, name := range staleBackendDevices[backendType] {
		if err := n.removeDeviceIfPresent(name, backendType); err != nil {
			return err
		}
	}

	return nil
}

// removeDeviceIfPresent deletes one interface, treating absence as success.
func (n *Network) removeDeviceIfPresent(name, backendType string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		// Absent is the expected case and the whole point. Any other lookup
		// failure means we cannot tell whether a stale device is there, which
		// is not a question to answer optimistically.
		var notFound netlink.LinkNotFoundError
		if errors.As(err, &notFound) {
			return nil
		}

		return fmt.Errorf("failed to look up %s while cleaning up after a previous backend: %w", name, err)
	}

	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("failed to remove stale %s device left by a previous backend: %w", name, err)
	}

	n.log.Info("removed stale network device from a previous backend",
		"device", name, "backend", backendType)

	return nil
}

func (n *Network) Start(ctx context.Context, eg *errgroup.Group) error {
	cfg := &fetcd.EtcdConfig{
		Endpoints: n.EtcdEndpoints,
		Prefix:    n.EtcdPrefix,
	}

	if n.TLSCertFile != "" && n.TLSKeyFile != "" && n.TLSCAFile != "" {
		cfg.Certfile = n.TLSCertFile
		cfg.Keyfile = n.TLSKeyFile
		cfg.CAFile = n.TLSCAFile
		n.log.Info("flannel subnet manager using mTLS for etcd connection")
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

	if n.BackendType == "" {
		return errNoBackend
	}
	backendType := n.BackendType

	if err := n.removeStaleBackendDevices(backendType); err != nil {
		return err
	}

	be, err := bm.GetBackend(backendType)
	if err != nil {
		n.log.Error("Failed to get backend", "backend", backendType, "error", err)
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
			return nil, fmt.Errorf("failed to find interface matching %s: %s", n.Interface, err)
		}
	} else {
		// Check explicitly specified interfaces
		extIface, err = ipmatch.LookupExtIface(n.Interface, "", "", ipStack, optsPublicIP)
		if err != nil {
			return nil, fmt.Errorf("failed to find interface matching %s: %s", n.Interface, err)
		}

		if extIface == nil {
			return nil, fmt.Errorf("failed to find interface matching %s", n.Interface)
		}
	}

	return extIface, nil
}
