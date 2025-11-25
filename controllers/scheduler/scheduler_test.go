package scheduler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

// reconcileSandbox is a test helper that processes a sandbox through the real controller framework.
// It creates a ReconcileController and calls ProcessEventForTest, which runs the exact same code
// path as production: handler invocation, diff calculation, and Patch application.
func reconcileSandbox(t *testing.T, ctx context.Context, server *testutils.InMemEntityServer, scheduler *Controller, sandboxID entity.Id) {
	t.Helper()

	// Create a real ReconcileController - this gives us the exact production code path
	rc := controller.NewReconcileController(
		"test-scheduler",
		testutils.TestLogger(t),
		entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox),
		server.EAC,
		controller.AdaptReconcileController[compute_v1alpha.Sandbox](scheduler),
		0, // resync period (not used for ProcessEventForTest)
		1, // workers (not used for ProcessEventForTest)
	)

	// Fetch current entity state
	resp, err := server.EAC.Get(ctx, sandboxID.String())
	require.NoError(t, err)

	// Create an event like the controller framework would
	event := controller.Event{
		Type:   controller.EventAdded,
		Id:     sandboxID,
		Entity: resp.Entity().Entity(),
	}

	// ProcessEventForTest runs processItem + applyUpdates - the exact production code path
	err = rc.ProcessEventForTest(ctx, event)
	require.NoError(t, err)
}

// TestSchedulerAssignsUnscheduledSandbox tests that the scheduler assigns
// a node to a sandbox that doesn't have a ScheduleKey
func TestSchedulerAssignsUnscheduledSandbox(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create a ready node
	node := &compute_v1alpha.Node{
		Status: compute_v1alpha.READY,
	}
	nodeID, err := server.Client.Create(ctx, "test-node", node)
	require.NoError(t, err)

	// Create scheduler and initialize (gathers nodes)
	scheduler := NewController(log, server.EAC)
	err = scheduler.Init(ctx)
	require.NoError(t, err)

	// Create an unscheduled sandbox
	sandbox := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.PENDING,
		Spec: compute_v1alpha.SandboxSpec{
			Container: []compute_v1alpha.SandboxSpecContainer{
				{Image: "test:latest"},
			},
		},
	}
	sandboxID, err := server.Client.Create(ctx, "test-sandbox", sandbox)
	require.NoError(t, err)

	// Run reconciliation through the real controller framework
	reconcileSandbox(t, ctx, server, scheduler, sandboxID)

	// Fetch the updated sandbox and verify it was assigned to the node
	resp, err := server.EAC.Get(ctx, sandboxID.String())
	require.NoError(t, err)

	var schedule compute_v1alpha.Schedule
	schedule.Decode(resp.Entity().Entity())

	assert.Equal(t, nodeID, schedule.Key.Node, "sandbox should be assigned to our node")
	assert.Equal(t, compute_v1alpha.KindSandbox, schedule.Key.Kind, "schedule key should have sandbox kind")
}

// TestSchedulerSkipsAlreadyScheduledSandbox tests that the scheduler
// doesn't re-assign a sandbox that already has a ScheduleKey
func TestSchedulerSkipsAlreadyScheduledSandbox(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create two ready nodes
	node1 := &compute_v1alpha.Node{Status: compute_v1alpha.READY}
	node1ID, err := server.Client.Create(ctx, "node-1", node1)
	require.NoError(t, err)

	node2 := &compute_v1alpha.Node{Status: compute_v1alpha.READY}
	_, err = server.Client.Create(ctx, "node-2", node2)
	require.NoError(t, err)

	// Create scheduler and initialize
	scheduler := NewController(log, server.EAC)
	err = scheduler.Init(ctx)
	require.NoError(t, err)

	// Create a sandbox that's already scheduled to node1
	sandbox := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.PENDING,
		Spec: compute_v1alpha.SandboxSpec{
			Container: []compute_v1alpha.SandboxSpecContainer{
				{Image: "test:latest"},
			},
		},
	}
	sandboxID, err := server.Client.Create(ctx, "test-sandbox", sandbox)
	require.NoError(t, err)

	// Manually add the schedule key to simulate already-scheduled sandbox
	schedule := compute_v1alpha.Schedule{
		Key: compute_v1alpha.Key{
			Kind: compute_v1alpha.KindSandbox,
			Node: node1ID,
		},
	}

	resp, err := server.EAC.Get(ctx, sandboxID.String())
	require.NoError(t, err)

	ent := resp.Entity().Entity()
	err = ent.Update(schedule.Encode())
	require.NoError(t, err)

	server.Store.AddEntity(sandboxID, ent)

	// Run reconciliation - should not change the assignment
	reconcileSandbox(t, ctx, server, scheduler, sandboxID)

	// Verify the sandbox is still assigned to node1 (not reassigned)
	resp, err = server.EAC.Get(ctx, sandboxID.String())
	require.NoError(t, err)

	var updatedSchedule compute_v1alpha.Schedule
	updatedSchedule.Decode(resp.Entity().Entity())
	assert.Equal(t, node1ID, updatedSchedule.Key.Node, "sandbox should still be assigned to node1")
}

