package deployment

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

// TestPoolCreationFixedMode tests that DeploymentLauncher creates pools with
// correct desired_instances for fixed-mode services
func TestPoolCreationFixedMode(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app
	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	// Create version with fixed-mode service
	version := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "test:latest",
		Config: core_v1alpha.Config{
			Port: 3000,
			Services: []core_v1alpha.Services{
				{
					Name: "postgres",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 2,
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", version)
	require.NoError(t, err)
	version.ID = verID

	// Set as active version
	app.ActiveVersion = version.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	// Create launcher and handle reconcile
	launcher := NewLauncher(log, server.EAC)

	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Verify pool was created with correct desired_instances
	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1, "should create one pool")

	pool := pools[0]
	assert.Equal(t, "postgres", pool.Service, "pool should be for postgres service")
	assert.Equal(t, int64(2), pool.DesiredInstances, "fixed mode should set desired_instances to 2")
	assert.Equal(t, version.ID, pool.SandboxSpec.Version, "pool should reference version")

	// Verify pool is referenced by version
	assert.Contains(t, pool.ReferencedByVersions, version.ID, "pool should be referenced by version")
}

// TestPoolCreationAutoMode tests that DeploymentLauncher creates pools with
// desired_instances=1 for auto-mode services to boot immediately after deploy
func TestPoolCreationAutoMode(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app
	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	// Create version with auto-mode service
	version := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "test:latest",
		Config: core_v1alpha.Config{
			Port: 3000,
			Services: []core_v1alpha.Services{
				{
					Name: "web",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:                "auto",
						RequestsPerInstance: 10,
						ScaleDownDelay:      "15m",
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", version)
	require.NoError(t, err)
	version.ID = verID

	// Set as active version
	app.ActiveVersion = version.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	// Create launcher and handle reconcile
	launcher := NewLauncher(log, server.EAC)

	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Verify pool was created with desired_instances=1
	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1, "should create one pool")

	pool := pools[0]
	assert.Equal(t, "web", pool.Service, "pool should be for web service")
	assert.Equal(t, int64(1), pool.DesiredInstances, "auto mode should start with desired_instances=1 to boot immediately")
	assert.Equal(t, version.ID, pool.SandboxSpec.Version, "pool should reference version")
}

// TestPoolReuseOnConfigChange tests that DeploymentLauncher reuses existing
// pools when SandboxSpec matches (e.g., only concurrency settings changed)
func TestPoolReuseOnConfigChange(t *testing.T) {
	ctx := context.Background()
	log := slog.Default() // testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app
	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	// Create v1 with postgres:16
	v1 := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "oci.miren.cloud/postgres:16",
		Config: core_v1alpha.Config{
			Port: 5432,
			Variable: []core_v1alpha.Variable{
				{Key: "DB_NAME", Value: "mydb"},
			},
			Services: []core_v1alpha.Services{
				{
					Name: "postgres",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 1,
					},
				},
			},
		},
	}
	v1ID, err := server.Client.Create(ctx, "test-v1", v1)
	require.NoError(t, err)
	v1.ID = v1ID

	// Deploy v1
	app.ActiveVersion = v1.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	launcher := NewLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Get the pool created for v1
	poolsV1 := listAllPools(t, ctx, server)
	require.Len(t, poolsV1, 1, "should create one pool for v1")
	poolV1ID := poolsV1[0].ID
	assert.Equal(t, int64(1), poolsV1[0].DesiredInstances, "v1 pool should have DesiredInstances=1 for fixed mode")

	// Create v2 with same image and env vars, only concurrency settings changed
	v2 := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v2",
		ImageUrl: "oci.miren.cloud/postgres:16", // Same image
		Config: core_v1alpha.Config{
			Port: 5432,
			Variable: []core_v1alpha.Variable{
				{Key: "DB_NAME", Value: "mydb"}, // Same env vars
			},
			Services: []core_v1alpha.Services{
				{
					Name: "postgres",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 2, // Changed from 1 to 2 (config-only change, doesn't affect spec)
					},
				},
			},
		},
	}
	v2ID, err := server.Client.Create(ctx, "test-v2", v2)
	require.NoError(t, err)
	v2.ID = v2ID

	// Deploy v2
	app.ActiveVersion = v2.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Verify same pool is reused
	poolsV2 := listAllPools(t, ctx, server)
	require.Len(t, poolsV2, 1, "should still have only one pool (reused)")

	pool := poolsV2[0]
	assert.Equal(t, poolV1ID, pool.ID, "should reuse the same pool ID")
	assert.Contains(t, pool.ReferencedByVersions, v1.ID, "pool should still reference v1")
	assert.Contains(t, pool.ReferencedByVersions, v2.ID, "pool should now also reference v2")
	assert.Len(t, pool.ReferencedByVersions, 2, "pool should reference both versions")

	// CRITICAL: When reusing a pool, DesiredInstances should be updated to match new version's concurrency settings
	assert.Equal(t, int64(2), pool.DesiredInstances, "pool should update DesiredInstances from 1 to 2 when v2 changes NumInstances")
}

