package runner

import (
	"context"
	"fmt"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/stretchr/testify/require"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/controllers/sandbox"
	schedulerctrl "miren.dev/runtime/controllers/scheduler"
	"miren.dev/runtime/network"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/testutils"
	"miren.dev/runtime/version"
)

func TestRunnerCoordinatorIntegration(t *testing.T) {
	r := require.New(t)

	// Setup test dependencies
	testDeps, cleanup := testutils.NewTestDeps()
	defer cleanup()

	// Create temp directory for test data
	tempDir := t.TempDir()

	// Use dynamic port to avoid conflicts with parallel tests
	port := testutils.GetFreePort(t)

	// Setup coordinator config
	coordCfg := coordinate.CoordinatorConfig{
		Address:       fmt.Sprintf("localhost:%d", port),
		EtcdEndpoints: []string{"etcd:2379"},
		Prefix:        "/test/miren/" + t.Name(), // Unique prefix for this test
		DataPath:      tempDir,                   // Use temp directory to prevent file leaks
	}

	// Setup runner config
	runnerCfg := RunnerConfig{
		Id:            "test-runner",
		ListenAddress: "localhost:0",
		Workers:       2,
	}

	// Create contexts
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Start coordinator in background
	coord := coordinate.NewControlPlane(coordinate.NewFoundation(testDeps.Log, coordCfg))
	err := coord.Start(ctx)
	r.NoError(err)
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		r.NoError(coord.Stop(shutdownCtx))
	}()

	rcfg, err := coord.ServiceConfig()
	r.NoError(err)

	runnerCfg.Config = rcfg
	runnerCfg.DataPath = t.TempDir()

	res, _ := netresolve.NewLocalResolver()

	// Build RunnerDeps from testDeps
	sbMetrics := sandbox.NewMetrics()
	sbMetrics.Log = testDeps.Log
	sbMetrics.CPUUsage = testDeps.CPU
	sbMetrics.MemUsage = testDeps.Mem

	deps := RunnerDeps{
		CC:              testDeps.CC,
		Namespace:       testDeps.Namespace,
		Bridge:          testDeps.Bridge,
		Tempdir:         tempDir,
		Subnet:          testDeps.Subnet,
		NetServ:         network.NewServiceManager(testDeps.Log, nil),
		LogsMaintainer:  testDeps.LogsMaintainer,
		LogWriter:       testDeps.LogWriter,
		StatusMon:       testDeps.StatusMon,
		IPv4Routable:    testDeps.IPv4Routable,
		ServicePrefixes: testDeps.ServicePrefixes,
		DisableLocalNet: false,
		Resolver:        res,
		SandboxMetrics:  sbMetrics,
	}

	// Create and start runner
	runner, err := NewRunner(testDeps.Log, deps, runnerCfg)
	r.NoError(err)

	runnerDone := make(chan error, 1)
	go func() {
		runnerDone <- runner.Start(ctx)
	}()

	defer func() {
		cancel()
		runner.Close()
	}()

	cfg, err := coord.LocalConfig()
	r.NoError(err)

	// Create RPC client to interact with coordinator
	rs, err := cfg.State(ctx)
	require.NoError(t, err)

	client, err := rs.Connect(coordCfg.Address, "entities")
	require.NoError(t, err)

	eac := entityserver_v1alpha.EntityAccessClient{Client: client}

	// Check the node entity for the runner
	nodeId := compute.NewNodeId(runnerCfg.Id).String()

	// Poll for the node entity to be ready (wait for runner startup to complete)
	var nodeRes *entityserver_v1alpha.EntityAccessClientGetResults
	pollTimeout := time.After(10 * time.Second)
	pollTicker := time.NewTicker(100 * time.Millisecond)
	defer pollTicker.Stop()

	for {
		select {
		case err := <-runnerDone:
			// runner.Start() completed, check for error
			require.NoError(t, err, "runner.Start() failed")
			// If it succeeded, the node should now be available, try one more time
			nodeRes, err = eac.Get(ctx, nodeId)
			require.NoError(t, err, "failed to get node entity after runner started")
			require.True(t, nodeRes.HasEntity(), "node entity not found after runner started")
			// Verify status field is present
			node := entity.New(nodeRes.Entity().Attrs())
			_, ok := node.Get(compute.NodeStatusId)
			require.True(t, ok, "node entity status not set after runner started")
			goto nodeReady
		case <-pollTimeout:
			t.Fatal("timeout waiting for node entity to be created with status")
		case <-pollTicker.C:
			nodeRes, err = eac.Get(ctx, nodeId)
			if err == nil && nodeRes.HasEntity() {
				// Check that status field is present before proceeding
				node := entity.New(nodeRes.Entity().Attrs())
				if _, ok := node.Get(compute.NodeStatusId); ok {
					goto nodeReady
				}
			}
		}
	}

