package sandboxpool

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
)

// TestManagerScaleUpFromZero tests that the manager creates sandboxes
// when the pool has DesiredInstances > 0 and no existing sandboxes
func TestManagerScaleUpFromZero(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create a sandbox pool with desired instances
	pool := &compute_v1alpha.SandboxPool{
		Service:          "web",
		DesiredInstances: 3,
		CurrentInstances: 0,
		ReadyInstances:   0,
		Mode:             compute_v1alpha.AUTO,
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Version: entity.Id("ver-1"),
			Container: []compute_v1alpha.SandboxSpecContainer{
				{
					Image: "test:latest",
					Env:   []string{"TEST=value"},
				},
			},
		},
	}

	poolID, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)
	pool.ID = poolID

	// Create manager and run reconciliation
	manager := NewManager(log, server.EAC)
	err = manager.reconcile(ctx, pool)
	require.NoError(t, err)

	// Verify 3 sandboxes were created
	sandboxes := listSandboxesForPool(t, ctx, server, pool)
	assert.Equal(t, 3, len(sandboxes), "should create 3 sandboxes")

	// Verify all sandboxes have correct labels and specs
	for _, sb := range sandboxes {
		assert.Equal(t, compute_v1alpha.PENDING, sb.Status, "new sandboxes should be PENDING")
		assert.Equal(t, pool.SandboxSpec.Version, sb.Version, "sandbox should use pool version")
		require.NotEmpty(t, sb.Spec.Container, "sandbox should have containers")
		assert.Equal(t, "test:latest", sb.Spec.Container[0].Image, "sandbox should use pool image")

		// Check labels
		var md core_v1alpha.Metadata
		md.Decode(sb.Entity)
		serviceLabel := getLabel(md.Labels, "service")
		assert.Equal(t, "web", serviceLabel, "sandbox should have service label")
		poolLabel := getLabel(md.Labels, "pool")
		assert.Equal(t, poolID.String(), poolLabel, "sandbox should have pool label")
	}

	// Verify pool status was updated
	updatedPool := getPool(t, ctx, server, poolID)
	assert.Equal(t, int64(3), updatedPool.CurrentInstances, "pool should reflect current count")
	assert.Equal(t, int64(0), updatedPool.ReadyInstances, "no sandboxes are running yet")
}

// TestManagerScaleUpPartial tests that the manager only creates
// the difference when some sandboxes already exist
func TestManagerScaleUpPartial(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create pool
	pool := &compute_v1alpha.SandboxPool{
		Service:          "api",
		DesiredInstances: 5,
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Version: entity.Id("ver-1"),
			Container: []compute_v1alpha.SandboxSpecContainer{
				{Image: "test:latest"},
			},
		},
	}

	poolID, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)
	pool.ID = poolID

	// Create 2 existing sandboxes for this pool
	for i := 0; i < 2; i++ {
		sb := &compute_v1alpha.Sandbox{
			Status:  compute_v1alpha.RUNNING,
			Version: pool.SandboxSpec.Version,
			Spec:    pool.SandboxSpec,
		}
		_, err := server.Client.Create(ctx, fmt.Sprintf("existing-sb-%d", i), sb,
			entityserver.WithLabels(types.LabelSet("service", "api", "pool", poolID.String())))
		require.NoError(t, err)
	}

	// Run reconciliation
	manager := NewManager(log, server.EAC)
	err = manager.reconcile(ctx, pool)
	require.NoError(t, err)

	// Verify total is now 5 (2 existing + 3 new)
	sandboxes := listSandboxesForPool(t, ctx, server, pool)
	assert.Equal(t, 5, len(sandboxes), "should have 5 total sandboxes")

	// Count RUNNING vs PENDING
	running := 0
	pending := 0
	for _, sb := range sandboxes {
		switch sb.Status {
		case compute_v1alpha.RUNNING:
			running++
		case compute_v1alpha.PENDING:
			pending++
		}
	}

	assert.Equal(t, 2, running, "should have 2 running (existing)")
	assert.Equal(t, 3, pending, "should have 3 pending (newly created)")

	// Verify pool status
	updatedPool := getPool(t, ctx, server, poolID)
	assert.Equal(t, int64(5), updatedPool.CurrentInstances)
	assert.Equal(t, int64(2), updatedPool.ReadyInstances, "only RUNNING sandboxes are ready")
}