// TestNewPoolOnImageChange tests that DeploymentLauncher creates a new pool
// when the image changes (SandboxSpec doesn't match), and scales down the old pool
func TestNewPoolOnImageChange(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app
	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	// Create v1 with postgres:16
	v1 := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "oci.miren.cloud/postgres:16",
		Config: core_v1alpha.Config{
			Port: 5432,
			Services: []core_v1alpha.Services{
				{
					Name: "postgres",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 1,
					},
				},
			},
		},
	}
	v1ID, err := server.Client.Create(ctx, "test-v1", v1)
	require.NoError(t, err)
	v1.ID = v1ID

	// Deploy v1
	app.ActiveVersion = v1.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	launcher := NewLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Get the pool created for v1
	poolsV1 := listAllPools(t, ctx, server)
	require.Len(t, poolsV1, 1, "should create one pool for v1")
	poolV1ID := poolsV1[0].ID

	// Create v2 with postgres:17 (image change)
	v2 := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v2",
		ImageUrl: "oci.miren.cloud/postgres:17", // Image changed
		Config: core_v1alpha.Config{
			Port: 5432,
			Services: []core_v1alpha.Services{
				{
					Name: "postgres",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 1,
					},
				},
			},
		},
	}
	v2ID, err := server.Client.Create(ctx, "test-v2", v2)
	require.NoError(t, err)
	v2.ID = v2ID

	// Deploy v2
	app.ActiveVersion = v2.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Verify new pool was created
	poolsV2 := listAllPools(t, ctx, server)
	require.Len(t, poolsV2, 2, "should have two pools now")

	// Find the new pool
	var poolV2 *compute_v1alpha.SandboxPool
	for i := range poolsV2 {
		if poolsV2[i].ID != poolV1ID {
			poolV2 = &poolsV2[i]
			break
		}
	}
	require.NotNil(t, poolV2, "should find the new pool")

	assert.Equal(t, "postgres", poolV2.Service, "new pool should be for postgres service")
	assert.Contains(t, poolV2.ReferencedByVersions, v2.ID, "new pool should reference v2")
	assert.NotContains(t, poolV2.ReferencedByVersions, v1.ID, "new pool should not reference v1")

	// Verify old pool was scaled down by re-fetching from store
	getRes, err := server.EAC.Get(ctx, poolV1ID.String())
	require.NoError(t, err)
	var poolV1Refreshed compute_v1alpha.SandboxPool
	poolV1Refreshed.Decode(getRes.Entity().Entity())

	t.Logf("Old pool state after refresh: DesiredInstances=%d, ReferencedByVersions=%v",
		poolV1Refreshed.DesiredInstances, poolV1Refreshed.ReferencedByVersions)
	assert.Equal(t, int64(0), poolV1Refreshed.DesiredInstances, "old pool should be scaled to 0")
	assert.NotContains(t, poolV1Refreshed.ReferencedByVersions, v2.ID, "old pool should not reference v2")
	assert.Len(t, poolV1Refreshed.ReferencedByVersions, 0, "old pool should have no version references")
}

