//go:build linux
// +build linux

package commands

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/davecgh/go-spew/spew"
	"golang.org/x/sync/errgroup"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/components/autotls"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/etcd"
	"miren.dev/runtime/components/ipalloc"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/components/ocireg"
	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/components/scheduler"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/grunge"
	"miren.dev/runtime/pkg/netdb"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/servers/httpingress"
)

func Dev(ctx *Context, opts struct {
	Address         string   `short:"a" long:"address" description:"Address to listen on" default:"localhost:8443"`
	RunnerAddress   string   `long:"runner-address" description:"Address to listen on" default:"localhost:8444"`
	EtcdEndpoints   []string `short:"e" long:"etcd" description:"Etcd endpoints" default:"http://etcd:2379"`
	EtcdPrefix      string   `short:"p" long:"etcd-prefix" description:"Etcd prefix" default:"/miren"`
	RunnerId        string   `short:"r" long:"runner-id" description:"Runner ID" default:"dev"`
	DataPath        string   `short:"d" long:"data-path" description:"Data path" default:"/var/lib/miren"`
	AdditionalNames []string `long:"dns-names" description:"Additional DNS names assigned to the server cert"`
	AdditionalIPs   []string `long:"ips" description:"Additional IPs assigned to the server cert"`
	StandardTLS     bool     `long:"serve-tls" description:"Expose the http ingress on standard TLS ports"`
	StartEtcd       bool     `long:"start-etcd" description:"Start embedded etcd server"`
	EtcdClientPort  int      `long:"etcd-client-port" description:"Etcd client port" default:"12379"`
	EtcdPeerPort    int      `long:"etcd-peer-port" description:"Etcd peer port" default:"12380"`
}) error {
	eg, sub := errgroup.WithContext(ctx)

	res, hm := netresolve.NewLocalResolver()

	var (
		cpu metrics.CPUUsage
		mem metrics.MemoryUsage
	)

	err := ctx.Server.Populate(&mem)
	if err != nil {
		ctx.Log.Error("failed to populate memory usage", "error", err)
		return err
	}

	err = ctx.Server.Populate(&cpu)
	if err != nil {
		ctx.Log.Error("failed to populate CPU usage", "error", err)
		return err
	}

	var additionalIps []net.IP
	for _, ip := range opts.AdditionalIPs {
		addr := net.ParseIP(ip)
		if addr == nil {
			ctx.Log.Error("failed to parse additional IP", "ip", ip)
			return fmt.Errorf("failed to parse additional IP %s", ip)
		}
		additionalIps = append(additionalIps, addr)
	}

	// Start embedded etcd server if requested
	if opts.StartEtcd {
		ctx.Log.Info("starting embedded etcd server", "client-port", opts.EtcdClientPort, "peer-port", opts.EtcdPeerPort)

		// Get containerd client from registry
		var cc *containerd.Client
		err := ctx.Server.Resolve(&cc)
		if err != nil {
			ctx.Log.Error("failed to get containerd client for etcd", "error", err)
			return err
		}

		// TODO figure out why I can't use ResolveNamed to pull out the namespace from ctx.Server
		etcdComponent := etcd.NewEtcdComponent(ctx.Log, cc, "runtime", opts.DataPath)

		etcdConfig := etcd.EtcdConfig{
			Name:         "dev-etcd",
			ClientPort:   opts.EtcdClientPort,
			PeerPort:     opts.EtcdPeerPort,
			InitialToken: "dev-cluster",
			ClusterState: "new",
		}

		err = etcdComponent.Start(sub, etcdConfig)
		if err != nil {
			ctx.Log.Error("failed to start etcd component", "error", err)
			return err
		}

		// Update etcd endpoints to use local etcd
		opts.EtcdEndpoints = []string{etcdComponent.ClientEndpoint()}
		ctx.Log.Info("using embedded etcd", "endpoint", etcdComponent.ClientEndpoint())

		// Ensure cleanup on exit
		defer func() {
			etcdComponent.Stop(ctx.Context)
		}()
	}

	co := coordinate.NewCoordinator(ctx.Log, coordinate.CoordinatorConfig{
		Address:         opts.Address,
		EtcdEndpoints:   opts.EtcdEndpoints,
		Prefix:          opts.EtcdPrefix,
		DataPath:        opts.DataPath,
		AdditionalNames: opts.AdditionalNames,
		AdditionalIPs:   additionalIps,
		Resolver:        res,
		TempDir:         os.TempDir(),
		Mem:             &mem,
		Cpu:             &cpu,
	})

	err = co.Start(sub)
	if err != nil {
		ctx.Log.Error("failed to start coordinator", "error", err)
		return err
	}

	time.Sleep(time.Second)

	subnets := []netip.Prefix{
		netip.MustParsePrefix("10.10.0.0/16"),
		netip.MustParsePrefix("fd47:cafe:d00d::/64"),
	}

	reg := ctx.Server

	ctx.Server.Register("service-prefixes", subnets)

	gn, err := grunge.NewNetwork(ctx.Log, grunge.NetworkOptions{
		EtcdEndpoints: opts.EtcdEndpoints,
		EtcdPrefix:    opts.EtcdPrefix + "/sub/flannel",
	})
	if err != nil {
		ctx.Log.Error("failed to create grunge network", "error", err)
		return err
	}

	err = gn.SetupConfig(ctx,
		netip.MustParsePrefix("10.8.0.0/16"),
		netip.MustParsePrefix("fd47:ace::/64"),
	)
	if err != nil {
		ctx.Log.Error("failed to setup grunge network", "error", err)
		return err
	}

	err = gn.Start(sub, eg)
	if err != nil {
		ctx.Log.Error("failed to start grunge network", "error", err)
		return err
	}

	lease := gn.Lease()

	ctx.Log.Info("leased IP prefixes", "ipv4", lease.IPv4().String(), "ipv6", lease.IPv6().String())

	leases, err := gn.AllLeases(ctx)
	if err != nil {
		ctx.Log.Error("failed to get all leases", "error", err)
		return err
	}

	ctx.Log.Info("cluster leases", "leasees", spew.Sdump(leases))

	reg.Register("ip4-routable", lease.IPv4())

	reg.ProvideName("subnet", func(opts struct {
		Dir    string       `asm:"data-path"`
		Id     string       `asm:"server-id"`
		Prefix netip.Prefix `asm:"ip4-routable"`
	}) (*netdb.Subnet, error) {
		os.MkdirAll(opts.Dir, 0755)
		ndb, err := netdb.New(filepath.Join(opts.Dir, "net.db"))
		if err != nil {
			return nil, fmt.Errorf("failed to open netdb: %w", err)
		}

		sub, err := ndb.Subnet(opts.Prefix.String())
		if err != nil {
			return nil, err
		}

		return sub, nil
	})

	reg.ProvideName("router-address", func(opts struct {
		Sub *netdb.Subnet `asm:"subnet"`
	}) (netip.Addr, error) {
		return opts.Sub.Router().Addr(), nil
	})

	// Run the runner!

	scfg, err := co.ServiceConfig()
	if err != nil {
		ctx.Log.Error("failed to get service config", "error", err)
		return err
	}

	// Create RPC client to interact with coordinator
	rs, err := scfg.State(ctx, rpc.WithLogger(ctx.Log))
	if err != nil {
		ctx.Log.Error("failed to create RPC client", "error", err)
		return err
	}

	client, err := rs.Connect(opts.Address, "entities")
	if err != nil {
		ctx.Log.Error("failed to connect to RPC server", "error", err)
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	ec := entityserver.NewClient(ctx.Log, eac)

	reg.Register("hl-entity-client", ec)

	ipa := ipalloc.NewAllocator(ctx.Log, subnets)
	eg.Go(func() error {
		return ipa.Watch(sub, eac)
	})

	aa := co.Activator()

	hs := httpingress.NewServer(ctx, ctx.Log, eac, aa)

	reg.Register("app-activator", aa)
	reg.Register("resolver", res)

	rcfg, err := co.NamedConfig("runner")
	if err != nil {
		return err
	}

	r := runner.NewRunner(ctx.Log, ctx.Server, runner.RunnerConfig{
		Id:            opts.RunnerId,
		ServerAddress: opts.Address,
		ListenAddress: opts.RunnerAddress,
		Workers:       runner.DefaulWorkers,
		Config:        rcfg,
	})

	err = r.Start(sub)
	if err != nil {
		return err
	}

	defer r.Close()

	sch, err := scheduler.NewScheduler(sub, ctx.Log, eac)
	if err != nil {
		ctx.Log.Error("failed to create scheduler", "error", err)
		return err
	}

	eg.Go(func() error {
		return sch.Watch(sub, eac)
	})

	go func() {
		err := http.ListenAndServe(":8989", hs)
		if err != nil {
			ctx.Log.Error("failed to start HTTP server", "error", err)
		}
	}()

	if opts.StandardTLS {
		autotls.ServeTLS(sub, ctx.Log, opts.DataPath, hs)
	}

	var registry ocireg.Registry
	err = reg.Populate(&registry)
	if err != nil {
		ctx.Log.Error("failed to populate OCI registry", "error", err)
		return err
	}

	registry.Start(ctx, ":5000")

	var regAddr netip.Addr

	err = reg.ResolveNamed(&regAddr, "router-address")
	if err != nil {
		ctx.Log.Error("failed to resolve router address", "error", err)
		return err
	}

	ctx.Log.Info("OCI registry URL", "url", regAddr)

	hm.SetHost("cluster.local", regAddr)

	ctx.Log.Info("Starting dev mode", "address", opts.Address, "etcd_endpoints", opts.EtcdEndpoints, "etcd_prefix", opts.EtcdPrefix, "runner_id", opts.RunnerId)

	return eg.Wait()
}