nodeReady:

	r.True(nodeRes.HasEntity())

	node := entity.New(nodeRes.Entity().Attrs())

	status, ok := node.Get(compute.NodeStatusId)
	r.True(ok)

	r.Equal(compute.NodeStatusReadyId, status.Value.Id())

	nodeVersion, ok := node.Get(compute.NodeVersionId)
	r.True(ok, "node entity should have version attr after startup")
	r.Equal(version.GetInfo().Version, nodeVersion.Value.String())

	// Create and start the scheduler controller
	scheduler := schedulerctrl.NewController(testDeps.Log, &eac)
	r.NoError(scheduler.Init(ctx))

	schedulerController := controller.NewReconcileController(
		"scheduler",
		testDeps.Log,
		entity.Ref(entity.EntityKind, compute.KindSandbox),
		&eac,
		controller.AdaptReconcileController[compute.Sandbox](scheduler),
		time.Minute,
		1,
	)
	r.NoError(schedulerController.Start(ctx))
	defer schedulerController.Stop()

	time.Sleep(1 * time.Second)

	id := fmt.Sprintf("sandbox/test-%d", time.Now().Unix())

	// Test creating a sandbox entity
	sandboxEntity := &entityserver_v1alpha.Entity{}
	sandboxEntity.SetAttrs(entity.New(
		entity.EntityKind, compute.KindSandbox,
		entity.Keyword(entity.Ident, id),
	).Attrs())

	_, err = eac.Put(ctx, sandboxEntity)
	r.NoError(err)

	ctx = namespaces.WithNamespace(ctx, runner.ContainerdNamespace())

	// Wait for container to be created with timeout
	var c containerd.Container
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

waitForCreate:
	for {
		select {
		case <-timeout:
			r.Fail("Timeout waiting for container creation")
		case <-ticker.C:
			c, err = runner.ContainerdContainerForSandbox(ctx, entity.Id(id))
			r.NoError(err)
			if c != nil {
				if t, _ := c.Task(ctx, nil); t != nil {
					// We're looking for an actual running process before we continue
					if pids, _ := t.Pids(ctx); len(pids) > 0 {
						break waitForCreate
					}
				}
			}
		}
	}

	r.NotNil(c)

	lbl, err := c.Labels(ctx)
	r.NoError(err)

	r.Equal(id, lbl["runtime.computer/entity-id"])

	// Extra cleanup attempt in case the test fails
	defer testutils.ClearContainer(ctx, c)

	r.NotNil(c)

	// Wait for sandbox entity to reach RUNNING status before deleting.
	// This ensures the controller has finished all its updates and the entity
	// is in a stable state, avoiding OCC conflicts during delete.
	timeout = time.After(10 * time.Second)
	ticker = time.NewTicker(100 * time.Millisecond)
waitForRunning:
	for {
		select {
		case <-timeout:
			r.Fail("Timeout waiting for sandbox to reach RUNNING status")
		case <-ticker.C:
			res, err := eac.Get(ctx, id)
			if err != nil {
				continue
			}
			if res.HasEntity() {
				var sb compute.Sandbox
				sb.Decode(res.Entity().Entity())
				if sb.Status == compute.RUNNING {
					break waitForRunning
				}
			}
		}
	}
	ticker.Stop()

	// Test deleting the sandbox entity

	_, err = eac.Delete(ctx, id)
	r.NoError(err)

	// Wait for container to be cleaned up with timeout
	var cleanedUp bool
	timeout = time.After(10 * time.Second)
	ticker = time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			r.Fail("Timeout waiting for container cleanup")
		case <-ticker.C:
			c, err = runner.ContainerdContainerForSandbox(ctx, entity.Id(id))
			r.NoError(err)
			if c == nil {
				cleanedUp = true
				goto done
			}
		}
	}
done:

	r.True(cleanedUp, "Container should be cleaned up after entity deletion")

	// Cleanup
	cancel()
}