// TestManagerServiceIsolation tests that pools for different services
// don't interfere with each other
func TestManagerServiceIsolation(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create two pools for different services
	pool1 := &compute_v1alpha.SandboxPool{
		Service:          "web",
		DesiredInstances: 2,
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Version: entity.Id("ver-1"),
			Container: []compute_v1alpha.SandboxSpecContainer{
				{Image: "test:latest"},
			},
		},
	}
	pool1ID, err := server.Client.Create(ctx, "pool-web", pool1)
	require.NoError(t, err)
	pool1.ID = pool1ID

	pool2 := &compute_v1alpha.SandboxPool{
		Service:          "worker",
		DesiredInstances: 3,
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Version: entity.Id("ver-1"),
			Container: []compute_v1alpha.SandboxSpecContainer{
				{Image: "test:latest"},
			},
		},
	}
	pool2ID, err := server.Client.Create(ctx, "pool-worker", pool2)
	require.NoError(t, err)
	pool2.ID = pool2ID

	// Run reconciliation for both
	manager := NewManager(log, server.EAC)
	err = manager.reconcile(ctx, pool1)
	require.NoError(t, err)
	err = manager.reconcile(ctx, pool2)
	require.NoError(t, err)

	// Verify each pool has correct count
	webSandboxes := listSandboxesForPool(t, ctx, server, pool1)
	workerSandboxes := listSandboxesForPool(t, ctx, server, pool2)

	assert.Equal(t, 2, len(webSandboxes), "web pool should have 2 sandboxes")
	assert.Equal(t, 3, len(workerSandboxes), "worker pool should have 3 sandboxes")

	// Verify sandboxes have correct service labels
	for _, sb := range webSandboxes {
		var md core_v1alpha.Metadata
		md.Decode(sb.Entity)
		assert.Equal(t, "web", getLabel(md.Labels, "service"))
	}

	for _, sb := range workerSandboxes {
		var md core_v1alpha.Metadata
		md.Decode(sb.Entity)
		assert.Equal(t, "worker", getLabel(md.Labels, "service"))
	}
}

// TestManagerVersionFiltering tests that only sandboxes with matching
// version are counted
func TestManagerVersionFiltering(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	pool := &compute_v1alpha.SandboxPool{
		Service:          "web",
		DesiredInstances: 3,
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Version: entity.Id("ver-2"),
			Container: []compute_v1alpha.SandboxSpecContainer{
				{Image: "test:v2"},
			},
		},
	}

	poolID, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)
	pool.ID = poolID

	// Create 2 sandboxes with old version (should be ignored)
	for i := 0; i < 2; i++ {
		sb := &compute_v1alpha.Sandbox{
			Status:  compute_v1alpha.RUNNING,
			Version: entity.Id("ver-1"), // Old version
			Spec: compute_v1alpha.SandboxSpec{
				Version: entity.Id("ver-1"),
				Container: []compute_v1alpha.SandboxSpecContainer{
					{Image: "test:v1"},
				},
			},
		}
		_, err := server.Client.Create(ctx, "old-sb", sb,
			entityserver.WithLabels(types.LabelSet("service", "web", "pool", poolID.String())))
		require.NoError(t, err)
	}

	// Run reconciliation
	manager := NewManager(log, server.EAC)
	err = manager.reconcile(ctx, pool)
	require.NoError(t, err)

	// Should create 3 new sandboxes (old version doesn't count)
	sandboxes := listSandboxesForPool(t, ctx, server, pool)
	ver2Count := 0
	for _, sb := range sandboxes {
		if sb.Version.String() == "ver-2" {
			ver2Count++
		}
	}

	assert.Equal(t, 3, ver2Count, "should create 3 sandboxes with new version")

	// Verify pool status counts only ver-2 sandboxes
	updatedPool := getPool(t, ctx, server, poolID)
	assert.Equal(t, int64(3), updatedPool.CurrentInstances, "should count only matching version")
}

