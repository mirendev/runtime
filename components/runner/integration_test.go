package runner

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/stretchr/testify/require"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/scheduler"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/testutils"
)

func TestRunnerCoordinatorIntegration(t *testing.T) {
	r := require.New(t)

	// Setup logging
	reg, cleanup := testutils.Registry(observability.TestInject)
	defer cleanup()

	var log *slog.Logger

	err := reg.Init(&log)
	r.NoError(err)

	// Create temp directory for test data
	tempDir := t.TempDir()

	// Setup coordinator config
	coordCfg := coordinate.CoordinatorConfig{
		Address:       "localhost:9991",          // Use test port
		EtcdEndpoints: []string{"etcd:2379"},     // Default etcd port
		Prefix:        "/test/miren/" + t.Name(), // Unique prefix for this test
		DataPath:      tempDir,                   // Use temp directory to prevent file leaks
	}

	// Setup runner config
	runnerCfg := RunnerConfig{
		Id:      "test-runner",
		Workers: 2,
	}

	// Create contexts
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start coordinator in background
	coord := coordinate.NewCoordinator(log, coordCfg)
	err = coord.Start(ctx)
	r.NoError(err)

	// Wait for coordinator to start
	time.Sleep(1 * time.Second)

	rcfg, err := coord.ServiceConfig()
	r.NoError(err)

	runnerCfg.Config = rcfg
	runnerCfg.DataPath = t.TempDir()

	// Create and start runner
	runner, err := NewRunner(log, reg, runnerCfg)
	r.NoError(err)

	runnerDone := make(chan error, 1)
	go func() {
		runnerDone <- runner.Start(ctx)
	}()

	defer runner.Close()

	// Wait for runner to start
	time.Sleep(1 * time.Second)

	select {
	case err := <-runnerDone:
		require.NoError(t, err)
	default:
	}

	cfg, err := coord.LocalConfig()
	r.NoError(err)

	// Create RPC client to interact with coordinator
	rs, err := cfg.State(ctx)
	require.NoError(t, err)

	client, err := rs.Connect(coordCfg.Address, "entities")
	require.NoError(t, err)

	eac := entityserver_v1alpha.EntityAccessClient{Client: client}

	// Check the node entity for the runner
	nodeId := "node/" + runnerCfg.Id

	res, err := eac.Get(ctx, nodeId)
	r.NoError(err)

	r.True(res.HasEntity())

	node := entity.New(res.Entity().Attrs())

	status, ok := node.Get(compute.NodeStatusId)
	r.True(ok)

	r.Equal(compute.NodeStatusReadyId, status.Value.Id())

	tmpSch, err := scheduler.NewScheduler(ctx, log, &eac)
	r.NoError(err)

	schNode, err := tmpSch.FindNodeById(entity.Id(nodeId))
	r.NoError(err)

	r.NotNil(schNode)

	go tmpSch.Watch(ctx, &eac)
	time.Sleep(1 * time.Second)

	id := fmt.Sprintf("sandbox/test-%d", time.Now().Unix())

	// Test creating a sandbox entity
	sandbox := &entityserver_v1alpha.Entity{}
	sandbox.SetAttrs(entity.New(
		entity.EntityKind, compute.KindSandbox,
		entity.Keyword(entity.Ident, id),
	).Attrs())

	_, err = eac.Put(ctx, sandbox)
	r.NoError(err)

	var (
		cc *containerd.Client
	)

	r.NoError(reg.Init(&cc))

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
