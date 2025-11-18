package testserver

import (
	"context"
	"fmt"
	"log/slog"
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
	"miren.dev/runtime/pkg/testutils"
	"miren.dev/runtime/servers/httpingress"
)

func TestServerConfig(t *testing.T) (string, error) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(path, []byte(`
active_cluster: local
clusters:
	local:
		hostname: localhost:8443
		insecure: true
`), 0644)
	if err != nil {
		return "", err
	}

	return path, nil
}

// TestServer spins up an equivalent of the dev server for testing purposes.
func TestServer(t *testing.T) error {
	ctx := t.Context()
	// Create a cancellable context
	ctx, ctxCancel := context.WithCancel(ctx)
	eg, sub := errgroup.WithContext(ctx)
	reg, cleanup := testutils.Registry()
	t.Cleanup(cleanup)

	// Don't use observability.TestInject because it injects a LogWriter that
	// is a no-op. The registry appears to have a bug where it isn't always picking
	// the same implementation, and sometimes it picks the DebugLogWriter and the test
	// fails because of that.

	reg.Provide(func(opts struct {
		Log *slog.Logger
	}) *observability.StatusMonitor {
		log := opts.Log
		if log == nil {
			log = slog.Default()
		}
		return &observability.StatusMonitor{
			Log: log,
			//entities: make(map[string]*EntityStatus),
		}
	})

	// Mirroring defaults from cli/commands/dev.go
	optsAddress := "localhost:8443"
	optsRunnerAddress := "localhost:8444"
	optsEtcdEndpoints := []string{"http://etcd:2379"}
	var optsEtcdPrefix string
	reg.ResolveNamed(&optsEtcdPrefix, "etcd-prefix")
	optsRunnerId := "dev"

	log := slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))

	res, hm := netresolve.NewLocalResolver()

	var (
		cpu  metrics.CPUUsage
		mem  metrics.MemoryUsage
		logs observability.LogReader
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

	err = reg.Populate(&logs)
	if err != nil {
		log.Error("failed to populate log reader", "error", err)
		ctxCancel()
		return err
	}

	tempDir := t.TempDir()

	co := coordinate.NewCoordinator(log, coordinate.CoordinatorConfig{
		Address:       optsAddress,
		EtcdEndpoints: optsEtcdEndpoints,
		Prefix:        optsEtcdPrefix,
		Resolver:      res,
		TempDir:       tempDir,
		DataPath:      filepath.Join(tempDir, "coordinator"),
		Mem:           &mem,
		Cpu:           &cpu,
		Logs:          &logs,
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

	// Run the runner!

	// Create RPC client to interact with coordinator
	scfg, err := co.ServiceConfig()
	if err != nil {
		ctxCancel()
		return err
	}

	// Create RPC client to interact with coordinator
	rs, err := scfg.State(ctx, rpc.WithLogger(log))
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
		defer t.Log("ipallocator watch complete")
		return ipa.Watch(ctx, eac)
	})

	aa := co.Activator()

	spm := co.SandboxPoolManager()

	ingressConfig := httpingress.IngressConfig{
		RequestTimeout: 60 * time.Second, // Default timeout for tests
	}
	hs := httpingress.NewServer(ctx, log, ingressConfig, client, aa, nil)

	reg.Register("app-activator", aa)
	reg.Register("sandbox-pool-manager", spm)
	reg.Register("resolver", res)

	rcfg, err := co.NamedConfig("runner")
	if err != nil {
		ctxCancel()
		return err
	}

	r, err := runner.NewRunner(log, reg, runner.RunnerConfig{
		Id:            optsRunnerId,
		ListenAddress: optsRunnerAddress,
		Workers:       1,
		Config:        rcfg,
		DataPath:      t.TempDir(),
	})
	if err != nil {
		ctxCancel()
		return err
	}

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
		defer t.Log("scheduled watch complete")
		return sch.Watch(ctx, eac)
	})

	httpServer := &http.Server{
		Addr:    ":80",
		Handler: hs,
	}

	// Register cleanup function to shutdown the HTTP server
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Error("failed to shutdown HTTP ingress server", "error", err)
		}
		log.Info("HTTP ingress server shutdown complete")
	})

	// Use errgroup to capture and propagate HTTP server errors
	eg.Go(func() error {
		log.Info("Starting HTTP ingress server", "addr", httpServer.Addr)
		err := httpServer.ListenAndServe()
		if err == http.ErrServerClosed {
			log.Info("ingress server closed")
			return nil
		}
		if err != nil {
			log.Error("failed to start HTTP server", "error", err)
			return err
		}
		return nil
	})

	var ociRegistry ocireg.Registry
	err = reg.Populate(&ociRegistry)
	if err != nil {
		log.Error("failed to populate OCI registry", "error", err)
		ctxCancel()
		return err
	}

	// Start the OCI Registry
	err = ociRegistry.Start(ctx, ":5000")
	if err != nil {
		log.Error("failed to start OCI registry", "error", err)
		ctxCancel()
		return err
	}

	// Register cleanup for OCI registry server
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := ociRegistry.Shutdown(shutdownCtx); err != nil {
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

	log.Info("Starting test server", "address", optsAddress, "etcd_endpoints", optsEtcdEndpoints, "etcd_prefix", optsEtcdPrefix, "runner_id", optsRunnerId)

	// Register cleanup for running components
	t.Cleanup(func() {
		// TODO: Close any RPC connections, currently hangs
		// if client != nil {
		// 	log.Info("Closing RPC client connections")
		// 	client.Close()
		// }

		log.Info("Stopping coordinator and controllers")
		co.Stop()

		log.Info("Canceling context to stop all components")
		ctxCancel()

		// TODO: eg.Wait still hangs, something's amiss in the context canecel handling. A problem for another day!
		// if err := eg.Wait(); err != nil {
		// 	log.Error("error waiting for components to stop", "error", err)
		// }
	})

	// Wait in a separate goroutine for any errors from the errgroup
	eg.Go(func() error {
		// This goroutine will exit when the first error occurs in any of the
		// other goroutines in the group, or when the context is canceled
		<-sub.Done()
		if err := sub.Err(); err != nil && err != context.Canceled {
			log.Error("component error detected", "error", err)
			return err
		}
		return nil
	})

	// Let the server run until the test ends or until an error occurs
	return nil
}