// TestServiceRemoval tests that DeploymentLauncher scales down pools
// when services are removed from the config
func TestServiceRemoval(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app
	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	// Create v1 with postgres service
	v1 := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "app:v1",
		Config: core_v1alpha.Config{
			Port: 3000,
			Services: []core_v1alpha.Services{
				{
					Name: "postgres",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 1,
					},
				},
			},
		},
	}
	v1ID, err := server.Client.Create(ctx, "test-v1", v1)
	require.NoError(t, err)
	v1.ID = v1ID

	// Deploy v1
	app.ActiveVersion = v1.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	launcher := NewLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Verify postgres pool was created
	poolsV1 := listAllPools(t, ctx, server)
	require.Len(t, poolsV1, 1, "should create postgres pool")
	assert.Equal(t, "postgres", poolsV1[0].Service)
	assert.Equal(t, int64(1), poolsV1[0].DesiredInstances, "postgres pool should have desired_instances=1")

	// Create v2 without postgres service
	v2 := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v2",
		ImageUrl: "app:v2",
		Config: core_v1alpha.Config{
			Port:     3000,
			Services: []core_v1alpha.Services{}, // No services
		},
	}
	v2ID, err := server.Client.Create(ctx, "test-v2", v2)
	require.NoError(t, err)
	v2.ID = v2ID

	// Deploy v2
	app.ActiveVersion = v2.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Verify postgres pool was scaled to 0 by re-fetching from store
	poolsV2 := listAllPools(t, ctx, server)
	require.Len(t, poolsV2, 1, "pool should still exist")
	poolID := poolsV2[0].ID

	getRes, err := server.EAC.Get(ctx, poolID.String())
	require.NoError(t, err)
	var refreshedPool compute_v1alpha.SandboxPool
	refreshedPool.Decode(getRes.Entity().Entity())

	assert.Equal(t, int64(0), refreshedPool.DesiredInstances, "postgres pool should be scaled to 0")
	assert.NotContains(t, refreshedPool.ReferencedByVersions, v2.ID, "pool should not reference v2")
}

// TestMultipleServices tests that DeploymentLauncher creates pools for
// all services with correct desired_instances
func TestMultipleServices(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app
	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	// Create version with multiple services
	version := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "test:latest",
		Config: core_v1alpha.Config{
			Port: 3000,
			Services: []core_v1alpha.Services{
				{
					Name: "web",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:                "auto",
						RequestsPerInstance: 10,
					},
				},
				{
					Name: "worker",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 3,
					},
				},
				{
					Name: "postgres",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 1,
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", version)
	require.NoError(t, err)
	version.ID = verID

	// Set as active version
	app.ActiveVersion = version.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	// Create launcher and handle reconcile
	launcher := NewLauncher(log, server.EAC)

	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Verify 3 pools were created
	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 3, "should create 3 pools")

	// Find each pool and verify
	poolsByService := make(map[string]*compute_v1alpha.SandboxPool)
	for i := range pools {
		poolsByService[pools[i].Service] = &pools[i]
	}

	// Verify web pool (auto mode)
	webPool, ok := poolsByService["web"]
	require.True(t, ok, "should have web pool")
	assert.Equal(t, int64(1), webPool.DesiredInstances, "web (auto) should start at 1")

	// Verify worker pool (fixed mode, 3 instances)
	workerPool, ok := poolsByService["worker"]
	require.True(t, ok, "should have worker pool")
	assert.Equal(t, int64(3), workerPool.DesiredInstances, "worker (fixed) should start at 3")

	// Verify postgres pool (fixed mode, 1 instance)
	postgresPool, ok := poolsByService["postgres"]
	require.True(t, ok, "should have postgres pool")
	assert.Equal(t, int64(1), postgresPool.DesiredInstances, "postgres (fixed) should start at 1")

	// Verify all pools reference the version
	for _, pool := range pools {
		assert.Contains(t, pool.ReferencedByVersions, version.ID, "all pools should reference version")
	}
}

