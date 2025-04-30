//go:build linux
// +build linux

package commands

import (
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	"github.com/davecgh/go-spew/spew"
	"golang.org/x/sync/errgroup"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/ipalloc"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/components/ocireg"
	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/components/scheduler"
	"miren.dev/runtime/pkg/grunge"
	"miren.dev/runtime/pkg/netdb"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/servers/httpingress"
)

func Dev(ctx *Context, opts struct {
	Address       string   `short:"a" long:"address" description:"Address to listen on" default:"localhost:8443"`
	RunnerAddress string   `long:"runner-address" description:"Address to listen on" default:"localhost:8444"`
	EtcdEndpoints []string `short:"e" long:"etcd" description:"Etcd endpoints" default:"http://etcd:2379"`
	EtcdPrefix    string   `short:"p" long:"etcd-prefix" description:"Etcd prefix" default:"/miren"`
	RunnerId      string   `short:"r" long:"runner-id" description:"Runner ID" default:"dev"`
}) error {
	eg, sub := errgroup.WithContext(ctx)

	res, hm := netresolve.NewLocalResolver()

	co := coordinate.NewCoordinator(ctx.Log, coordinate.CoordinatorConfig{
		Address:       opts.Address,
		EtcdEndpoints: opts.EtcdEndpoints,
		Prefix:        opts.EtcdPrefix,
		Resolver:      res,
		TempDir:       os.TempDir(),
	})

	err := co.Start(sub)
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

	err = gn.Start(sub)
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

	// Setup the core services on the coordinator address for dev

	/*
		{
			serv := co.Server()

			var builder build.RPCBuilder

			err = ctx.Server.Populate(&builder)
			if err != nil {
				return fmt.Errorf("populating build: %w", err)
			}

			serv.ExposeValue("dev.miren.runtime/build", build.AdaptBuilder(&builder))
		}
	*/

	// Run the runner!

	// Create RPC client to interact with coordinator
	rs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
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

	ipa := ipalloc.NewAllocator(ctx.Log, subnets)
	eg.Go(func() error {
		return ipa.Watch(sub, eac)
	})

	aa := co.Activator()

	hs := httpingress.NewServer(ctx, ctx.Log, eac, aa)

	reg.Register("app-activator", aa)
	reg.Register("resolver", res)

	r := runner.NewRunner(ctx.Log, ctx.Server, runner.RunnerConfig{
		Id:            opts.RunnerId,
		ServerAddress: opts.Address,
		ListenAddress: opts.RunnerAddress,
		Workers:       runner.DefaulWorkers,
	})

	err = r.Start(sub)
	if err != nil {
		return err
	}

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

	regAddr, err := ocireg.SetupReg(ctx, ctx.Log, eac)
	if err != nil {
		ctx.Log.Error("failed to setup OCI registry", "error", err)
		return err
	}

	ctx.Log.Info("OCI registry URL", "url", regAddr)

	hm.SetHost("cluster.local", regAddr)

	ctx.Log.Info("Starting dev mode", "address", opts.Address, "etcd_endpoints", opts.EtcdEndpoints, "etcd_prefix", opts.EtcdPrefix, "runner_id", opts.RunnerId)

	return eg.Wait()
}
