package testserver

import (
	"context"
	"log/slog"
	"net/http"
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
	"miren.dev/runtime/controllers/sandbox"
	"miren.dev/runtime/network"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/slogfmt"
	"miren.dev/runtime/pkg/testutils"
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

	testDeps, cleanup := testutils.NewTestDeps()
	t.Cleanup(cleanup)

	// Mirroring defaults from cli/commands/dev.go
	optsAddress := "localhost:8443"
	optsRunnerAddress := "localhost:8444"
	optsRunnerId := "dev"

	log := slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))

	res, hm := netresolve.NewLocalResolver()

	tempDir := t.TempDir()

	co := coordinate.NewCoordinator(log, coordinate.CoordinatorConfig{
		Address:            optsAddress,
		EtcdEndpoints:      []string{"http://etcd:2379"},
		Prefix:             "/" + testDeps.Namespace,
		Resolver:           res,
		TempDir:            tempDir,
		DataPath:           filepath.Join(tempDir, "coordinator"),
		Mem:                testDeps.Mem,
		Cpu:                testDeps.CPU,
		Logs:               testDeps.Logs,
		NoAuth:             true,
		HTTPRequestTimeout: 60 * time.Second,
	})

	t.Log("Starting coordinator")
	err := co.Start(sub)
	if err != nil {
		log.Error("failed to start coordinator", "error", err)
		ctxCancel()
		return err
	}
	// Cleanup runs in reverse registration order. Register the coordinator now
	// so the HTTP and OCI servers below drain before coordinator RPC shuts down.
	t.Cleanup(func() {
		log.Info("Stopping coordinator and controllers")
		ctxCancel()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := co.Stop(shutdownCtx); err != nil {
			log.Error("failed to stop coordinator", "error", err)
		}
	})

	// Create subnet from test deps
	subnet := testDeps.Subnet

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

	ipa := ipalloc.NewAllocator(log, testDeps.ServicePrefixes)
	eg.Go(func() error {
		defer t.Log("ipallocator watch complete")
		return ipa.Watch(ctx, eac)
	})

	// Get httpingress from coordinator (created there for admin server)
	hs := co.HttpIngress()

	rcfg, err := co.RunnerConfig(optsRunnerAddress)
	if err != nil {
		ctxCancel()
		return err
	}

	// Create network service manager
	netServ := network.NewServiceManager(log, eac)

	// Create observability components
	statusMon := observability.NewStatusMonitor(log)
	logsMaintainer := observability.NewLogsMaintainer()

	// Use testDeps.LogWriter (PersistentLogWriter) so logs can be read back via VictoriaLogs
	logWriter := testDeps.LogWriter

	// Create sandbox metrics
	sbMetrics := sandbox.NewMetrics()
	sbMetrics.Log = log
	sbMetrics.CPUUsage = testDeps.CPU
	sbMetrics.MemUsage = testDeps.Mem

	deps := runner.RunnerDeps{
		CC:              testDeps.CC,
		Namespace:       testDeps.Namespace,
		Bridge:          testDeps.Bridge,
		Tempdir:         tempDir,
		Subnet:          subnet,
		NetServ:         netServ,
		LogsMaintainer:  logsMaintainer,
		LogWriter:       logWriter,
		StatusMon:       statusMon,
		IPv4Routable:    testDeps.IPv4Routable,
		ServicePrefixes: testDeps.ServicePrefixes,
		DisableLocalNet: false,
		Resolver:        res,
		SandboxMetrics:  sbMetrics,
		IsCoordinator:   true,
	}

	r, err := runner.NewRunner(log, deps, runner.RunnerConfig{
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

	err = r.Start(sub, eg)
	if err != nil {
		ctxCancel()
		return err
	}

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

	// Create OCI registry
	ociRegistry := ocireg.NewRegistry(filepath.Join(tempDir, "oci"), log, ec, nil)

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

	regAddr := subnet.Router().Addr()

	log.Info("OCI registry URL", "url", regAddr)

	hm.SetHost("cluster.local", regAddr)

	log.Info("Starting test server", "address", optsAddress, "runner_id", optsRunnerId)

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