// TestInMemStoreMultiValuedAttributeUpdate tests whether the inmem store
// properly handles Replace operations with multi-valued attributes
func TestInMemStoreMultiValuedAttributeUpdate(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create a pool with one reference
	pool := &compute_v1alpha.SandboxPool{
		Service:          "postgres",
		DesiredInstances: 1,
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Version: entity.Id("version-1"),
		},
		ReferencedByVersions: []entity.Id{entity.Id("version-1")},
	}

	poolID, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)
	pool.ID = poolID

	// Verify initial state
	initialResp, err := server.EAC.Get(ctx, string(poolID))
	require.NoError(t, err)
	var initialPool compute_v1alpha.SandboxPool
	initialPool.Decode(initialResp.Entity().Entity())
	assert.Len(t, initialPool.ReferencedByVersions, 1, "should have 1 reference initially")
	assert.Contains(t, initialPool.ReferencedByVersions, entity.Id("version-1"))

	// Now update to add a second reference using Replace (simulating what updatePool does)
	poolWithTwoRefs := &compute_v1alpha.SandboxPool{
		Service:              "postgres",
		DesiredInstances:     1,
		SandboxSpec:          pool.SandboxSpec,
		ReferencedByVersions: []entity.Id{entity.Id("version-1"), entity.Id("version-2")},
	}
	poolWithTwoRefs.ID = poolID

	// Get the existing entity
	resp, err := server.EAC.Get(ctx, string(poolID))
	require.NoError(t, err)
	ent := resp.Entity().Entity()

	// Build new attrs from poolWithTwoRefs
	newAttrs := poolWithTwoRefs.Encode()

	// Filter out ReferencedByVersions from encoded attrs - we'll add them separately
	filteredAttrs := make([]entity.Attr, 0, len(newAttrs))
	for _, attr := range newAttrs {
		if attr.ID != compute_v1alpha.SandboxPoolReferencedByVersionsId {
			filteredAttrs = append(filteredAttrs, attr)
		}
	}
	newAttrs = filteredAttrs

	// Build final attrs: metadata from existing + new pool attrs
	finalAttrs := make([]entity.Attr, 0, len(ent.Attrs())+len(newAttrs))

	// Collect IDs we're replacing
	replacingIDs := make(map[entity.Id]bool)
	for _, attr := range newAttrs {
		replacingIDs[attr.ID] = true
	}
	// Always replace ReferencedByVersions since we're explicitly setting them
	replacingIDs[compute_v1alpha.SandboxPoolReferencedByVersionsId] = true

	// Add existing attrs except those we're replacing
	for _, attr := range ent.Attrs() {
		if !replacingIDs[attr.ID] {
			finalAttrs = append(finalAttrs, attr)
		}
	}

	// Add all new attrs
	finalAttrs = append(finalAttrs, newAttrs...)

	// Add all references (multi-valued attribute - can't use entity.Update/Set)
	for _, ref := range poolWithTwoRefs.ReferencedByVersions {
		finalAttrs = append(finalAttrs, entity.Ref(compute_v1alpha.SandboxPoolReferencedByVersionsId, ref))
	}

	// Use Replace with the combined attributes
	_, err = server.EAC.Replace(ctx, finalAttrs, 0)
	require.NoError(t, err)

	// Verify the update persisted
	updatedResp, err := server.EAC.Get(ctx, string(poolID))
	require.NoError(t, err)
	var updatedPool compute_v1alpha.SandboxPool
	updatedPool.Decode(updatedResp.Entity().Entity())

	t.Logf("After update: ReferencedByVersions = %v", updatedPool.ReferencedByVersions)

	// This is the key assertion - does the inmem store preserve both references?
	assert.Len(t, updatedPool.ReferencedByVersions, 2, "should have 2 references after update")
	assert.Contains(t, updatedPool.ReferencedByVersions, entity.Id("version-1"), "should still have version-1")
	assert.Contains(t, updatedPool.ReferencedByVersions, entity.Id("version-2"), "should have version-2")
}

