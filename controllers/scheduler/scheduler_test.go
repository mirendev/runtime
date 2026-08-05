package scheduler

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
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

// createReadyNode creates a node entity with Status: READY using a session,
// matching the production pattern where the runner sets its own status via a
// session-scoped attribute.
func createReadyNode(t *testing.T, ctx context.Context, client *entityserver.Client, name string, node *compute_v1alpha.Node) entity.Id {
	t.Helper()

	// Create the node without the session-scoped status attribute
	status := node.Status
	node.Status = ""
	if node.ApiAddress == "" {
		node.ApiAddress = ":8444"
	}
	nodeID, err := client.Create(ctx, name, node)
	require.NoError(t, err)

	// Set status via a session-enabled client (status is session-scoped)
	_, sc, err := client.NewSession(ctx, "test node health")
	require.NoError(t, err)

	err = sc.UpdateAttrs(ctx, nodeID, (&compute_v1alpha.Node{Status: status}).Encode)
	require.NoError(t, err)

	return nodeID
}

// TestSchedulerAssignsUnscheduledSandbox tests that the scheduler assigns
// a node to a sandbox that doesn't have a ScheduleKey
func TestSchedulerAssignsUnscheduledSandbox(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create a ready node (status is session-scoped, so use the helper)
	nodeID := createReadyNode(t, ctx, server.Client, "test-node", &compute_v1alpha.Node{
		Status: compute_v1alpha.READY,
	})

	// Create scheduler and initialize (gathers nodes)
	scheduler := NewController(log, server.EAC)
	err := scheduler.Init(ctx)
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

// TestSchedulerSkipsCordonedNode verifies that a cordoned node (READY, but with
// a persistent Scheduling=cordoned) is not a scheduling candidate: an
// unscheduled sandbox lands on the uncordoned node instead.
func TestSchedulerSkipsCordonedNode(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// A READY but cordoned node. Scheduling is a persistent (non-session)
	// attribute, so it is written directly at create time.
	createReadyNode(t, ctx, server.Client, "cordoned-node", &compute_v1alpha.Node{
		Status:     compute_v1alpha.READY,
		Scheduling: compute_v1alpha.CORDONED,
	})
	// A plain READY node that should receive the work.
	readyID := createReadyNode(t, ctx, server.Client, "ready-node", &compute_v1alpha.Node{
		Status: compute_v1alpha.READY,
	})

	scheduler := NewController(log, server.EAC)
	require.NoError(t, scheduler.Init(ctx))

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

	reconcileSandbox(t, ctx, server, scheduler, sandboxID)

	resp, err := server.EAC.Get(ctx, sandboxID.String())
	require.NoError(t, err)

	var schedule compute_v1alpha.Schedule
	schedule.Decode(resp.Entity().Entity())
	assert.Equal(t, readyID, schedule.Key.Node, "sandbox should avoid the cordoned node")
}

// TestSchedulerSkipsAlreadyScheduledSandbox tests that the scheduler
// doesn't re-assign a sandbox that already has a ScheduleKey
func TestSchedulerSkipsAlreadyScheduledSandbox(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create two ready nodes
	node1ID := createReadyNode(t, ctx, server.Client, "node-1", &compute_v1alpha.Node{
		Status: compute_v1alpha.READY,
	})
	createReadyNode(t, ctx, server.Client, "node-2", &compute_v1alpha.Node{
		Status: compute_v1alpha.READY,
	})

	// Create scheduler and initialize
	scheduler := NewController(log, server.EAC)
	err := scheduler.Init(ctx)
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
	createReadyNode(t, ctx, server.Client, "disabled-node", &compute_v1alpha.Node{
		Status: compute_v1alpha.DISABLED,
	})

	// Create scheduler and initialize
	scheduler := NewController(log, server.EAC)
	err := scheduler.Init(ctx)
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

	// Create multiple ready nodes. Each needs a distinct name: Client.Create
	// derives the entity id from node/<name>, so empty names would collapse all
	// three onto the same id (and now conflict, since CreateEntity is
	// put-if-absent on every backend).
	nodeIDs := make(map[entity.Id]bool)
	for i := range 3 {
		nodeID := createReadyNode(t, ctx, server.Client, fmt.Sprintf("node-%d", i), &compute_v1alpha.Node{
			Status: compute_v1alpha.READY,
		})
		nodeIDs[nodeID] = true
	}

	// Create scheduler and initialize
	scheduler := NewController(log, server.EAC)
	err := scheduler.Init(ctx)
	require.NoError(t, err)

	// Create and schedule multiple sandboxes
	for i := range 5 {
		sandbox := &compute_v1alpha.Sandbox{
			Status: compute_v1alpha.PENDING,
		}
		sandboxID, err := server.Client.Create(ctx, fmt.Sprintf("sandbox-%d", i), sandbox)
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

// TestSchedulerStatefulSandboxGoesToCoordinator tests that stateful sandboxes
// (those with miren disk volumes) are scheduled to the coordinator node
func TestSchedulerStatefulSandboxGoesToCoordinator(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create a coordinator node (role=coordinator constraint)
	coordNodeID := createReadyNode(t, ctx, server.Client, "coordinator", &compute_v1alpha.Node{
		Status:      compute_v1alpha.READY,
		Constraints: types.LabelSet("role", "coordinator"),
	})

	// Create a runner node
	createReadyNode(t, ctx, server.Client, "runner", &compute_v1alpha.Node{
		Status:   compute_v1alpha.READY,
		RunnerId: "550e8400-e29b-41d4-a716-446655440000",
	})

	// Create scheduler and initialize
	scheduler := NewController(log, server.EAC)
	err := scheduler.Init(ctx)
	require.NoError(t, err)

	// Create a stateful sandbox (has miren disk volume)
	sandbox := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.PENDING,
		Spec: compute_v1alpha.SandboxSpec{
			Volume: []compute_v1alpha.SandboxSpecVolume{
				{
					Name:     "data",
					Provider: "miren",
					DiskName: "my-disk",
				},
			},
			Container: []compute_v1alpha.SandboxSpecContainer{
				{Image: "test:latest"},
			},
		},
	}
	sandboxID, err := server.Client.Create(ctx, "stateful-sandbox", sandbox)
	require.NoError(t, err)

	// Run reconciliation
	reconcileSandbox(t, ctx, server, scheduler, sandboxID)

	// Verify the stateful sandbox was assigned to the coordinator
	resp, err := server.EAC.Get(ctx, sandboxID.String())
	require.NoError(t, err)

	var schedule compute_v1alpha.Schedule
	schedule.Decode(resp.Entity().Entity())
	assert.Equal(t, coordNodeID, schedule.Key.Node, "stateful sandbox should be assigned to coordinator")
}

// TestSchedulerStatelessSandboxPrefersRunners tests that stateless sandboxes
// prefer runner nodes over the coordinator when runners are available
func TestSchedulerStatelessSandboxPrefersRunners(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create a coordinator node (role=coordinator constraint)
	coordNodeID := createReadyNode(t, ctx, server.Client, "coordinator", &compute_v1alpha.Node{
		Status:      compute_v1alpha.READY,
		Constraints: types.LabelSet("role", "coordinator"),
	})

	// Create multiple runner nodes. Each needs a distinct name: Client.Create
	// derives the entity id from node/<name>, so empty names would collapse all
	// three onto the same id (and now conflict, since CreateEntity is
	// put-if-absent on every backend).
	runnerIDs := make(map[entity.Id]bool)
	for i := range 3 {
		runnerID := createReadyNode(t, ctx, server.Client, fmt.Sprintf("runner-%d", i), &compute_v1alpha.Node{
			Status:   compute_v1alpha.READY,
			RunnerId: "550e8400-e29b-41d4-a716-44665544000" + string(rune('0'+i)),
		})
		runnerIDs[runnerID] = true
	}

	// Create scheduler and initialize
	scheduler := NewController(log, server.EAC)
	err := scheduler.Init(ctx)
	require.NoError(t, err)

	// Create and schedule multiple stateless sandboxes
	for i := range 10 {
		sandbox := &compute_v1alpha.Sandbox{
			Status: compute_v1alpha.PENDING,
			Spec: compute_v1alpha.SandboxSpec{
				Container: []compute_v1alpha.SandboxSpecContainer{
					{Image: "test:latest"},
				},
			},
		}
		sandboxID, err := server.Client.Create(ctx, fmt.Sprintf("sandbox-%d", i), sandbox)
		require.NoError(t, err)

		reconcileSandbox(t, ctx, server, scheduler, sandboxID)

		// Verify the stateless sandbox was assigned to a runner, not the coordinator
		resp, err := server.EAC.Get(ctx, sandboxID.String())
		require.NoError(t, err)

		var schedule compute_v1alpha.Schedule
		schedule.Decode(resp.Entity().Entity())
		assert.True(t, runnerIDs[schedule.Key.Node], "stateless sandbox should be assigned to a runner")
		assert.NotEqual(t, coordNodeID, schedule.Key.Node, "stateless sandbox should NOT be assigned to coordinator")
	}
}

// TestSchedulerStatelessFallsBackToCoordinator tests that stateless sandboxes
// fall back to the coordinator when no runners are available
func TestSchedulerStatelessFallsBackToCoordinator(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create only a coordinator node (no runners)
	coordNodeID := createReadyNode(t, ctx, server.Client, "coordinator", &compute_v1alpha.Node{
		Status:      compute_v1alpha.READY,
		Constraints: types.LabelSet("role", "coordinator"),
	})

	// Create scheduler and initialize
	scheduler := NewController(log, server.EAC)
	err := scheduler.Init(ctx)
	require.NoError(t, err)

	// Create a stateless sandbox
	sandbox := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.PENDING,
		Spec: compute_v1alpha.SandboxSpec{
			Container: []compute_v1alpha.SandboxSpecContainer{
				{Image: "test:latest"},
			},
		},
	}
	sandboxID, err := server.Client.Create(ctx, "stateless-sandbox", sandbox)
	require.NoError(t, err)

	// Run reconciliation
	reconcileSandbox(t, ctx, server, scheduler, sandboxID)

	// Verify the stateless sandbox falls back to coordinator when no runners available
	resp, err := server.EAC.Get(ctx, sandboxID.String())
	require.NoError(t, err)

	var schedule compute_v1alpha.Schedule
	schedule.Decode(resp.Entity().Entity())
	assert.Equal(t, coordNodeID, schedule.Key.Node, "stateless sandbox should fall back to coordinator when no runners")
}

// TestIsStatefulDetection tests the isStateful helper function
func TestIsStatefulDetection(t *testing.T) {
	log := testutils.TestLogger(t)
	scheduler := NewController(log, nil)

	tests := []struct {
		name     string
		sandbox  *compute_v1alpha.Sandbox
		expected bool
	}{
		{
			name: "stateful - spec volume with miren provider",
			sandbox: &compute_v1alpha.Sandbox{
				Spec: compute_v1alpha.SandboxSpec{
					Volume: []compute_v1alpha.SandboxSpecVolume{
						{Name: "data", Provider: "miren"},
					},
				},
			},
			expected: true,
		},
		{
			name: "stateful - legacy volume with miren provider",
			sandbox: &compute_v1alpha.Sandbox{
				Volume: []compute_v1alpha.Volume{
					{Name: "data", Provider: "miren"},
				},
			},
			expected: true,
		},
		{
			name: "stateless - no volumes",
			sandbox: &compute_v1alpha.Sandbox{
				Spec: compute_v1alpha.SandboxSpec{
					Container: []compute_v1alpha.SandboxSpecContainer{
						{Image: "test:latest"},
					},
				},
			},
			expected: false,
		},
		{
			name: "stateful - volume with local provider",
			sandbox: &compute_v1alpha.Sandbox{
				Spec: compute_v1alpha.SandboxSpec{
					Volume: []compute_v1alpha.SandboxSpecVolume{
						{Name: "data", Provider: "local"},
					},
				},
			},
			expected: true,
		},
		{
			name: "stateful - mixed volumes with one miren",
			sandbox: &compute_v1alpha.Sandbox{
				Spec: compute_v1alpha.SandboxSpec{
					Volume: []compute_v1alpha.SandboxSpecVolume{
						{Name: "cache", Provider: "local"},
						{Name: "data", Provider: "miren"},
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scheduler.isStateful(tt.sandbox)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// createPreScheduledSandbox creates a sandbox already assigned to a node, used
// to seed sibling state for spread tests.
func createPreScheduledSandbox(t *testing.T, ctx context.Context, server *testutils.InMemEntityServer, name, poolID string, nodeID entity.Id, status compute_v1alpha.SandboxStatus) entity.Id {
	t.Helper()

	sb := &compute_v1alpha.Sandbox{
		Status: status,
		Spec: compute_v1alpha.SandboxSpec{
			Container: []compute_v1alpha.SandboxSpecContainer{
				{Image: "test:latest"},
			},
		},
	}
	sbID, err := server.Client.Create(ctx, name, sb,
		entityserver.WithLabels(types.LabelSet("service", "web", "pool", poolID)))
	require.NoError(t, err)

	// Patch in the schedule via the entity server so cardinality-aware merge
	// keeps the sandbox's existing entity/kind alongside the new schedule kind.
	schedule := compute_v1alpha.Schedule{
		Key: compute_v1alpha.Key{
			Kind: compute_v1alpha.KindSandbox,
			Node: nodeID,
		},
	}
	err = server.Client.Patch(ctx, sbID, 0, schedule.Encode()...)
	require.NoError(t, err)

	return sbID
}

// TestSchedulerSpreadsReplicasAcrossRunners is the MIR-1143 regression: with
// two runners and two siblings in the same pool, both replicas must not pile
// onto the same node. Repeats enough times to catch the random-collision case
// that surfaced the bug.
func TestSchedulerSpreadsReplicasAcrossRunners(t *testing.T) {
	for trial := range 20 {
		t.Run(fmt.Sprintf("trial-%d", trial), func(t *testing.T) {
			ctx := context.Background()
			log := testutils.TestLogger(t)

			server, cleanup := testutils.NewInMemEntityServer(t)
			defer cleanup()

			runner1 := createReadyNode(t, ctx, server.Client, "runner-1", &compute_v1alpha.Node{
				Status:   compute_v1alpha.READY,
				RunnerId: "550e8400-e29b-41d4-a716-446655440001",
			})
			runner2 := createReadyNode(t, ctx, server.Client, "runner-2", &compute_v1alpha.Node{
				Status:   compute_v1alpha.READY,
				RunnerId: "550e8400-e29b-41d4-a716-446655440002",
			})

			scheduler := NewController(log, server.EAC)
			require.NoError(t, scheduler.Init(ctx))

			poolID := "pool-spread-test"

			placements := make(map[entity.Id]int)
			for i := range 2 {
				sb := &compute_v1alpha.Sandbox{
					Status: compute_v1alpha.PENDING,
					Spec: compute_v1alpha.SandboxSpec{
						Container: []compute_v1alpha.SandboxSpecContainer{
							{Image: "test:latest"},
						},
					},
				}
				sbID, err := server.Client.Create(ctx, fmt.Sprintf("sb-%d", i), sb,
					entityserver.WithLabels(types.LabelSet("service", "web", "pool", poolID)))
				require.NoError(t, err)

				reconcileSandbox(t, ctx, server, scheduler, sbID)

				resp, err := server.EAC.Get(ctx, sbID.String())
				require.NoError(t, err)
				var schedule compute_v1alpha.Schedule
				schedule.Decode(resp.Entity().Entity())
				placements[schedule.Key.Node]++
			}

			assert.Equal(t, 1, placements[runner1], "runner-1 should host exactly one replica")
			assert.Equal(t, 1, placements[runner2], "runner-2 should host exactly one replica")
		})
	}
}

// TestSchedulerPacksWhenReplicasExceedRunners verifies the soft fallback: with
// more replicas than runners we don't refuse to schedule, we just pack.
func TestSchedulerPacksWhenReplicasExceedRunners(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	runner1 := createReadyNode(t, ctx, server.Client, "runner-1", &compute_v1alpha.Node{
		Status:   compute_v1alpha.READY,
		RunnerId: "550e8400-e29b-41d4-a716-446655440001",
	})
	runner2 := createReadyNode(t, ctx, server.Client, "runner-2", &compute_v1alpha.Node{
		Status:   compute_v1alpha.READY,
		RunnerId: "550e8400-e29b-41d4-a716-446655440002",
	})

	scheduler := NewController(log, server.EAC)
	require.NoError(t, scheduler.Init(ctx))

	poolID := "pool-pack-test"

	placements := make(map[entity.Id]int)
	for i := range 5 {
		sb := &compute_v1alpha.Sandbox{
			Status: compute_v1alpha.PENDING,
			Spec: compute_v1alpha.SandboxSpec{
				Container: []compute_v1alpha.SandboxSpecContainer{
					{Image: "test:latest"},
				},
			},
		}
		sbID, err := server.Client.Create(ctx, fmt.Sprintf("sb-%d", i), sb,
			entityserver.WithLabels(types.LabelSet("service", "web", "pool", poolID)))
		require.NoError(t, err)

		reconcileSandbox(t, ctx, server, scheduler, sbID)

		resp, err := server.EAC.Get(ctx, sbID.String())
		require.NoError(t, err)
		var schedule compute_v1alpha.Schedule
		schedule.Decode(resp.Entity().Entity())
		placements[schedule.Key.Node]++
	}

	assert.Equal(t, 5, placements[runner1]+placements[runner2], "all replicas should be scheduled")
	// With 5 replicas over 2 runners the soft-spread keeps each node within 1
	// of perfect balance (i.e. 3/2 or 2/3 — no 5/0 or 4/1).
	diff := placements[runner1] - placements[runner2]
	if diff < 0 {
		diff = -diff
	}
	assert.LessOrEqual(t, diff, 1, "soft spread should balance within 1: got %d/%d",
		placements[runner1], placements[runner2])
}

// TestSchedulerSpreadIgnoresTerminalSiblings ensures STOPPED and DEAD siblings
// don't influence placement — they're not actually consuming the node.
func TestSchedulerSpreadIgnoresTerminalSiblings(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	runner1 := createReadyNode(t, ctx, server.Client, "runner-1", &compute_v1alpha.Node{
		Status:   compute_v1alpha.READY,
		RunnerId: "550e8400-e29b-41d4-a716-446655440001",
	})
	runner2 := createReadyNode(t, ctx, server.Client, "runner-2", &compute_v1alpha.Node{
		Status:   compute_v1alpha.READY,
		RunnerId: "550e8400-e29b-41d4-a716-446655440002",
	})

	poolID := "pool-terminal-test"

	// runner-1 has a STOPPED sibling (should be ignored).
	// runner-2 has a RUNNING sibling (should count).
	// A fresh replica should land on runner-1 because it has 0 active siblings.
	createPreScheduledSandbox(t, ctx, server, "stopped-sb", poolID, runner1, compute_v1alpha.STOPPED)
	createPreScheduledSandbox(t, ctx, server, "running-sb", poolID, runner2, compute_v1alpha.RUNNING)

	scheduler := NewController(log, server.EAC)
	require.NoError(t, scheduler.Init(ctx))

	sb := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.PENDING,
		Spec: compute_v1alpha.SandboxSpec{
			Container: []compute_v1alpha.SandboxSpecContainer{
				{Image: "test:latest"},
			},
		},
	}
	sbID, err := server.Client.Create(ctx, "fresh-sb", sb,
		entityserver.WithLabels(types.LabelSet("service", "web", "pool", poolID)))
	require.NoError(t, err)

	reconcileSandbox(t, ctx, server, scheduler, sbID)

	resp, err := server.EAC.Get(ctx, sbID.String())
	require.NoError(t, err)
	var schedule compute_v1alpha.Schedule
	schedule.Decode(resp.Entity().Entity())
	assert.Equal(t, runner1, schedule.Key.Node,
		"fresh replica should prefer runner-1 (0 active siblings) over runner-2 (1 active sibling)")
}
