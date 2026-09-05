package nodehealth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

// createReadyNode creates a node entity with session-scoped READY status.
func createReadyNode(t *testing.T, ctx context.Context, client *entityserver.Client, name string, node *compute_v1alpha.Node) entity.Id {
	t.Helper()

	status := node.Status
	node.Status = ""
	if node.ApiAddress == "" {
		node.ApiAddress = ":8444"
	}
	nodeID, err := client.Create(ctx, name, node)
	require.NoError(t, err)

	_, sc, err := client.NewSession(ctx, "test node health")
	require.NoError(t, err)

	err = sc.UpdateAttrs(ctx, nodeID, (&compute_v1alpha.Node{Status: status}).Encode)
	require.NoError(t, err)

	return nodeID
}

// createScheduledSandbox creates a sandbox entity and assigns it to a node
// via a schedule key, mimicking what the scheduler controller does.
func createScheduledSandbox(t *testing.T, ctx context.Context, server *testutils.InMemEntityServer, name string, nodeID entity.Id, status compute_v1alpha.SandboxStatus) entity.Id {
	t.Helper()

	sandbox := &compute_v1alpha.Sandbox{
		Status: status,
		Spec: compute_v1alpha.SandboxSpec{
			Container: []compute_v1alpha.SandboxSpecContainer{
				{Image: "test:latest"},
			},
		},
	}
	sandboxID, err := server.Client.Create(ctx, name, sandbox)
	require.NoError(t, err)

	// Add schedule key to assign sandbox to the node
	schedule := compute_v1alpha.Schedule{
		Key: compute_v1alpha.Key{
			Kind: compute_v1alpha.KindSandbox,
			Node: nodeID,
		},
	}
	_, err = server.EAC.Patch(ctx, entity.New(
		entity.DBId, sandboxID,
		schedule.Encode,
	).Attrs(), 0)
	require.NoError(t, err)

	return sandboxID
}

// reconcileNode runs the nodehealth controller through the real controller
// framework for a single node entity.
func reconcileNode(t *testing.T, ctx context.Context, server *testutils.InMemEntityServer, ctrl *Controller) {
	t.Helper()

	rc := controller.NewReconcileController(
		"test-nodehealth",
		testutils.TestLogger(t),
		entity.Ref(entity.EntityKind, compute_v1alpha.KindNode),
		server.EAC,
		controller.AdaptReconcileController[compute_v1alpha.Node](ctrl),
		0, 1,
	)

	resp, err := server.EAC.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindNode))
	require.NoError(t, err)

	for _, e := range resp.Values() {
		event := controller.Event{
			Type:   controller.EventUpdated,
			Id:     e.Entity().Id(),
			Entity: e.Entity(),
		}
		err = rc.ProcessEventForTest(ctx, event)
		require.NoError(t, err)
	}
}

func getSandboxStatus(t *testing.T, ctx context.Context, server *testutils.InMemEntityServer, sandboxID entity.Id) compute_v1alpha.SandboxStatus {
	t.Helper()
	resp, err := server.EAC.Get(ctx, sandboxID.String())
	require.NoError(t, err)
	var sb compute_v1alpha.Sandbox
	sb.Decode(resp.Entity().Entity())
	return sb.Status
}

func TestReadyNodeNoAction(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	nodeID := createReadyNode(t, ctx, server.Client, "test-node", &compute_v1alpha.Node{
		Status: compute_v1alpha.READY,
	})

	sbID := createScheduledSandbox(t, ctx, server, "test-sandbox", nodeID, compute_v1alpha.RUNNING)

	ctrl := NewController(testutils.TestLogger(t), server.EAC)
	require.NoError(t, ctrl.Init(ctx))

	reconcileNode(t, ctx, server, ctrl)

	assert.Equal(t, compute_v1alpha.RUNNING, getSandboxStatus(t, ctx, server, sbID),
		"sandbox on READY node should remain RUNNING")
}

func TestNonReadyNodeWithinGracePeriod(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create a node that is NOT ready (no session-scoped status set)
	node := &compute_v1alpha.Node{ApiAddress: ":8444"}
	nodeID, err := server.Client.Create(ctx, "test-node", node)
	require.NoError(t, err)

	sbID := createScheduledSandbox(t, ctx, server, "test-sandbox", nodeID, compute_v1alpha.RUNNING)

	ctrl := NewController(testutils.TestLogger(t), server.EAC)
	ctrl.gracePeriod = 5 * time.Minute
	require.NoError(t, ctrl.Init(ctx))

	reconcileNode(t, ctx, server, ctrl)

	assert.Equal(t, compute_v1alpha.RUNNING, getSandboxStatus(t, ctx, server, sbID),
		"sandbox should remain RUNNING within grace period")
}