// TestUpdatePoolPreservesMetadata verifies that updatePool doesn't wipe out
// entity metadata like CreatedAt and UpdatedAt when setting zero values
func TestUpdatePoolPreservesMetadata(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create a pool with some initial values
	pool := &compute_v1alpha.SandboxPool{
		Service:          "postgres",
		DesiredInstances: 1,
		CurrentInstances: 1,
		ReadyInstances:   1,
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Version: entity.Id("version-1"),
		},
		ReferencedByVersions: []entity.Id{entity.Id("version-1")},
	}

	poolID, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)
	pool.ID = poolID

	// Get the entity to check initial metadata
	initialResp, err := server.EAC.Get(ctx, string(poolID))
	require.NoError(t, err)
	initialEntity := initialResp.Entity().Entity()

	initialCreatedAt := initialEntity.GetCreatedAt()
	initialUpdatedAt := initialEntity.GetUpdatedAt()
	require.False(t, initialCreatedAt.IsZero(), "pool should have CreatedAt set")
	require.False(t, initialUpdatedAt.IsZero(), "pool should have UpdatedAt set")

	// Now update the pool with zero values (simulating scale-down)
	pool.DesiredInstances = 0
	pool.CurrentInstances = 0
	pool.ReadyInstances = 0
	pool.ReferencedByVersions = []entity.Id{} // Empty refs

	launcher := NewLauncher(log, server.EAC)
	poolWithEntity := &PoolWithEntity{
		Pool:   pool,
		Entity: *initialEntity,
	}
	err = launcher.updatePool(ctx, poolWithEntity)
	require.NoError(t, err)

	// Get the entity again to verify metadata is preserved
	updatedResp, err := server.EAC.Get(ctx, string(poolID))
	require.NoError(t, err)
	updatedEntity := updatedResp.Entity().Entity()

	// Verify metadata was preserved
	assert.Equal(t, initialCreatedAt, updatedEntity.GetCreatedAt(),
		"CreatedAt should be preserved during update")
	assert.GreaterOrEqual(t, updatedEntity.GetUpdatedAt(), initialUpdatedAt,
		"UpdatedAt should be updated but not zeroed")

	// Verify the zero values were actually set
	var updatedPool compute_v1alpha.SandboxPool
	updatedPool.Decode(updatedEntity)
	assert.Equal(t, int64(0), updatedPool.DesiredInstances, "should set DesiredInstances to 0")
	assert.Equal(t, int64(0), updatedPool.CurrentInstances, "should set CurrentInstances to 0")
	assert.Equal(t, int64(0), updatedPool.ReadyInstances, "should set ReadyInstances to 0")
	assert.Empty(t, updatedPool.ReferencedByVersions, "should clear ReferencedByVersions")
}

// TestAutoModePoolReusePreservesDesiredInstances tests that when reusing a pool
// for an auto mode service, the launcher does NOT reset desired_instances.
// For auto mode, the activator manages desired_instances based on traffic,
// so the launcher should not interfere.
func TestAutoModePoolReusePreservesDesiredInstances(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app
	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	// Create version with auto-mode service
	version := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "web:latest",
		Config: core_v1alpha.Config{
			Port: 8080,
			Services: []core_v1alpha.Services{
				{
					Name: "web",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:                "auto",
						RequestsPerInstance: 10,
						ScaleDownDelay:      "15m",
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", version)
	require.NoError(t, err)
	version.ID = verID

	// Set as active version
	app.ActiveVersion = version.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	// First reconciliation - creates pool with desired=1 (boots immediately after deploy)
	launcher := NewLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1, "should create one pool")
	pool := pools[0]
	assert.Equal(t, "web", pool.Service)
	assert.Equal(t, int64(1), pool.DesiredInstances, "auto mode should start with desired=1")

	// Simulate activator scaling up the pool (e.g., traffic arrived)
	pool.DesiredInstances = 2
	err = server.Client.Update(ctx, &pool)
	require.NoError(t, err)

	// Verify pool now has desired=2
	pools = listAllPools(t, ctx, server)
	require.Len(t, pools, 1)
	assert.Equal(t, int64(2), pools[0].DesiredInstances, "activator scaled to 2")

	// Second reconciliation - reuses the same pool
	// BUG: Before the fix, this would reset desired_instances back to 0
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// CRITICAL: For auto mode, desired_instances should NOT be reset by launcher
	pools = listAllPools(t, ctx, server)
	require.Len(t, pools, 1, "should still have one pool (reused)")
	pool = pools[0]
	assert.Equal(t, int64(2), pool.DesiredInstances,
		"auto mode pool desired_instances should be preserved (not reset to 1)")
	assert.Contains(t, pool.ReferencedByVersions, version.ID,
		"pool should still reference the version")
}

// Helper functions

func listAllPools(t *testing.T, ctx context.Context, server *testutils.InMemEntityServer) []compute_v1alpha.SandboxPool {
	t.Helper()

	resp, err := server.EAC.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandboxPool))
	require.NoError(t, err)

	var pools []compute_v1alpha.SandboxPool
	for _, ent := range resp.Values() {
		var pool compute_v1alpha.SandboxPool
		pool.Decode(ent.Entity())
		pools = append(pools, pool)
	}

	return pools
}

