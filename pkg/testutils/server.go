package testutils

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/ipalloc"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/components/ocireg"
	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/components/scheduler"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/netdb"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/slogfmt"
	"miren.dev/runtime/servers/httpingress"
)

// TestServer spins up an equivalent of the dev server for testing purposes.
func TestServer(t *testing.T) error {
	ctx := t.Context()
	// Create a cancellable context
	ctx, ctxCancel := context.WithCancel(ctx)
	eg, sub := errgroup.WithContext(ctx)
	reg, cleanup := Registry(observability.TestInject)
	t.Cleanup(cleanup)

	// Mirroring defaults from cli/commands/dev.go
	optsAddress := "localhost:8443"
	optsRunnerAddress := "localhost:8444"
	optsEtcdEndpoints := []string{"http://etcd:2379"}
	optsEtcdPrefix := "/miren"
	optsRunnerId := "dev"

	log := slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))

	res, hm := netresolve.NewLocalResolver()

	var (
		cpu metrics.CPUUsage
		mem metrics.MemoryUsage
	)

	err := reg.Populate(&mem)
	if err != nil {
		log.Error("failed to populate memory usage", "error", err)
		ctxCancel()
		return err
	}

	err = reg.Populate(&cpu)
	if err != nil {
		log.Error("failed to populate CPU usage", "error", err)
		ctxCancel()
		return err
	}

	co := coordinate.NewCoordinator(log, coordinate.CoordinatorConfig{
		Address:       optsAddress,
		EtcdEndpoints: optsEtcdEndpoints,
		Prefix:        optsEtcdPrefix,
		Resolver:      res,
		TempDir:       os.TempDir(),
		Mem:           &mem,
		Cpu:           &cpu,
	})

	t.Log("Starting coordinator")
	err = co.Start(sub)
	if err != nil {
		log.Error("failed to start coordinator", "error", err)
		ctxCancel()
		return err
	}

	time.Sleep(time.Second)

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

	// Setup the core services on the coordinator address for dev

	/*
		{
			serv := co.Server()

			var builder build.RPCBuilder

			err = reg.Populate(&builder)
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
		log.Error("failed to create RPC client", "error", err)
		ctxCancel()
		return err
	}

	client, err := rs.Connect(optsAddress, "entities")
	if err != nil {
		log.Error("failed to connect to RPC server", "error", err)
		ctxCancel()
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	ec := entityserver.NewClient(log, eac)

	reg.Register("hl-entity-client", ec)

	var subnets []netip.Prefix
	reg.Resolve(&subnets)

	ipa := ipalloc.NewAllocator(log, subnets)
	eg.Go(func() error {
		defer t.Log("ipa escaped")
		return ipa.Watch(ctx, eac)
	})

	aa := co.Activator()

	hs := httpingress.NewServer(ctx, log, eac, aa)

	reg.Register("app-activator", aa)
	reg.Register("resolver", res)

	r := runner.NewRunner(log, reg, runner.RunnerConfig{
		Id:            optsRunnerId,
		ServerAddress: optsAddress,
		ListenAddress: optsRunnerAddress,
		Workers:       1,
	})

	err = r.Start(sub)
	if err != nil {
		ctxCancel()
		return err
	}

	sch, err := scheduler.NewScheduler(sub, log, eac)
	if err != nil {
		log.Error("failed to create scheduler", "error", err)
		ctxCancel()
		return err
	}

	eg.Go(func() error {
		defer t.Log("ipa escaped")
		return sch.Watch(ctx, eac)
	})

	httpServer := &http.Server{
		Addr:    ":8989",
		Handler: hs,
	}

	go func() {
		err := httpServer.ListenAndServe()
		if err == http.ErrServerClosed {
			log.Info("ingress server closed")
		}
		if err != nil && err != http.ErrServerClosed {
			log.Error("failed to start HTTP server", "error", err)
		}
	}()

	// Register cleanup function to shutdown the HTTP server
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Error("failed to shutdown HTTP server", "error", err)
		}
		log.Info("HTTP server shutdown complete")
	})

	var ociRegistry ocireg.Registry
	err = reg.Populate(&ociRegistry)
	if err != nil {
		log.Error("failed to populate OCI registry", "error", err)
		ctxCancel()
		return err
	}

	// Start the OCI Registry with a proper server
	ociServer := &http.Server{
		Addr: ":5000",
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		err := ociRegistry.Start(ctx, ":5000")
		if err == http.ErrServerClosed {
			log.Info("OCI registry closed")
		}
		if err != nil && err != http.ErrServerClosed {
			log.Error("failed to start OCI registry", "error", err)
		}
	}()

	// Register cleanup for OCI registry server
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := ociServer.Shutdown(shutdownCtx); err != nil {
			log.Error("failed to shutdown OCI registry server", "error", err)
		}
		log.Info("OCI registry server shutdown complete")
	})

	var regAddr netip.Addr

	err = reg.ResolveNamed(&regAddr, "router-address")
	if err != nil {
		log.Error("failed to resolve router address", "error", err)
		ctxCancel()
		return err
	}

	log.Info("OCI registry URL", "url", regAddr)

	hm.SetHost("cluster.local", regAddr)

	log.Info("Starting test mode", "address", optsAddress, "etcd_endpoints", optsEtcdEndpoints, "etcd_prefix", optsEtcdPrefix, "runner_id", optsRunnerId)

	// Register cleanup for running components
	t.Cleanup(func() {
		// The runner will be stopped by the context cancellation
		log.Info("Runner will be stopped via context cancellation")

		// TODO: Close any RPC connections, currently hangs
		// if client != nil {
		// 	log.Info("Closing RPC client connections")
		// 	client.Close()
		// }

		// Cancel context to signal all components to stop
		log.Info("Canceling context to stop all components")
		ctxCancel()

		// TODO: Graceful shutdown
		// log.Info("Waiting for errgroup to complete")
		// Wait for errgroup to finish
		// if err := eg.Wait(); err != nil {
		// 	log.Error("error waiting for components to stop", "error", err)
		// }

		log.Info("Test server shutdown complete")
	})

	// Let the server run until the test ends
	return nil
}