func TestNonReadyNodeGracePeriodExpired(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	node := &compute_v1alpha.Node{ApiAddress: ":8444"}
	nodeID, err := server.Client.Create(ctx, "test-node", node)
	require.NoError(t, err)

	sbRunning := createScheduledSandbox(t, ctx, server, "sb-running", nodeID, compute_v1alpha.RUNNING)
	sbPending := createScheduledSandbox(t, ctx, server, "sb-pending", nodeID, compute_v1alpha.PENDING)

	ctrl := NewController(testutils.TestLogger(t), server.EAC)
	ctrl.gracePeriod = 5 * time.Minute
	require.NoError(t, ctrl.Init(ctx))

	// First reconciliation starts the grace period
	reconcileNode(t, ctx, server, ctrl)

	// Advance time past grace period
	ctrl.now = func() time.Time { return time.Now().Add(6 * time.Minute) }

	// Second reconciliation should mark sandboxes DEAD
	reconcileNode(t, ctx, server, ctrl)

	assert.Equal(t, compute_v1alpha.DEAD, getSandboxStatus(t, ctx, server, sbRunning),
		"RUNNING sandbox should be marked DEAD after grace period")
	assert.Equal(t, compute_v1alpha.DEAD, getSandboxStatus(t, ctx, server, sbPending),
		"PENDING sandbox should be marked DEAD after grace period")
}

func TestNodeRecoversWithinGracePeriod(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Start with a non-ready node
	node := &compute_v1alpha.Node{ApiAddress: ":8444"}
	nodeID, err := server.Client.Create(ctx, "test-node", node)
	require.NoError(t, err)

	sbID := createScheduledSandbox(t, ctx, server, "test-sandbox", nodeID, compute_v1alpha.RUNNING)

	ctrl := NewController(testutils.TestLogger(t), server.EAC)
	ctrl.gracePeriod = 5 * time.Minute
	require.NoError(t, ctrl.Init(ctx))

	// First reconciliation starts the grace period
	reconcileNode(t, ctx, server, ctrl)

	// Node recovers (set status to READY via session)
	_, sc, err := server.Client.NewSession(ctx, "recovery")
	require.NoError(t, err)
	err = sc.UpdateAttrs(ctx, nodeID, (&compute_v1alpha.Node{Status: compute_v1alpha.READY}).Encode)
	require.NoError(t, err)

	// Reconcile again - should clear grace period tracking
	reconcileNode(t, ctx, server, ctrl)

	// Advance time well past the original grace period
	ctrl.now = func() time.Time { return time.Now().Add(10 * time.Minute) }

	// Reconcile again - should NOT mark sandboxes dead (node is READY)
	reconcileNode(t, ctx, server, ctrl)

	assert.Equal(t, compute_v1alpha.RUNNING, getSandboxStatus(t, ctx, server, sbID),
		"sandbox should remain RUNNING after node recovered")
}

func TestMarksStoppedSandboxDeadAfterGracePeriod(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	node := &compute_v1alpha.Node{ApiAddress: ":8444"}
	nodeID, err := server.Client.Create(ctx, "test-node", node)
	require.NoError(t, err)

	sbDead := createScheduledSandbox(t, ctx, server, "sb-dead", nodeID, compute_v1alpha.DEAD)
	sbStopped := createScheduledSandbox(t, ctx, server, "sb-stopped", nodeID, compute_v1alpha.STOPPED)
	sbRunning := createScheduledSandbox(t, ctx, server, "sb-running", nodeID, compute_v1alpha.RUNNING)

	ctrl := NewController(testutils.TestLogger(t), server.EAC)
	ctrl.gracePeriod = 0 // immediate action for this test
	require.NoError(t, ctrl.Init(ctx))

	reconcileNode(t, ctx, server, ctrl)

	assert.Equal(t, compute_v1alpha.DEAD, getSandboxStatus(t, ctx, server, sbDead),
		"already DEAD sandbox should stay DEAD")
	assert.Equal(t, compute_v1alpha.DEAD, getSandboxStatus(t, ctx, server, sbStopped),
		"STOPPED sandbox cannot finish cleanup after its node fails")
	assert.Equal(t, compute_v1alpha.DEAD, getSandboxStatus(t, ctx, server, sbRunning),
		"RUNNING sandbox should be marked DEAD")
}

func TestSweepOrphanedSandboxes(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	missingNodeID := createReadyNode(t, ctx, server.Client, "missing-node", &compute_v1alpha.Node{
		Status: compute_v1alpha.READY,
	})
	readyNodeID := createReadyNode(t, ctx, server.Client, "ready-node", &compute_v1alpha.Node{
		Status: compute_v1alpha.READY,
	})

	oldTime := time.Now().Add(-10 * time.Minute)
	server.Store.NowFunc = func() time.Time { return oldTime }
	orphanStopped := createScheduledSandbox(t, ctx, server, "orphan-stopped", missingNodeID, compute_v1alpha.STOPPED)
	orphanDead := createScheduledSandbox(t, ctx, server, "orphan-dead", missingNodeID, compute_v1alpha.DEAD)
	ownedStopped := createScheduledSandbox(t, ctx, server, "owned-stopped", readyNodeID, compute_v1alpha.STOPPED)
	server.Store.NowFunc = nil

	_, err := server.EAC.Delete(ctx, missingNodeID.String())
	require.NoError(t, err)

	ctrl := NewController(testutils.TestLogger(t), server.EAC)
	require.NoError(t, ctrl.Init(ctx))
	require.NoError(t, ctrl.SweepOrphanedSandboxes(ctx))

	_, err = server.EAC.Get(ctx, orphanStopped.String())
	assert.Error(t, err, "old STOPPED sandbox on a missing node should be deleted")
	_, err = server.EAC.Get(ctx, orphanDead.String())
	assert.Error(t, err, "old DEAD sandbox on a missing node should be deleted")
	assert.Equal(t, compute_v1alpha.STOPPED, getSandboxStatus(t, ctx, server, ownedStopped),
		"terminal sandbox on an existing node should be left for its owner")
}