// TestPerServiceEnvVarsDoNotRestartOtherServices verifies that changing env vars
// for one service doesn't cause other services to restart (pool reuse works correctly)
func TestPerServiceEnvVarsDoNotRestartOtherServices(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app
	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	// Create version v1 with two services: web and postgres
	version1 := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "test:latest",
		Config: core_v1alpha.Config{
			Port: 3000,
			Services: []core_v1alpha.Services{
				{
					Name: "web",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 1,
					},
				},
				{
					Name: "postgres",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 1,
					},
				},
			},
		},
	}
	ver1ID, err := server.Client.Create(ctx, "test-ver-v1", version1)
	require.NoError(t, err)
	version1.ID = ver1ID

	// Set as active version
	app.ActiveVersion = version1.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	// Create launcher and reconcile to create pools
	launcher := NewLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Verify both pools were created
	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 2, "should create two pools")

	// Find web and postgres pools
	var webPool, postgresPool *compute_v1alpha.SandboxPool
	for i := range pools {
		switch pools[i].Service {
		case "web":
			webPool = &pools[i]
		case "postgres":
			postgresPool = &pools[i]
		}
	}
	require.NotNil(t, webPool, "web pool should exist")
	require.NotNil(t, postgresPool, "postgres pool should exist")

	// Save postgres pool ID for later comparison
	postgresPoolID := postgresPool.ID

	// Create version v2 with env var ONLY for web service
	version2 := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v2",
		ImageUrl: "test:latest",
		Config: core_v1alpha.Config{
			Port: 3000,
			Services: []core_v1alpha.Services{
				{
					Name: "web",
					Env: []core_v1alpha.Env{
						{
							Key:   "API_KEY",
							Value: "secret123",
						},
					},
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 1,
					},
				},
				{
					Name: "postgres",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 1,
					},
				},
			},
		},
	}
	ver2ID, err := server.Client.Create(ctx, "test-ver-v2", version2)
	require.NoError(t, err)
	version2.ID = ver2ID

	// Update active version to v2
	app.ActiveVersion = version2.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	// Reconcile with new version
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Verify pools after update
	poolsAfter := listAllPools(t, ctx, server)
	require.Len(t, poolsAfter, 3, "should have 3 pools total (old web, new web, reused postgres)")

	// Find the postgres pool - it should be the SAME pool (reused)
	var postgresPoolAfter *compute_v1alpha.SandboxPool
	var webV2Pool *compute_v1alpha.SandboxPool
	for i := range poolsAfter {
		if poolsAfter[i].Service == "postgres" && poolsAfter[i].ID == postgresPoolID {
			postgresPoolAfter = &poolsAfter[i]
		}
		if poolsAfter[i].Service == "web" && poolsAfter[i].SandboxSpec.Version == version2.ID {
			webV2Pool = &poolsAfter[i]
		}
	}

	// CRITICAL: Postgres pool should be reused (same ID)
	require.NotNil(t, postgresPoolAfter, "postgres pool should still exist with same ID")
	assert.Equal(t, postgresPoolID, postgresPoolAfter.ID, "postgres pool ID should be unchanged (reused)")
	assert.Contains(t, postgresPoolAfter.ReferencedByVersions, version2.ID, "postgres pool should be referenced by v2")
	// Note: During rolling deployments, pools can be referenced by multiple versions
	// The old v1 reference will remain until the pool is no longer needed
	assert.Contains(t, postgresPoolAfter.ReferencedByVersions, version1.ID, "postgres pool should still reference v1 during transition")

	// Web pool should be NEW (different spec due to env var)
	require.NotNil(t, webV2Pool, "web pool for v2 should exist")
	assert.NotEqual(t, webPool.ID, webV2Pool.ID, "web pool should be recreated with new ID")
	assert.Contains(t, webV2Pool.ReferencedByVersions, version2.ID, "web v2 pool should be referenced by v2")

	// Verify env vars are in the web pool spec
	require.Len(t, webV2Pool.SandboxSpec.Container, 1, "web pool should have one container")
	foundAPIKey := false
	for _, envVar := range webV2Pool.SandboxSpec.Container[0].Env {
		if envVar == "API_KEY=secret123" {
			foundAPIKey = true
			break
		}
	}
	assert.True(t, foundAPIKey, "web pool should have API_KEY env var")

	// Verify postgres pool spec does NOT have the API_KEY env var
	require.Len(t, postgresPoolAfter.SandboxSpec.Container, 1, "postgres pool should have one container")
	foundAPIKeyInPostgres := false
	for _, envVar := range postgresPoolAfter.SandboxSpec.Container[0].Env {
		if envVar == "API_KEY=secret123" {
			foundAPIKeyInPostgres = true
			break
		}
	}
	assert.False(t, foundAPIKeyInPostgres, "postgres pool should NOT have API_KEY env var")
}