// TestSchedulerNoAvailableNodes tests that the scheduler handles
// the case where no nodes are available (all not ready or none exist)
func TestSchedulerNoAvailableNodes(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create a node that's not ready
	node := &compute_v1alpha.Node{
		Status: compute_v1alpha.DISABLED,
	}
	_, err := server.Client.Create(ctx, "disabled-node", node)
	require.NoError(t, err)

	// Create scheduler and initialize
	scheduler := NewController(log, server.EAC)
	err = scheduler.Init(ctx)
	require.NoError(t, err)

	// Create an unscheduled sandbox
	sandbox := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.PENDING,
	}
	sandboxID, err := server.Client.Create(ctx, "test-sandbox", sandbox)
	require.NoError(t, err)

	// Run reconciliation - should not error, just not assign
	reconcileSandbox(t, ctx, server, scheduler, sandboxID)

	// Verify the sandbox was NOT assigned (no schedule key added)
	resp, err := server.EAC.Get(ctx, sandboxID.String())
	require.NoError(t, err)

	_, ok := resp.Entity().Entity().Get(compute_v1alpha.ScheduleKeyId)
	assert.False(t, ok, "sandbox should not have schedule key when no nodes available")
}

// TestSchedulerMultipleNodes tests that the scheduler can assign
// sandboxes when multiple ready nodes are available
func TestSchedulerMultipleNodes(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create multiple ready nodes
	nodeIDs := make(map[entity.Id]bool)
	for i := 0; i < 3; i++ {
		node := &compute_v1alpha.Node{Status: compute_v1alpha.READY}
		nodeID, err := server.Client.Create(ctx, "", node)
		require.NoError(t, err)
		nodeIDs[nodeID] = true
	}

	// Create scheduler and initialize
	scheduler := NewController(log, server.EAC)
	err := scheduler.Init(ctx)
	require.NoError(t, err)

	// Create and schedule multiple sandboxes
	for i := 0; i < 5; i++ {
		sandbox := &compute_v1alpha.Sandbox{
			Status: compute_v1alpha.PENDING,
		}
		sandboxID, err := server.Client.Create(ctx, "", sandbox)
		require.NoError(t, err)

		reconcileSandbox(t, ctx, server, scheduler, sandboxID)

		// Fetch and verify assigned to one of our nodes
		resp, err := server.EAC.Get(ctx, sandboxID.String())
		require.NoError(t, err)

		var schedule compute_v1alpha.Schedule
		schedule.Decode(resp.Entity().Entity())
		assert.True(t, nodeIDs[schedule.Key.Node], "sandbox should be assigned to one of our nodes")
	}
}

// TestSchedulerInitGathersNodes tests that Init properly gathers
// all existing nodes from the entity store
func TestSchedulerInitGathersNodes(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create nodes before initializing scheduler
	readyNode := &compute_v1alpha.Node{Status: compute_v1alpha.READY}
	readyID, err := server.Client.Create(ctx, "ready-node", readyNode)
	require.NoError(t, err)

	disabledNode := &compute_v1alpha.Node{Status: compute_v1alpha.DISABLED}
	disabledID, err := server.Client.Create(ctx, "disabled-node", disabledNode)
	require.NoError(t, err)

	// Create scheduler and initialize
	scheduler := NewController(log, server.EAC)
	err = scheduler.Init(ctx)
	require.NoError(t, err)

	// Verify both nodes were gathered
	assert.Len(t, scheduler.nodes, 2, "scheduler should have gathered 2 nodes")
	assert.Contains(t, scheduler.nodes, readyID, "scheduler should have ready node")
	assert.Contains(t, scheduler.nodes, disabledID, "scheduler should have disabled node")

	// Verify node status is preserved
	assert.Equal(t, compute_v1alpha.READY, scheduler.nodes[readyID].Status)
	assert.Equal(t, compute_v1alpha.DISABLED, scheduler.nodes[disabledID].Status)
}