func TestSweepOrphanedSandboxesHonorsGracePeriod(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	missingNodeID := createReadyNode(t, ctx, server.Client, "missing-node", &compute_v1alpha.Node{
		Status: compute_v1alpha.READY,
	})
	freshOrphan := createScheduledSandbox(t, ctx, server, "fresh-orphan", missingNodeID, compute_v1alpha.STOPPED)
	_, err := server.EAC.Delete(ctx, missingNodeID.String())
	require.NoError(t, err)

	ctrl := NewController(testutils.TestLogger(t), server.EAC)
	require.NoError(t, ctrl.Init(ctx))
	require.NoError(t, ctrl.SweepOrphanedSandboxes(ctx))

	assert.Equal(t, compute_v1alpha.STOPPED, getSandboxStatus(t, ctx, server, freshOrphan),
		"a freshly orphaned sandbox should survive the node failure grace period")
}

func TestDisabledNodeSkipsGracePeriod(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create a DISABLED node (simulating Drain in progress)
	nodeID := createReadyNode(t, ctx, server.Client, "draining-node", &compute_v1alpha.Node{
		Status: compute_v1alpha.DISABLED,
	})

	sbID := createScheduledSandbox(t, ctx, server, "test-sandbox", nodeID, compute_v1alpha.RUNNING)

	ctrl := NewController(testutils.TestLogger(t), server.EAC)
	ctrl.gracePeriod = 0 // would fire immediately if not skipped
	require.NoError(t, ctrl.Init(ctx))

	reconcileNode(t, ctx, server, ctrl)

	assert.Equal(t, compute_v1alpha.RUNNING, getSandboxStatus(t, ctx, server, sbID),
		"sandbox on DISABLED node should remain RUNNING (drain handles cleanup)")
}

func TestHandledNodeNotRescanned(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	node := &compute_v1alpha.Node{ApiAddress: ":8444"}
	nodeID, err := server.Client.Create(ctx, "test-node", node)
	require.NoError(t, err)

	sbID := createScheduledSandbox(t, ctx, server, "test-sandbox", nodeID, compute_v1alpha.RUNNING)

	ctrl := NewController(testutils.TestLogger(t), server.EAC)
	ctrl.gracePeriod = 0
	require.NoError(t, ctrl.Init(ctx))

	// First reconcile marks sandbox DEAD
	reconcileNode(t, ctx, server, ctrl)
	assert.Equal(t, compute_v1alpha.DEAD, getSandboxStatus(t, ctx, server, sbID))

	// Create a new sandbox on the same dead node (simulating external creation)
	sbNew := createScheduledSandbox(t, ctx, server, "new-sandbox", nodeID, compute_v1alpha.RUNNING)

	// Second reconcile should skip (node already handled)
	reconcileNode(t, ctx, server, ctrl)
	assert.Equal(t, compute_v1alpha.RUNNING, getSandboxStatus(t, ctx, server, sbNew),
		"new sandbox should not be marked DEAD because node was already handled")
}

func TestMultipleNodesIndependent(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Node 1 is healthy
	node1ID := createReadyNode(t, ctx, server.Client, "healthy-node", &compute_v1alpha.Node{
		Status: compute_v1alpha.READY,
	})

	// Node 2 is dead
	node2 := &compute_v1alpha.Node{ApiAddress: ":8445"}
	node2ID, err := server.Client.Create(ctx, "dead-node", node2)
	require.NoError(t, err)

	sb1 := createScheduledSandbox(t, ctx, server, "sb-on-healthy", node1ID, compute_v1alpha.RUNNING)
	sb2 := createScheduledSandbox(t, ctx, server, "sb-on-dead", node2ID, compute_v1alpha.RUNNING)

	ctrl := NewController(testutils.TestLogger(t), server.EAC)
	ctrl.gracePeriod = 0 // immediate
	require.NoError(t, ctrl.Init(ctx))

	reconcileNode(t, ctx, server, ctrl)

	assert.Equal(t, compute_v1alpha.RUNNING, getSandboxStatus(t, ctx, server, sb1),
		"sandbox on healthy node should remain RUNNING")
	assert.Equal(t, compute_v1alpha.DEAD, getSandboxStatus(t, ctx, server, sb2),
		"sandbox on dead node should be marked DEAD")
}