// TestPerServicePortConfiguration tests that launcher correctly configures ports
// based on per-service configuration, with fallback to global port and defaults
func TestPerServicePortConfiguration(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app
	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	// Create version with multiple services having different port configurations
	version := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "test:latest",
		Config: core_v1alpha.Config{
			Port: 4000, // Global port
			Services: []core_v1alpha.Services{
				{
					Name: "web",
					Port: 8080, // Per-service port (should override global)
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:                "auto",
						RequestsPerInstance: 10,
					},
				},
				{
					Name: "api",
					Port: 3001, // Per-service port
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 1,
					},
				},
				{
					Name: "admin",
					// No per-service port - should use global port 4000
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 1,
					},
				},
				{
					Name: "worker",
					// No per-service port, and it's not "web" - should not get any port
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 1,
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", version)
	require.NoError(t, err)
	version.ID = verID

	// Set as active version
	app.ActiveVersion = version.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	// Create launcher and reconcile
	launcher := NewLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Get all pools
	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 4, "Expected four pools")

	// Build map of pools by service
	poolsByService := make(map[string]*compute_v1alpha.SandboxPool)
	for i := range pools {
		poolsByService[pools[i].Service] = &pools[i]
	}

	// Test web service - should use per-service port 8080
	webPool := poolsByService["web"]
	require.NotNil(t, webPool, "web pool should exist")
	require.Len(t, webPool.SandboxSpec.Container, 1, "web pool should have one container")
	require.Len(t, webPool.SandboxSpec.Container[0].Port, 1, "web container should have one port")
	assert.Equal(t, int64(8080), webPool.SandboxSpec.Container[0].Port[0].Port, "web should use per-service port 8080")
	assert.Equal(t, "http", webPool.SandboxSpec.Container[0].Port[0].Name)
	assert.Equal(t, "http", webPool.SandboxSpec.Container[0].Port[0].Type)

	// Test api service - should use per-service port 3001
	apiPool := poolsByService["api"]
	require.NotNil(t, apiPool, "api pool should exist")
	require.Len(t, apiPool.SandboxSpec.Container, 1, "api pool should have one container")
	require.Len(t, apiPool.SandboxSpec.Container[0].Port, 1, "api container should have one port")
	assert.Equal(t, int64(3001), apiPool.SandboxSpec.Container[0].Port[0].Port, "api should use per-service port 3001")

	// Test admin service - global port only applies to "web", so admin gets no port
	adminPool := poolsByService["admin"]
	require.NotNil(t, adminPool, "admin pool should exist")
	require.Len(t, adminPool.SandboxSpec.Container, 1, "admin pool should have one container")
	assert.Empty(t, adminPool.SandboxSpec.Container[0].Port, "admin should not have any port (global port only applies to web)")

	// Test worker service - should not have any port configured (non-web service with no port config)
	workerPool := poolsByService["worker"]
	require.NotNil(t, workerPool, "worker pool should exist")
	require.Len(t, workerPool.SandboxSpec.Container, 1, "worker pool should have one container")
	assert.Empty(t, workerPool.SandboxSpec.Container[0].Port, "worker should not have any port configured")
}