// TestManagerStatusOnlyUpdate tests that reconciliation doesn't create
// sandboxes when actual == desired, but still updates status
func TestManagerStatusOnlyUpdate(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	pool := &compute_v1alpha.SandboxPool{
		Service:          "web",
		DesiredInstances: 2,
		CurrentInstances: 0, // Stale status
		ReadyInstances:   0,
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Version: entity.Id("ver-1"),
			Container: []compute_v1alpha.SandboxSpecContainer{
				{Image: "test:latest"},
			},
		},
	}

	poolID, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)
	pool.ID = poolID

	// Create 2 existing sandboxes (1 running, 1 pending)
	sb1 := &compute_v1alpha.Sandbox{
		Status:  compute_v1alpha.RUNNING,
		Version: pool.SandboxSpec.Version,
		Spec:    pool.SandboxSpec,
	}
	_, err = server.Client.Create(ctx, "sb1", sb1,
		entityserver.WithLabels(types.LabelSet("service", "web", "pool", poolID.String())))
	require.NoError(t, err)

	sb2 := &compute_v1alpha.Sandbox{
		Status:  compute_v1alpha.PENDING,
		Version: pool.SandboxSpec.Version,
		Spec:    pool.SandboxSpec,
	}
	_, err = server.Client.Create(ctx, "sb2", sb2,
		entityserver.WithLabels(types.LabelSet("service", "web", "pool", poolID.String())))
	require.NoError(t, err)

	// Run reconciliation
	manager := NewManager(log, server.EAC)
	err = manager.reconcile(ctx, pool)
	require.NoError(t, err)

	// Should not create new sandboxes
	sandboxes := listSandboxesForPool(t, ctx, server, pool)
	assert.Equal(t, 2, len(sandboxes), "should not create new sandboxes")

	// Verify pool status was updated correctly
	updatedPool := getPool(t, ctx, server, poolID)
	assert.Equal(t, int64(2), updatedPool.CurrentInstances, "should update current count")
	assert.Equal(t, int64(1), updatedPool.ReadyInstances, "should count only RUNNING")
}

// TestManagerNoUpdateWhenStatusUnchanged tests that Put is not called
// when status values haven't changed
func TestManagerNoUpdateWhenStatusUnchanged(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	pool := &compute_v1alpha.SandboxPool{
		Service:          "web",
		DesiredInstances: 1,
		CurrentInstances: 1,
		ReadyInstances:   1,
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Version: entity.Id("ver-1"),
			Container: []compute_v1alpha.SandboxSpecContainer{
				{Image: "test:latest"},
			},
		},
	}

	poolID, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)
	pool.ID = poolID

	// Create 1 running sandbox
	sb := &compute_v1alpha.Sandbox{
		Status:  compute_v1alpha.RUNNING,
		Version: pool.SandboxSpec.Version,
		Spec:    pool.SandboxSpec,
	}
	_, err = server.Client.Create(ctx, "sb", sb,
		entityserver.WithLabels(types.LabelSet("service", "web", "pool", poolID.String())))
	require.NoError(t, err)

	// Wait a bit to ensure any processing completes
	time.Sleep(10 * time.Millisecond)

	// Run reconciliation
	manager := NewManager(log, server.EAC)
	err = manager.reconcile(ctx, pool)
	require.NoError(t, err)

	// Get pool again and verify status is correct
	finalPool := getPool(t, ctx, server, poolID)

	// Status should still be correct
	assert.Equal(t, int64(1), finalPool.CurrentInstances)
	assert.Equal(t, int64(1), finalPool.ReadyInstances)

	// The implementation should skip Put when values unchanged,
	// but we can't easily verify this without instrumentation.
	// At minimum, verify values are still correct.
}

// Helper functions

type sandboxWithEntity struct {
	compute_v1alpha.Sandbox
	*entity.Entity
}

func listSandboxesForPool(t *testing.T, ctx context.Context, server *testutils.InMemEntityServer, pool *compute_v1alpha.SandboxPool) []*sandboxWithEntity {
	results, err := server.EAC.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox))
	require.NoError(t, err)

	var sandboxes []*sandboxWithEntity

	for _, ent := range results.Values() {
		var sb compute_v1alpha.Sandbox
		sb.Decode(ent.Entity())

		// Filter by version and service
		if sb.Version.String() != pool.SandboxSpec.Version.String() {
			continue
		}

		var md core_v1alpha.Metadata
		md.Decode(ent.Entity())

		serviceLabel := getLabel(md.Labels, "service")
		if serviceLabel != pool.Service {
			continue
		}

		sandboxes = append(sandboxes, &sandboxWithEntity{
			Sandbox: sb,
			Entity:  ent.Entity(),
		})
	}

	return sandboxes
}

func getPool(t *testing.T, ctx context.Context, server *testutils.InMemEntityServer, poolID entity.Id) *compute_v1alpha.SandboxPool {
	resp, err := server.EAC.Get(ctx, poolID.String())
	require.NoError(t, err)

	var pool compute_v1alpha.SandboxPool
	pool.Decode(resp.Entity().Entity())

	return &pool
}

func getLabel(labels types.Labels, key string) string {
	val, _ := labels.Get(key)
	return val
}
