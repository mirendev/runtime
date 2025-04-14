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
	"miren.dev/runtime/api/entityserver/v1alpha"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/scheduler"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
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

	// Setup coordinator config
	coordCfg := coordinate.CoordinatorConfig{
		Address:       "localhost:9991",          // Use test port
		EtcdEndpoints: []string{"etcd:2379"},     // Default etcd port
		Prefix:        "/test/miren/" + t.Name(), // Unique prefix for this test
	}

	// Setup runner config
	runnerCfg := RunnerConfig{
		Id:            "node/test-runner",
		ServerAddress: coordCfg.Address,
		Workers:       2,
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

	// Create and start runner
	runner := NewRunner(log, reg, runnerCfg)
	runnerDone := make(chan error, 1)
	go func() {
		runnerDone <- runner.Start(ctx)
	}()

	// Wait for runner to start
	time.Sleep(1 * time.Second)

	// Create RPC client to interact with coordinator
	rs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	require.NoError(t, err)

	client, err := rs.Connect(coordCfg.Address, "entities")
	require.NoError(t, err)

	eac := v1alpha.EntityAccessClient{Client: client}

	// Check the node entity for the runner
	nodeId := runnerCfg.Id

	res, err := eac.Get(ctx, nodeId)
	r.NoError(err)

	r.True(res.HasEntity())

	node := &entity.Entity{
		Attrs: res.Entity().Attrs(),
	}

	status, ok := node.Get(compute.NodeStatusId)
	r.True(ok)

	r.Equal(compute.NodeStatusReadyId, status.Value.Id())

	tmpSch, err := scheduler.NewScheduler(ctx, log, &eac)
	r.NoError(err)

	schNode, err := tmpSch.FindNodeById(entity.Id(nodeId))
	r.NoError(err)

	r.NotNil(schNode)

	go tmpSch.Watch(ctx, &eac)

	id := fmt.Sprintf("sandbox/test-%d", time.Now().Unix())

	// Test creating a sandbox entity
	sandbox := &v1alpha.Entity{}
	sandbox.SetAttrs(entity.Attrs(
		entity.EntityKind, compute.KindSandbox,
		entity.Keyword(entity.Ident, id),
	))

	_, err = eac.Put(ctx, sandbox)
	r.NoError(err)

	// Wait a bit for processing
	time.Sleep(2 * time.Second)

	var (
		cc *containerd.Client
	)

	r.NoError(reg.Init(&cc))

	ctx = namespaces.WithNamespace(ctx, runner.ContainerdNamespace())

	c, err := runner.ContainerdContainerForSandbox(ctx, entity.Id(id))
	r.NoError(err)

	r.NotNil(c)

	lbl, err := c.Labels(ctx)
	r.NoError(err)

	r.Equal(id, lbl["runtime.computer/entity-id"])

	defer testutils.ClearContainer(ctx, c)

	r.NotNil(c)

	// Test deleting the sandbox entity

	_, err = eac.Delete(ctx, id)
	r.NoError(err)

	// Wait a bit for processing

	time.Sleep(2 * time.Second)

	c, err = runner.ContainerdContainerForSandbox(ctx, entity.Id(id))
	r.NoError(err)
	r.Nil(c)

	// Verify the sandbox was processed
	// In a real test, you'd verify the actual sandbox state
	// This is just a placeholder for demonstration

	// Cleanup
	cancel()

	select {
	case err := <-runnerDone:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("runner failed to shut down")
	}
}