// TestWebServiceDefaultPort tests that "web" service gets default port 3000 when no port is configured
func TestWebServiceDefaultPort(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app
	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	// Create version with web service but no port configuration at all
	version := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "test:latest",
		Config: core_v1alpha.Config{
			// No Port field - defaults to 0
			Services: []core_v1alpha.Services{
				{
					Name: "web",
					// No Port field
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:                "auto",
						RequestsPerInstance: 10,
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", version)
	require.NoError(t, err)
	version.ID = verID

	// Set as active version
	app.ActiveVersion = version.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	// Create launcher and reconcile
	launcher := NewLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Get pool
	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1, "Expected one pool")

	pool := pools[0]
	require.Len(t, pool.SandboxSpec.Container, 1, "pool should have one container")
	require.Len(t, pool.SandboxSpec.Container[0].Port, 1, "web container should have one port")
	assert.Equal(t, int64(3000), pool.SandboxSpec.Container[0].Port[0].Port, "web service should default to port 3000")
}

// TestPortNameAndType tests that launcher correctly wires port_name and port_type
func TestPortNameAndType(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app
	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	// Create version with custom port_name and port_type
	version := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "test:latest",
		Config: core_v1alpha.Config{
			Services: []core_v1alpha.Services{
				{
					Name:     "grpc-service",
					Port:     9090,
					PortName: "grpc",
					PortType: "grpc",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 1,
					},
				},
				{
					Name: "web",
					Port: 8080,
					// No port_name or port_type - should default to "http" and "http"
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:                "auto",
						RequestsPerInstance: 10,
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", version)
	require.NoError(t, err)
	version.ID = verID

	// Set as active version
	app.ActiveVersion = version.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	// Create launcher and reconcile
	launcher := NewLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Get all pools
	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 2, "Expected two pools")

	// Build map of pools by service
	poolsByService := make(map[string]*compute_v1alpha.SandboxPool)
	for i := range pools {
		poolsByService[pools[i].Service] = &pools[i]
	}

	// Verify grpc-service has custom port_name and port_type
	grpcPool, ok := poolsByService["grpc-service"]
	require.True(t, ok, "grpc-service pool should exist")
	require.Len(t, grpcPool.SandboxSpec.Container, 1, "pool should have one container")
	require.Len(t, grpcPool.SandboxSpec.Container[0].Port, 1, "grpc container should have one port")

	grpcPort := grpcPool.SandboxSpec.Container[0].Port[0]
	assert.Equal(t, int64(9090), grpcPort.Port, "grpc service should use port 9090")
	assert.Equal(t, "grpc", grpcPort.Name, "grpc service should have port name grpc")
	assert.Equal(t, "grpc", grpcPort.Type, "grpc service should have port type grpc")

	// Verify web service has default port_name and port_type
	webPool, ok := poolsByService["web"]
	require.True(t, ok, "web pool should exist")
	require.Len(t, webPool.SandboxSpec.Container, 1, "pool should have one container")
	require.Len(t, webPool.SandboxSpec.Container[0].Port, 1, "web container should have one port")

	webPort := webPool.SandboxSpec.Container[0].Port[0]
	assert.Equal(t, int64(8080), webPort.Port, "web service should use port 8080")
	assert.Equal(t, "http", webPort.Name, "web service should default to port name http")
	assert.Equal(t, "http", webPort.Type, "web service should default to port type http")
}
