package deployment

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appclient "miren.dev/runtime/api/app"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	apiserver "miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/network/network_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
)

func newTestLauncher(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) *Launcher {
	l := NewLauncher(log, eac)
	l.PoolReadyTimeout = 100 * time.Millisecond
	return l
}

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

	launcher := newTestLauncher(log, server.EAC)

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

	launcher := newTestLauncher(log, server.EAC)

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

	launcher := newTestLauncher(log, server.EAC)
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

	launcher := newTestLauncher(log, server.EAC)
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

	launcher := newTestLauncher(log, server.EAC)
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

	launcher := newTestLauncher(log, server.EAC)

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

	launcher := newTestLauncher(log, server.EAC)
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
	launcher := newTestLauncher(log, server.EAC)
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

	launcher := newTestLauncher(log, server.EAC)
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

	launcher := newTestLauncher(log, server.EAC)
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
	assert.Contains(t, webPool.SandboxSpec.Container[0].Env, "PORT=8080", "PORT env var should be set for web service")

	// Test api service - should use per-service port 3001
	apiPool := poolsByService["api"]
	require.NotNil(t, apiPool, "api pool should exist")
	require.Len(t, apiPool.SandboxSpec.Container, 1, "api pool should have one container")
	require.Len(t, apiPool.SandboxSpec.Container[0].Port, 1, "api container should have one port")
	assert.Equal(t, int64(3001), apiPool.SandboxSpec.Container[0].Port[0].Port, "api should use per-service port 3001")
	assert.Contains(t, apiPool.SandboxSpec.Container[0].Env, "PORT=3001", "PORT env var should be set for api service")

	// Test admin service - global port only applies to "web", so admin gets no port
	adminPool := poolsByService["admin"]
	require.NotNil(t, adminPool, "admin pool should exist")
	require.Len(t, adminPool.SandboxSpec.Container, 1, "admin pool should have one container")
	assert.Empty(t, adminPool.SandboxSpec.Container[0].Port, "admin should not have any port (global port only applies to web)")
	for _, env := range adminPool.SandboxSpec.Container[0].Env {
		assert.False(t, strings.HasPrefix(env, "PORT="), "PORT env var should NOT be set for admin service without port")
	}

	// Test worker service - should not have any port configured (non-web service with no port config)
	workerPool := poolsByService["worker"]
	require.NotNil(t, workerPool, "worker pool should exist")
	require.Len(t, workerPool.SandboxSpec.Container, 1, "worker pool should have one container")
	assert.Empty(t, workerPool.SandboxSpec.Container[0].Port, "worker should not have any port configured")
	for _, env := range workerPool.SandboxSpec.Container[0].Env {
		assert.False(t, strings.HasPrefix(env, "PORT="), "PORT env var should NOT be set for worker service without port")
	}
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

	launcher := newTestLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Get pool
	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1, "Expected one pool")

	pool := pools[0]
	require.Len(t, pool.SandboxSpec.Container, 1, "pool should have one container")
	require.Len(t, pool.SandboxSpec.Container[0].Port, 1, "web container should have one port")
	assert.Equal(t, int64(3000), pool.SandboxSpec.Container[0].Port[0].Port, "web service should default to port 3000")

	// Verify PORT env var is set
	assert.Contains(t, pool.SandboxSpec.Container[0].Env, "PORT=3000", "PORT env var should be set to default port")
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

	launcher := newTestLauncher(log, server.EAC)
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

// TestRapidVersionChangesCreateSinglePool tests that when multiple AppVersions
// are created in quick succession, the launcher only creates a pool for the
// latest ActiveVersion. This verifies that Reconcile() re-reads the App from
// the store (coalescing stale events) rather than using the event-embedded snapshot.
func TestRapidVersionChangesCreateSinglePool(t *testing.T) {
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

	// Create 3 versions rapidly with the same spec (same image, same config)
	versions := make([]*core_v1alpha.AppVersion, 3)
	for i := range versions {
		ver := &core_v1alpha.AppVersion{
			App:      app.ID,
			Version:  fmt.Sprintf("v%d", i+1),
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
				},
			},
		}
		verID, err := server.Client.Create(ctx, fmt.Sprintf("test-ver-v%d", i+1), ver)
		require.NoError(t, err)
		ver.ID = verID
		versions[i] = ver

		// Set each version as active (simulating rapid deploys)
		app.ActiveVersion = ver.ID
		err = server.Client.Update(ctx, app)
		require.NoError(t, err)
	}

	// At this point ActiveVersion points to v3.
	// Simulate the controller framework dispatching events for v1, v2, v3.
	// Each event carries a stale App snapshot from when it was dispatched.
	launcher := newTestLauncher(log, server.EAC)

	// Simulate stale v1 event: app snapshot has ActiveVersion=v1
	staleAppV1 := &core_v1alpha.App{
		ID:            app.ID,
		Project:       app.Project,
		ActiveVersion: versions[0].ID, // stale: points to v1
	}
	err = launcher.Reconcile(ctx, staleAppV1, nil)
	require.NoError(t, err)

	// Simulate stale v2 event
	staleAppV2 := &core_v1alpha.App{
		ID:            app.ID,
		Project:       app.Project,
		ActiveVersion: versions[1].ID, // stale: points to v2
	}
	err = launcher.Reconcile(ctx, staleAppV2, nil)
	require.NoError(t, err)

	// Simulate v3 event (current)
	staleAppV3 := &core_v1alpha.App{
		ID:            app.ID,
		Project:       app.Project,
		ActiveVersion: versions[2].ID,
	}
	err = launcher.Reconcile(ctx, staleAppV3, nil)
	require.NoError(t, err)

	// All three reconciles should have seen ActiveVersion=v3 (the latest)
	// and created/reused a single pool for it.
	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1, "should create only one pool despite 3 reconcile calls with stale events")

	pool := pools[0]
	assert.Equal(t, "web", pool.Service)
	assert.Equal(t, int64(1), pool.DesiredInstances, "auto mode should have desired_instances=1")
	// The pool should reference v3 (the latest version)
	assert.Contains(t, pool.ReferencedByVersions, versions[2].ID, "pool should reference the latest version v3")
}

// TestNoActiveVersionSkipsReconcile tests that the launcher returns early
// without creating pools when an app has no active version set.
// This behavior is critical for app deletion - we clear ActiveVersion first
// to prevent the launcher from recreating pools during the deletion window.
func TestNoActiveVersionSkipsReconcile(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app with no ActiveVersion
	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	launcher := newTestLauncher(log, server.EAC)

	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Verify no pools were created
	pools := listAllPools(t, ctx, server)
	assert.Empty(t, pools, "should not create any pools when ActiveVersion is empty")
}

func TestWaitForPoolReadySuccess(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	launcher := newTestLauncher(log, server.EAC)

	pool := &compute_v1alpha.SandboxPool{
		Service:          "web",
		DesiredInstances: 1,
		ReadyInstances:   1,
	}
	poolID, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)

	err = launcher.waitForPoolReady(ctx, poolID, 5*time.Second)
	assert.NoError(t, err)
}

func TestWaitForPoolReadyTimeout(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	launcher := newTestLauncher(log, server.EAC)

	pool := &compute_v1alpha.SandboxPool{
		Service:          "web",
		DesiredInstances: 1,
		ReadyInstances:   0,
	}
	poolID, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)

	err = launcher.waitForPoolReady(ctx, poolID, 100*time.Millisecond)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not ready after")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestWaitForPoolReadyContextCancelled(t *testing.T) {
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	launcher := newTestLauncher(log, server.EAC)

	ctx, cancel := context.WithCancel(context.Background())

	pool := &compute_v1alpha.SandboxPool{
		Service:          "web",
		DesiredInstances: 1,
		ReadyInstances:   0,
	}
	poolID, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)

	cancel()

	err = launcher.waitForPoolReady(ctx, poolID, 60*time.Second)
	assert.Error(t, err)
}

func TestCleanupWaitsForNewPool(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	v1 := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "test:v1",
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
			},
		},
	}
	v1ID, err := server.Client.Create(ctx, "test-v1", v1)
	require.NoError(t, err)
	v1.ID = v1ID

	app.ActiveVersion = v1.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	launcher := newTestLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	poolsV1 := listAllPools(t, ctx, server)
	require.Len(t, poolsV1, 1)

	// Different image forces a new pool, triggering the wait-then-cleanup flow
	v2 := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v2",
		ImageUrl: "test:v2",
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
			},
		},
	}
	v2ID, err := server.Client.Create(ctx, "test-v2", v2)
	require.NoError(t, err)
	v2.ID = v2ID

	app.ActiveVersion = v2.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	// waitForPoolReady will timeout (nothing sets ReadyInstances in unit tests)
	// and proceed with cleanup — this is the expected path.
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 2, "should have both old and new pools")

	var oldPool, newPool *compute_v1alpha.SandboxPool
	for i := range pools {
		if pools[i].SandboxSpec.Version == v1.ID {
			oldPool = &pools[i]
		}
		if pools[i].SandboxSpec.Version == v2.ID {
			newPool = &pools[i]
		}
	}

	require.NotNil(t, newPool, "new pool should exist")
	assert.Equal(t, int64(1), newPool.DesiredInstances)

	if oldPool != nil {
		assert.Equal(t, int64(0), oldPool.DesiredInstances, "old pool should be scaled down")
	}
}

// TestMultiPortServiceConfiguration tests that the launcher correctly maps multiple ports
// from the config spec to the sandbox spec.
func TestMultiPortServiceConfiguration(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	version := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "test:latest",
		Config: core_v1alpha.Config{
			Services: []core_v1alpha.Services{
				{
					Name: "irc",
					Ports: []core_v1alpha.Ports{
						{Port: 6667, Name: "irc", Type: "tcp"},
						{Port: 6697, Name: "irc-tls", Type: "tcp", NodePort: 6697},
					},
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

	app.ActiveVersion = version.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	launcher := newTestLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1)

	pool := pools[0]
	require.Len(t, pool.SandboxSpec.Container, 1)

	container := pool.SandboxSpec.Container[0]
	require.Len(t, container.Port, 2, "should have two ports")

	assert.Equal(t, int64(6667), container.Port[0].Port)
	assert.Equal(t, "irc", container.Port[0].Name)
	assert.Equal(t, "tcp", container.Port[0].Type)
	assert.Equal(t, int64(0), container.Port[0].NodePort)

	assert.Equal(t, int64(6697), container.Port[1].Port)
	assert.Equal(t, "irc-tls", container.Port[1].Name)
	assert.Equal(t, "tcp", container.Port[1].Type)
	assert.Equal(t, int64(6697), container.Port[1].NodePort)

	// PORT env var should be set to first port (no HTTP type, so first port wins)
	assert.Contains(t, container.Env, "PORT=6667")
}

// TestMultiPortHTTPPortEnvVar tests that PORT env var is set to the first HTTP-typed port
// when multiple ports are configured.
func TestMultiPortHTTPPortEnvVar(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	version := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "test:latest",
		Config: core_v1alpha.Config{
			Services: []core_v1alpha.Services{
				{
					Name: "web",
					Ports: []core_v1alpha.Ports{
						{Port: 9090, Name: "metrics", Type: "tcp"},
						{Port: 8080, Name: "http", Type: "http"},
						{Port: 9443, Name: "https", Type: "http"},
					},
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

	app.ActiveVersion = version.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	launcher := newTestLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1)

	container := pools[0].SandboxSpec.Container[0]
	require.Len(t, container.Port, 3, "should have three ports")

	// PORT env var should be set to the first HTTP-typed port (8080), not the first port overall
	assert.Contains(t, container.Env, "PORT=8080")
}

// TestScalarPortBackwardCompatWithMultiPort tests that scalar port fields still work
// alongside services that use the new ports array.
func TestScalarPortBackwardCompatWithMultiPort(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	version := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "test:latest",
		Config: core_v1alpha.Config{
			Services: []core_v1alpha.Services{
				{
					Name:     "web",
					Port:     8080,
					PortName: "http",
					PortType: "http",
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

	app.ActiveVersion = version.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	launcher := newTestLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1)

	container := pools[0].SandboxSpec.Container[0]
	require.Len(t, container.Port, 1, "should have one port from scalar fields")
	assert.Equal(t, int64(8080), container.Port[0].Port)
	assert.Equal(t, "http", container.Port[0].Name)
	assert.Equal(t, "http", container.Port[0].Type)
	assert.Contains(t, container.Env, "PORT=8080")
}

// listAllServices lists all network Service entities from the in-mem store
func listAllServices(t *testing.T, ctx context.Context, server *testutils.InMemEntityServer) []network_v1alpha.Service {
	t.Helper()

	resp, err := server.EAC.List(ctx, entity.Ref(entity.EntityKind, network_v1alpha.KindService))
	require.NoError(t, err)

	var services []network_v1alpha.Service
	for _, ent := range resp.Values() {
		var svc network_v1alpha.Service
		svc.Decode(ent.Entity())
		services = append(services, svc)
	}

	return services
}

// TestServiceEntityCreatedForNonHTTPPorts tests that the launcher creates a network
// Service entity for services that have non-HTTP ports (e.g., TCP/UDP).
func TestServiceEntityCreatedForNonHTTPPorts(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "tcp-echo", app)
	require.NoError(t, err)
	app.ID = appID

	version := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "tcp-echo:latest",
		Config: core_v1alpha.Config{
			Services: []core_v1alpha.Services{
				{
					Name: "echo",
					Ports: []core_v1alpha.Ports{
						{
							Port:     3000,
							Name:     "health",
							Type:     "http",
							Protocol: core_v1alpha.TCP,
						},
						{
							Port:     7000,
							Name:     "echo",
							Type:     "tcp",
							Protocol: core_v1alpha.TCP,
							NodePort: 7000,
						},
					},
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

	app.ActiveVersion = version.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	launcher := newTestLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Verify Service entity was created
	services := listAllServices(t, ctx, server)
	require.Len(t, services, 1, "should create one service entity")

	svc := services[0]
	assert.Equal(t, entity.Id("svc/tcp-echo-echo"), svc.ID)
	require.Len(t, svc.Port, 2, "service should have 2 ports")

	assert.Equal(t, int64(3000), svc.Port[0].Port)
	assert.Equal(t, "health", svc.Port[0].Name)
	assert.Equal(t, "http", svc.Port[0].Type)
	assert.Equal(t, network_v1alpha.TCP, svc.Port[0].Protocol)

	assert.Equal(t, int64(7000), svc.Port[1].Port)
	assert.Equal(t, "echo", svc.Port[1].Name)
	assert.Equal(t, "tcp", svc.Port[1].Type)
	assert.Equal(t, int64(7000), svc.Port[1].NodePort)
	assert.Equal(t, network_v1alpha.TCP, svc.Port[1].Protocol)

	// Verify match labels on the service
	appLabel, ok := svc.Match.Get("app")
	assert.True(t, ok, "service should have app match label")
	assert.Equal(t, "tcp-echo", appLabel)

	// Verify metadata labels
	svcResp, err := server.EAC.Get(ctx, "svc/tcp-echo-echo")
	require.NoError(t, err)
	var meta core_v1alpha.Metadata
	meta.Decode(svcResp.Entity().Entity())
	managedBy, _ := meta.Labels.Get("managed-by")
	assert.Equal(t, "launcher", managedBy)
	svcLabel, _ := meta.Labels.Get("service")
	assert.Equal(t, "echo", svcLabel)
}

// TestServiceEntityUpdatedWhenPortsChange tests that deploying a new version
// with changed ports updates the existing Service entity.
func TestServiceEntityUpdatedWhenPortsChange(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "irc-server", app)
	require.NoError(t, err)
	app.ID = appID

	// v1: single TCP port
	v1 := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "irc:v1",
		Config: core_v1alpha.Config{
			Services: []core_v1alpha.Services{
				{
					Name: "irc",
					Ports: []core_v1alpha.Ports{
						{
							Port:     6667,
							Name:     "plaintext",
							Type:     "tcp",
							Protocol: core_v1alpha.TCP,
						},
					},
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

	app.ActiveVersion = v1.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	launcher := newTestLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	services := listAllServices(t, ctx, server)
	require.Len(t, services, 1)
	require.Len(t, services[0].Port, 1, "v1 should have 1 port")
	assert.Equal(t, int64(6667), services[0].Port[0].Port)

	// v2: add a second TCP port (TLS)
	v2 := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v2",
		ImageUrl: "irc:v2",
		Config: core_v1alpha.Config{
			Services: []core_v1alpha.Services{
				{
					Name: "irc",
					Ports: []core_v1alpha.Ports{
						{
							Port:     6667,
							Name:     "plaintext",
							Type:     "tcp",
							Protocol: core_v1alpha.TCP,
						},
						{
							Port:     6697,
							Name:     "tls",
							Type:     "tcp",
							Protocol: core_v1alpha.TCP,
							NodePort: 6697,
						},
					},
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

	app.ActiveVersion = v2.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Verify Service entity was updated with 2 ports
	services = listAllServices(t, ctx, server)
	require.Len(t, services, 1, "should still have one service entity")
	assert.Equal(t, entity.Id("svc/irc-server-irc"), services[0].ID)
	require.Len(t, services[0].Port, 2, "v2 should have 2 ports")
	assert.Equal(t, int64(6667), services[0].Port[0].Port)
	assert.Equal(t, int64(6697), services[0].Port[1].Port)
	assert.Equal(t, int64(6697), services[0].Port[1].NodePort)
}

// TestServiceEntityDeletedWhenServiceRemoved tests that Service entities are
// cleaned up when a service is removed or all its ports become HTTP-only.
func TestServiceEntityDeletedWhenServiceRemoved(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "tcp-app", app)
	require.NoError(t, err)
	app.ID = appID

	// v1: has TCP service
	v1 := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "app:v1",
		Config: core_v1alpha.Config{
			Services: []core_v1alpha.Services{
				{
					Name: "tcp-svc",
					Ports: []core_v1alpha.Ports{
						{
							Port:     9000,
							Name:     "data",
							Type:     "tcp",
							Protocol: core_v1alpha.TCP,
						},
					},
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 1,
					},
				},
				{
					Name: "web",
					Ports: []core_v1alpha.Ports{
						{
							Port:     3000,
							Name:     "http",
							Type:     "http",
							Protocol: core_v1alpha.TCP,
						},
					},
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:                "auto",
						RequestsPerInstance: 10,
					},
				},
			},
		},
	}
	v1ID, err := server.Client.Create(ctx, "test-v1", v1)
	require.NoError(t, err)
	v1.ID = v1ID

	app.ActiveVersion = v1.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	launcher := newTestLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Verify: tcp-svc service entity exists, web does not (HTTP-only)
	services := listAllServices(t, ctx, server)
	require.Len(t, services, 1, "should have one service entity (tcp-svc)")
	assert.Equal(t, entity.Id("svc/tcp-app-tcp-svc"), services[0].ID)

	// v2: remove tcp-svc, only keep web
	v2 := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v2",
		ImageUrl: "app:v2",
		Config: core_v1alpha.Config{
			Services: []core_v1alpha.Services{
				{
					Name: "web",
					Ports: []core_v1alpha.Ports{
						{
							Port:     3000,
							Name:     "http",
							Type:     "http",
							Protocol: core_v1alpha.TCP,
						},
					},
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:                "auto",
						RequestsPerInstance: 10,
					},
				},
			},
		},
	}
	v2ID, err := server.Client.Create(ctx, "test-v2", v2)
	require.NoError(t, err)
	v2.ID = v2ID

	app.ActiveVersion = v2.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Verify: tcp-svc service entity was deleted
	services = listAllServices(t, ctx, server)
	assert.Empty(t, services, "service entity should be deleted when service is removed")
}

// TestNoServiceEntityForHTTPOnlyService verifies that services with only HTTP
// ports do not get a Service entity created (they use httpingress instead).
// TestServiceEntityCreatedForScalarNonHTTPPort tests that the launcher creates a
// Service entity when scalar port fields (Port/PortType) specify a non-HTTP port.
func TestServiceEntityCreatedForScalarNonHTTPPort(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "legacy-tcp", app)
	require.NoError(t, err)
	app.ID = appID

	// Use scalar Port/PortType fields instead of Ports[] array
	version := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "legacy:latest",
		Config: core_v1alpha.Config{
			Services: []core_v1alpha.Services{
				{
					Name:     "worker",
					Port:     9000,
					PortName: "data",
					PortType: "tcp",
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

	app.ActiveVersion = version.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	launcher := newTestLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Verify Service entity was created from scalar port fields
	services := listAllServices(t, ctx, server)
	require.Len(t, services, 1, "should create service entity for scalar non-HTTP port")

	svc := services[0]
	assert.Equal(t, entity.Id("svc/legacy-tcp-worker"), svc.ID)
	require.Len(t, svc.Port, 1, "service should have 1 port backfilled from scalar fields")
	assert.Equal(t, int64(9000), svc.Port[0].Port)
	assert.Equal(t, "data", svc.Port[0].Name)
	assert.Equal(t, "tcp", svc.Port[0].Type)
}

func TestNoServiceEntityForHTTPOnlyService(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "web-app", app)
	require.NoError(t, err)
	app.ID = appID

	version := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "web:latest",
		Config: core_v1alpha.Config{
			Services: []core_v1alpha.Services{
				{
					Name: "web",
					Ports: []core_v1alpha.Ports{
						{
							Port:     3000,
							Name:     "http",
							Type:     "http",
							Protocol: core_v1alpha.TCP,
						},
					},
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

	app.ActiveVersion = version.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	launcher := newTestLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	services := listAllServices(t, ctx, server)
	assert.Empty(t, services, "HTTP-only service should not create a Service entity")
}

// TestWebServiceImplicitHTTPPortWithMultiPort tests that a web service with only non-HTTP
// ports configured still gets the implicit HTTP port 3000 injected, and PORT env var is
// set to 3000.
func TestWebServiceImplicitHTTPPortWithMultiPort(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	version := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "test:latest",
		Config: core_v1alpha.Config{
			Services: []core_v1alpha.Services{
				{
					Name: "web",
					Ports: []core_v1alpha.Ports{
						{Port: 6667, Name: "irc", Type: "tcp"},
						{Port: 6697, Name: "irc-tls", Type: "tcp", NodePort: 6697},
					},
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

	app.ActiveVersion = version.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	launcher := newTestLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1)

	container := pools[0].SandboxSpec.Container[0]
	require.Len(t, container.Port, 3, "should have two explicit TCP ports + implicit HTTP port 3000")

	assert.Equal(t, int64(6667), container.Port[0].Port)
	assert.Equal(t, "irc", container.Port[0].Name)
	assert.Equal(t, "tcp", container.Port[0].Type)

	assert.Equal(t, int64(6697), container.Port[1].Port)
	assert.Equal(t, "irc-tls", container.Port[1].Name)
	assert.Equal(t, "tcp", container.Port[1].Type)
	assert.Equal(t, int64(6697), container.Port[1].NodePort)

	assert.Equal(t, int64(3000), container.Port[2].Port)
	assert.Equal(t, "http", container.Port[2].Name)
	assert.Equal(t, "http", container.Port[2].Type)

	// PORT env var should be set to 3000 (the implicit HTTP port)
	assert.Contains(t, container.Env, "PORT=3000")
}

// TestDiskProviderMapping verifies that disk provider enum values are mapped to
// the short string form that the sandbox volume controller expects.
func TestDiskProviderMapping(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

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
						Mode:         "fixed",
						NumInstances: 1,
					},
					Disks: []core_v1alpha.Disks{
						{
							Name:      "local-data",
							Provider:  core_v1alpha.LOCAL,
							MountPath: "/data",
						},
						{
							Name:      "miren-data",
							Provider:  core_v1alpha.MIREN,
							MountPath: "/miren-data",
							SizeGb:    1,
						},
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", version)
	require.NoError(t, err)
	version.ID = verID

	app.ActiveVersion = version.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	launcher := newTestLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1, "should create one pool")

	volumes := pools[0].SandboxSpec.Volume
	require.Len(t, volumes, 2, "should have two volumes")

	// Volumes should have short provider strings, not full entity enum values
	var localVol, mirenVol *compute_v1alpha.SandboxSpecVolume
	for i := range volumes {
		switch volumes[i].Name {
		case "local-data":
			localVol = &volumes[i]
		case "miren-data":
			mirenVol = &volumes[i]
		}
	}

	require.NotNil(t, localVol, "should have local-data volume")
	assert.Equal(t, "local", localVol.Provider, "local disk provider should be mapped to short string")
	assert.Equal(t, "/data", localVol.MountPath)

	require.NotNil(t, mirenVol, "should have miren-data volume")
	assert.Equal(t, "miren", mirenVol.Provider, "miren disk provider should be mapped to short string")
	assert.Equal(t, "/miren-data", mirenVol.MountPath)
}

// TestDiskProviderDefaultsToMiren verifies that an empty provider defaults to "miren".
func TestDiskProviderDefaultsToMiren(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

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
						Mode:         "fixed",
						NumInstances: 1,
					},
					Disks: []core_v1alpha.Disks{
						{
							Name:      "default-disk",
							MountPath: "/storage",
							SizeGb:    1,
						},
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", version)
	require.NoError(t, err)
	version.ID = verID

	app.ActiveVersion = version.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	launcher := newTestLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1)

	volumes := pools[0].SandboxSpec.Volume
	require.Len(t, volumes, 1)
	assert.Equal(t, "miren", volumes[0].Provider, "empty provider should default to miren")
}

// TestLocalDisksAttachedForAutoScalingWebService verifies that local disks are
// mounted on web services with auto concurrency mode. This is a regression test
// for MIR-950 where the miren disk concurrency guard broke out of the entire
// disk loop, preventing local disks from being attached.
func TestLocalDisksAttachedForAutoScalingWebService(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

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
					Disks: []core_v1alpha.Disks{
						{
							Name:      "data",
							Provider:  core_v1alpha.LOCAL,
							MountPath: "/miren/data/local",
						},
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", version)
	require.NoError(t, err)
	version.ID = verID

	app.ActiveVersion = version.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	launcher := newTestLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1, "should create one pool")

	volumes := pools[0].SandboxSpec.Volume
	require.Len(t, volumes, 1, "local disk should be attached even with auto concurrency")
	assert.Equal(t, "local", volumes[0].Provider)
	assert.Equal(t, "data", volumes[0].Name)
	assert.Equal(t, "/miren/data/local", volumes[0].MountPath)

	// Verify the container mount was also added
	containers := pools[0].SandboxSpec.Container
	require.NotEmpty(t, containers)
	require.Len(t, containers[0].Mount, 1, "container should have disk mount")
	assert.Equal(t, "data", containers[0].Mount[0].Source)
	assert.Equal(t, "/miren/data/local", containers[0].Mount[0].Destination)
}

// TestMirenDisksSkippedForAutoScalingWebService verifies that miren disks are
// still correctly skipped for non-fixed concurrency services, while local disks
// in the same config are still attached.
func TestMirenDisksSkippedForAutoScalingWebService(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

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
					Disks: []core_v1alpha.Disks{
						{
							Name:      "local-data",
							Provider:  core_v1alpha.LOCAL,
							MountPath: "/data",
						},
						{
							Name:      "miren-data",
							Provider:  core_v1alpha.MIREN,
							MountPath: "/miren-data",
							SizeGb:    1,
						},
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", version)
	require.NoError(t, err)
	version.ID = verID

	app.ActiveVersion = version.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	launcher := newTestLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1, "should create one pool")

	volumes := pools[0].SandboxSpec.Volume
	require.Len(t, volumes, 1, "only local disk should be attached, miren disk should be skipped")
	assert.Equal(t, "local", volumes[0].Provider)
	assert.Equal(t, "local-data", volumes[0].Name)
}

func TestAutoMountLocalStorageWithExistingData(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

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

	app.ActiveVersion = version.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	// Create a temp DataPath with existing data for this app
	dataPath := t.TempDir()
	localDir := filepath.Join(dataPath, "data", "local", app.ID.String())
	require.NoError(t, os.MkdirAll(localDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(localDir, "data.db"), []byte("test"), 0644))

	launcher := newTestLauncher(log, server.EAC)
	launcher.DataPath = dataPath
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1)

	// Should have auto-mounted the local volume
	volumes := pools[0].SandboxSpec.Volume
	require.Len(t, volumes, 1, "should auto-mount local storage when data exists")
	assert.Equal(t, "local-data", volumes[0].Name)
	assert.Equal(t, "local", volumes[0].Provider)
	assert.Equal(t, "/miren/data/local", volumes[0].MountPath)

	mounts := pools[0].SandboxSpec.Container[0].Mount
	require.Len(t, mounts, 1)
	assert.Equal(t, "local-data", mounts[0].Source)
	assert.Equal(t, "/miren/data/local", mounts[0].Destination)
}

func TestNoAutoMountWhenNoExistingData(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

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

	app.ActiveVersion = version.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	// DataPath exists but no local data for this app
	launcher := newTestLauncher(log, server.EAC)
	launcher.DataPath = t.TempDir()
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1)

	// Should NOT auto-mount since there's no existing data
	assert.Empty(t, pools[0].SandboxSpec.Volume, "should not auto-mount when no existing data")
}

func TestNoAutoMountWhenExplicitDiskConfig(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

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
						Mode:         "fixed",
						NumInstances: 1,
					},
					Disks: []core_v1alpha.Disks{
						{
							Name:      "my-data",
							Provider:  core_v1alpha.LOCAL,
							MountPath: "/miren/data/local",
						},
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", version)
	require.NoError(t, err)
	version.ID = verID

	app.ActiveVersion = version.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	// Create existing data even though explicit config exists
	dataPath := t.TempDir()
	localDir := filepath.Join(dataPath, "data", "local", app.ID.String())
	require.NoError(t, os.MkdirAll(localDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(localDir, "data.db"), []byte("test"), 0644))

	launcher := newTestLauncher(log, server.EAC)
	launcher.DataPath = dataPath
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1)

	// Should only have the explicit disk, not the auto-mounted one
	volumes := pools[0].SandboxSpec.Volume
	require.Len(t, volumes, 1, "should only have explicit disk config")
	assert.Equal(t, "my-data", volumes[0].Name)
	assert.Equal(t, "local", volumes[0].Provider)
}

// TestNoAutoMountWhenExplicitLocalDiskElsewhere reproduces MIR-1423: an explicit
// local-provider disk mounted somewhere other than the legacy /miren/data/local
// path (and not named "local-data") must suppress the transitional auto-mount.
// All local volumes share the same per-app host directory, so the explicit disk
// already exposes the data; the path-only check used to miss this and inject a
// duplicate local-data mount at /miren/data/local pointing at the same bytes.
func TestNoAutoMountWhenExplicitLocalDiskElsewhere(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

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
						Mode:         "fixed",
						NumInstances: 1,
					},
					Disks: []core_v1alpha.Disks{
						{
							Name:      "chisigns-data",
							Provider:  core_v1alpha.LOCAL,
							MountPath: "/data",
						},
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", version)
	require.NoError(t, err)
	version.ID = verID

	app.ActiveVersion = version.ID
	require.NoError(t, server.Client.Update(ctx, app))

	// The per-app local directory is already populated, which would otherwise
	// self-trigger the legacy auto-mount for this explicit local disk.
	dataPath := t.TempDir()
	localDir := filepath.Join(dataPath, "data", "local", app.ID.String())
	require.NoError(t, os.MkdirAll(localDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(localDir, "data.db"), []byte("test"), 0644))

	launcher := newTestLauncher(log, server.EAC)
	launcher.DataPath = dataPath
	require.NoError(t, launcher.Reconcile(ctx, app, nil))

	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1)

	// Only the declared disk should be present — no injected local-data duplicate.
	volumes := pools[0].SandboxSpec.Volume
	require.Len(t, volumes, 1, "explicit local disk should suppress the auto-mount")
	assert.Equal(t, "chisigns-data", volumes[0].Name)
	assert.Equal(t, "local", volumes[0].Provider)
	assert.Equal(t, "/data", volumes[0].MountPath)
}

// TestNoAutoMountWhenDiskNameTaken verifies the auto-mount is skipped when the
// service already declares a disk under the "local-data" name, even at a
// different mount path. Injecting a second disk with that name would produce a
// duplicate volume name in the sandbox spec.
func TestNoAutoMountWhenDiskNameTaken(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

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
						Mode:         "fixed",
						NumInstances: 1,
					},
					Disks: []core_v1alpha.Disks{
						{
							Name:      "local-data",
							Provider:  core_v1alpha.LOCAL,
							MountPath: "/somewhere/else",
						},
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", version)
	require.NoError(t, err)
	version.ID = verID

	app.ActiveVersion = version.ID
	require.NoError(t, server.Client.Update(ctx, app))

	// Existing data would otherwise trigger the auto-mount.
	dataPath := t.TempDir()
	localDir := filepath.Join(dataPath, "data", "local", app.ID.String())
	require.NoError(t, os.MkdirAll(localDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(localDir, "data.db"), []byte("test"), 0644))

	launcher := newTestLauncher(log, server.EAC)
	launcher.DataPath = dataPath
	require.NoError(t, launcher.Reconcile(ctx, app, nil))

	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1)

	// Only the explicitly-named disk should be present; no injected duplicate.
	volumes := pools[0].SandboxSpec.Volume
	require.Len(t, volumes, 1, "should not inject a duplicate local-data disk")
	assert.Equal(t, "local-data", volumes[0].Name)
	assert.Equal(t, "/somewhere/else", volumes[0].MountPath)
}

// TestAutoMountDrainsSupersededPool reproduces MIR-1293: an app using
// auto-mounted local storage (existing data, no explicit disk config) must not
// accumulate a second pool for the same version when the auto-mount first drifts
// the pool spec. Before the fix the auto-mount was patched into the sandbox spec
// at build time only, so serviceHasDisks stayed false, the stale-pool drain never
// ran, and the superseded diskless pool lingered as a duplicate that still
// referenced the current version. Registering the auto-mount as a real disk in
// the resolved config puts the app on the disk-aware path, so the superseded pool
// is drained and dereferenced.
func TestAutoMountDrainsSupersededPool(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

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

	app.ActiveVersion = version.ID
	require.NoError(t, server.Client.Update(ctx, app))

	dataPath := t.TempDir()
	launcher := newTestLauncher(log, server.EAC)
	launcher.DataPath = dataPath

	// First reconcile: no local data yet, so a single diskless pool is created.
	require.NoError(t, launcher.Reconcile(ctx, app, nil))
	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1)
	require.Empty(t, pools[0].SandboxSpec.Volume, "first pool should be diskless")
	disklessPoolID := pools[0].ID

	// The app writes to its local storage between reconciles, so the auto-mount
	// probe now finds existing data.
	localDir := filepath.Join(dataPath, "data", "local", app.ID.String())
	require.NoError(t, os.MkdirAll(localDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(localDir, "data.db"), []byte("test"), 0644))

	// Second reconcile: the auto-mount now drifts the spec. The superseded
	// diskless pool must be drained and dereferenced, not left as a duplicate
	// orphan that still references the current version (MIR-1293).
	require.NoError(t, launcher.Reconcile(ctx, app, nil))

	getRes, err := server.EAC.Get(ctx, disklessPoolID.String())
	require.NoError(t, err)
	var oldPool compute_v1alpha.SandboxPool
	oldPool.Decode(getRes.Entity().Entity())
	assert.Equal(t, int64(0), oldPool.DesiredInstances,
		"superseded diskless pool should be scaled to 0")
	assert.Empty(t, oldPool.ReferencedByVersions,
		"superseded diskless pool should be dereferenced")

	// Exactly one pool should still reference the current version, and it should
	// carry the auto-mounted local disk.
	pools = listAllPools(t, ctx, server)
	var referencing []compute_v1alpha.SandboxPool
	for i := range pools {
		if containsRef(pools[i].ReferencedByVersions, version.ID) {
			referencing = append(referencing, pools[i])
		}
	}
	require.Len(t, referencing, 1, "exactly one pool should reference the current version")
	require.Len(t, referencing[0].SandboxSpec.Volume, 1)
	assert.Equal(t, "local-data", referencing[0].SandboxSpec.Volume[0].Name)
	assert.Equal(t, "local", referencing[0].SandboxSpec.Volume[0].Provider)
	assert.Equal(t, "/miren/data/local", referencing[0].SandboxSpec.Volume[0].MountPath)

	// The container mount is the user-visible half of the auto-mount, so verify
	// it too, not just the volume declaration.
	require.Len(t, referencing[0].SandboxSpec.Container, 1)
	require.Len(t, referencing[0].SandboxSpec.Container[0].Mount, 1)
	assert.Equal(t, "local-data", referencing[0].SandboxSpec.Container[0].Mount[0].Source)
	assert.Equal(t, "/miren/data/local", referencing[0].SandboxSpec.Container[0].Mount[0].Destination)
}

// TestDiskPoolDrainedBeforeNewPoolCreated verifies that when deploying a new
// version of an app with disks, the old pool is scaled to 0 before the new
// pool is created. This prevents conflicts when both old and new sandboxes try
// to mount the same disk.
func TestDiskPoolDrainedBeforeNewPoolCreated(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	// Deploy v1 with a local disk (e.g. victoriametrics)
	v1 := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "oci.miren.cloud/victoriametrics:v1",
		Config: core_v1alpha.Config{
			Port: 8428,
			Services: []core_v1alpha.Services{
				{
					Name: "victoriametrics",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 1,
					},
					Disks: []core_v1alpha.Disks{
						{
							Name:      "vm-data",
							Provider:  core_v1alpha.LOCAL,
							MountPath: "/miren/data/local/victoria-metrics-data",
						},
					},
				},
			},
		},
	}
	v1ID, err := server.Client.Create(ctx, "test-v1", v1)
	require.NoError(t, err)
	v1.ID = v1ID

	app.ActiveVersion = v1.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	launcher := newTestLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	poolsV1 := listAllPools(t, ctx, server)
	require.Len(t, poolsV1, 1, "should create one pool for v1")
	poolV1ID := poolsV1[0].ID

	// Deploy v2 with same local disk but new image (triggers new pool)
	v2 := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v2",
		ImageUrl: "oci.miren.cloud/victoriametrics:v2",
		Config: core_v1alpha.Config{
			Port: 8428,
			Services: []core_v1alpha.Services{
				{
					Name: "victoriametrics",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 1,
					},
					Disks: []core_v1alpha.Disks{
						{
							Name:      "vm-data",
							Provider:  core_v1alpha.LOCAL,
							MountPath: "/miren/data/local/victoria-metrics-data",
						},
					},
				},
			},
		},
	}
	v2ID, err := server.Client.Create(ctx, "test-v2", v2)
	require.NoError(t, err)
	v2.ID = v2ID

	app.ActiveVersion = v2.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// The old pool should have been drained (scaled to 0, no references)
	// BEFORE the new pool was created.
	getRes, err := server.EAC.Get(ctx, poolV1ID.String())
	require.NoError(t, err)
	var poolV1Refreshed compute_v1alpha.SandboxPool
	poolV1Refreshed.Decode(getRes.Entity().Entity())

	assert.Equal(t, int64(0), poolV1Refreshed.DesiredInstances,
		"old disk pool should be scaled to 0")
	assert.Empty(t, poolV1Refreshed.ReferencedByVersions,
		"old disk pool should have no version references")

	// New pool should have been created for v2
	allPools := listAllPools(t, ctx, server)
	require.Len(t, allPools, 2, "should have old and new pools")

	var poolV2 *compute_v1alpha.SandboxPool
	for i := range allPools {
		if allPools[i].ID != poolV1ID {
			poolV2 = &allPools[i]
			break
		}
	}
	require.NotNil(t, poolV2, "should find the new pool")
	assert.Equal(t, "victoriametrics", poolV2.Service)
	assert.Contains(t, poolV2.ReferencedByVersions, v2.ID)

	// New pool should have the local disk volume
	require.Len(t, poolV2.SandboxSpec.Volume, 1)
	assert.Equal(t, "local", poolV2.SandboxSpec.Volume[0].Provider)
}

// TestDiskDrainBlockedByActiveSandbox verifies that when a RUNNING sandbox
// exists for the old pool, the drain times out and the new pool is NOT created.
// This proves drain-before-create ordering: the launcher won't start the new
// version while the old one still holds the disk.
func TestDiskDrainBlockedByActiveSandbox(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	v1 := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "oci.miren.cloud/victoriametrics:v1",
		Config: core_v1alpha.Config{
			Port: 8428,
			Services: []core_v1alpha.Services{
				{
					Name: "victoriametrics",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 1,
					},
					Disks: []core_v1alpha.Disks{
						{
							Name:      "vm-data",
							Provider:  core_v1alpha.LOCAL,
							MountPath: "/miren/data/local/victoria-metrics-data",
						},
					},
				},
			},
		},
	}
	v1ID, err := server.Client.Create(ctx, "test-v1", v1)
	require.NoError(t, err)
	v1.ID = v1ID

	app.ActiveVersion = v1.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	launcher := newTestLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	poolsV1 := listAllPools(t, ctx, server)
	require.Len(t, poolsV1, 1)
	poolV1ID := poolsV1[0].ID

	// Simulate a RUNNING sandbox for the old pool (as the sandbox controller
	// would create). This holds the disk and blocks draining.
	sb := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.RUNNING,
	}
	_, err = server.Client.Create(ctx, "old-sandbox",
		sb,
		apiserver.WithLabels(types.LabelSet(
			"pool", poolV1ID.String(),
			"service", "victoriametrics",
		)),
	)
	require.NoError(t, err)

	// Deploy v2
	v2 := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v2",
		ImageUrl: "oci.miren.cloud/victoriametrics:v2",
		Config: core_v1alpha.Config{
			Port: 8428,
			Services: []core_v1alpha.Services{
				{
					Name: "victoriametrics",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 1,
					},
					Disks: []core_v1alpha.Disks{
						{
							Name:      "vm-data",
							Provider:  core_v1alpha.LOCAL,
							MountPath: "/miren/data/local/victoria-metrics-data",
						},
					},
				},
			},
		},
	}
	v2ID, err := server.Client.Create(ctx, "test-v2", v2)
	require.NoError(t, err)
	v2.ID = v2ID

	app.ActiveVersion = v2.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	// Reconcile should NOT create a new pool because the old sandbox is
	// still running (drain times out). The service is skipped.
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Only the original pool should exist — no v2 pool was created
	allPools := listAllPools(t, ctx, server)
	require.Len(t, allPools, 1, "should not create new pool while old sandbox is running")

	// The old pool was marked for drain (desired=0, no refs)
	getRes, err := server.EAC.Get(ctx, poolV1ID.String())
	require.NoError(t, err)
	var poolV1 compute_v1alpha.SandboxPool
	poolV1.Decode(getRes.Entity().Entity())
	assert.Equal(t, int64(0), poolV1.DesiredInstances,
		"old pool should be scaled to 0")
	assert.Empty(t, poolV1.ReferencedByVersions,
		"old pool should have no version references")
}

// TestServiceHasDisks verifies the serviceHasDisks helper.
func TestServiceHasDisks(t *testing.T) {
	tests := []struct {
		name     string
		spec     *core_v1alpha.ConfigSpec
		service  string
		expected bool
	}{
		{
			name: "service with local disk",
			spec: &core_v1alpha.ConfigSpec{
				Services: []core_v1alpha.ConfigSpecServices{
					{
						Name: "db",
						Disks: []core_v1alpha.ConfigSpecServicesDisks{
							{Name: "data", Provider: core_v1alpha.ConfigSpecServicesDisksLOCAL},
						},
					},
				},
			},
			service:  "db",
			expected: true,
		},
		{
			name: "service with miren disk",
			spec: &core_v1alpha.ConfigSpec{
				Services: []core_v1alpha.ConfigSpecServices{
					{
						Name: "db",
						Disks: []core_v1alpha.ConfigSpecServicesDisks{
							{Name: "data", Provider: core_v1alpha.ConfigSpecServicesDisksMIREN},
						},
					},
				},
			},
			service:  "db",
			expected: true,
		},
		{
			name: "service with no disks",
			spec: &core_v1alpha.ConfigSpec{
				Services: []core_v1alpha.ConfigSpecServices{
					{Name: "web"},
				},
			},
			service:  "web",
			expected: false,
		},
		{
			name: "different service has disk",
			spec: &core_v1alpha.ConfigSpec{
				Services: []core_v1alpha.ConfigSpecServices{
					{Name: "web"},
					{
						Name: "db",
						Disks: []core_v1alpha.ConfigSpecServicesDisks{
							{Name: "data", Provider: core_v1alpha.ConfigSpecServicesDisksLOCAL},
						},
					},
				},
			},
			service:  "web",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, serviceHasDisks(tt.spec, tt.service))
		})
	}
}

// TestDisklessServiceNotDrained verifies that services without disks still use
// the normal rolling deploy strategy (new pool created before old pool is
// cleaned up).
func TestDisklessServiceNotDrained(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	// Deploy v1 — no disks
	v1 := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "oci.miren.cloud/myapp:v1",
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
			},
		},
	}
	v1ID, err := server.Client.Create(ctx, "test-v1", v1)
	require.NoError(t, err)
	v1.ID = v1ID

	app.ActiveVersion = v1.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	launcher := newTestLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	poolsV1 := listAllPools(t, ctx, server)
	require.Len(t, poolsV1, 1)

	// Deploy v2 with new image
	v2 := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v2",
		ImageUrl: "oci.miren.cloud/myapp:v2",
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
			},
		},
	}
	v2ID, err := server.Client.Create(ctx, "test-v2", v2)
	require.NoError(t, err)
	v2.ID = v2ID

	app.ActiveVersion = v2.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	// Both pools should exist — rolling deploy creates the new pool before
	// cleaning up the old one (create-before-drain). Verify one is the active
	// v2 pool and the other was scaled down by cleanup.
	allPools := listAllPools(t, ctx, server)
	require.Len(t, allPools, 2, "rolling deploy should have both pools")

	var newPool, oldPool *compute_v1alpha.SandboxPool
	for i := range allPools {
		if len(allPools[i].ReferencedByVersions) > 0 {
			newPool = &allPools[i]
		} else {
			oldPool = &allPools[i]
		}
	}
	require.NotNil(t, newPool, "should have an active pool for v2")
	require.NotNil(t, oldPool, "should have a decommissioned old pool")
	assert.Equal(t, int64(1), newPool.DesiredInstances, "new pool should be active")
	assert.Equal(t, int64(0), oldPool.DesiredInstances, "old pool should be scaled down")
}

// TestPortTimeoutPropagatesToSandboxSpec verifies that a per-service
// port_timeout in ConfigSpec is copied into SandboxSpec.PortWaitTimeout
// for the matching service, and left empty for siblings.
func TestPortTimeoutPropagatesToSandboxSpec(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{Project: entity.Id("project-1")}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	ver := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "test:latest",
	}
	verID, err := server.Client.Create(ctx, "test-ver", ver)
	require.NoError(t, err)
	ver.ID = verID

	cfgSpec := &core_v1alpha.ConfigSpec{
		Services: []core_v1alpha.ConfigSpecServices{
			{Name: "web", Port: 4000, PortTimeout: "120s"},
			{Name: "worker"},
		},
	}

	l := newTestLauncher(log, server.EAC)

	webSpec, err := l.buildSandboxSpec(ctx, app, ver, cfgSpec, "web", "test:latest")
	require.NoError(t, err)
	assert.Equal(t, "120s", webSpec.PortWaitTimeout, "web service should propagate explicit timeout")

	workerSpec, err := l.buildSandboxSpec(ctx, app, ver, cfgSpec, "worker", "test:latest")
	require.NoError(t, err)
	assert.Empty(t, workerSpec.PortWaitTimeout, "worker without timeout stays empty so default applies in resolvePortWaitTimeout")
}

// TestRuntimeEnvVarsInjectedIntoSandboxSpec pins the names Miren injects into
// every app container. The old names (MIREN_APP, MIREN_VERSION) collided with the
// vars the CLI reads from its own environment, so `miren` run inside a sandbox
// targeted the sandbox's app instead of the one in .miren/app.toml (MIR-1406).
func TestRuntimeEnvVarsInjectedIntoSandboxSpec(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{Project: entity.Id("project-1")}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	ver := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "test:latest",
	}
	verID, err := server.Client.Create(ctx, "test-ver", ver)
	require.NoError(t, err)
	ver.ID = verID

	cfgSpec := &core_v1alpha.ConfigSpec{
		Services: []core_v1alpha.ConfigSpecServices{{Name: "web", Port: 4000}},
	}

	l := newTestLauncher(log, server.EAC)

	spec, err := l.buildSandboxSpec(ctx, app, ver, cfgSpec, "web", "test:latest")
	require.NoError(t, err)
	require.NotEmpty(t, spec.Container)

	env := spec.Container[0].Env
	assert.Contains(t, env, appclient.EnvRuntimeApp+"=test-app")
	assert.Contains(t, env, appclient.EnvRuntimeVersion+"=v1")

	for _, e := range env {
		assert.False(t, strings.HasPrefix(e, "MIREN_APP="),
			"MIREN_APP must not be injected — the CLI reads it as its target app (MIR-1406)")
		assert.False(t, strings.HasPrefix(e, "MIREN_VERSION="),
			"MIREN_VERSION was renamed to %s", appclient.EnvRuntimeVersion)
	}
}

// TestRuntimeEnvNamesDoNotCollideWithClientEnv is the deployment half of the
// MIR-1406 regression guard: every var Miren injects into a sandbox must live
// under the reserved MIREN_RUNTIME_ sub-namespace. That is what makes collisions
// impossible for any CLI var, present or future — the CLI reads no var under that
// prefix. The CLI half, which derives the CLI's env-tag names by reflection and
// asserts none reads an injected var, lives in cli/commands
// (TestCLIEnvTagsDoNotReadInjectedVars) so it stays next to the flags it checks.
func TestRuntimeEnvNamesDoNotCollideWithClientEnv(t *testing.T) {
	for _, injected := range appclient.RuntimeEnvNames {
		assert.True(t, strings.HasPrefix(injected, "MIREN_RUNTIME_"),
			"%s is injected but not under MIREN_RUNTIME_ — it could collide with a CLI var", injected)
	}
}

// TestCreatePoolForVersionEphemeral verifies that the web pool of an ephemeral
// AppVersion is seeded at DesiredInstances=1 even when the user's web config
// asks for a higher fixed count. EphemeralStrategy handles the cap at runtime.
func TestCreatePoolForVersionEphemeral(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	version := &core_v1alpha.AppVersion{
		App:            app.ID,
		Version:        "v1",
		ImageUrl:       "test:latest",
		EphemeralLabel: "feat-x",
		EphemeralTtl:   "48h",
		Config: core_v1alpha.Config{
			Port: 3000,
			Services: []core_v1alpha.Services{
				{
					Name: "web",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						// Even if the config asks for many instances, an ephemeral
						// deploy must stay at 1.
						Mode:         "fixed",
						NumInstances: 5,
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", version)
	require.NoError(t, err)
	version.ID = verID

	launcher := newTestLauncher(log, server.EAC)

	// Ephemeral versions do not set ActiveVersion. The activator drives pool
	// creation via CreatePoolForVersion for these.
	poolID, err := launcher.CreatePoolForVersion(ctx, version, "web")
	require.NoError(t, err)
	assert.NotEmpty(t, poolID, "expected a new pool to be created")

	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1, "should create exactly one pool")

	pool := pools[0]
	assert.Equal(t, int64(1), pool.DesiredInstances, "ephemeral pool must seed DesiredInstances at 1 regardless of fixed-mode config")
	assert.Equal(t, "web", pool.Service)
	assert.Contains(t, pool.ReferencedByVersions, version.ID)
}

// TestNonEphemeralPoolUnaffected guards against the ephemeral changes leaking
// into normal deploys: a non-ephemeral fixed-mode version must still get the
// configured instance count.
func TestNonEphemeralPoolUnaffected(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	version := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "test:latest",
		// No EphemeralLabel: this is a normal deploy.
		Config: core_v1alpha.Config{
			Port: 3000,
			Services: []core_v1alpha.Services{
				{
					Name: "postgres",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 3,
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", version)
	require.NoError(t, err)
	version.ID = verID

	app.ActiveVersion = version.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	launcher := newTestLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1)

	pool := pools[0]
	assert.Equal(t, int64(3), pool.DesiredInstances, "non-ephemeral fixed mode must honor configured NumInstances")
}

// TestEphemeralNonWebHonorsFixedConcurrency guards the supporting-service
// case: a "db" service on an ephemeral preview deploy is not the routed web
// service, so it must honor its configured fixed-mode NumInstances rather
// than getting capped to 1 by the ephemeral routing.
func TestEphemeralNonWebHonorsFixedConcurrency(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	version := &core_v1alpha.AppVersion{
		App:            app.ID,
		Version:        "v1",
		ImageUrl:       "test:latest",
		EphemeralLabel: "feat-x",
		EphemeralTtl:   "48h",
		Config: core_v1alpha.Config{
			Port: 3000,
			Services: []core_v1alpha.Services{
				{
					Name: "web",
					// Default auto-mode concurrency; ephemeral routing caps at 1 anyway.
				},
				{
					Name: "db",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 3,
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", version)
	require.NoError(t, err)
	version.ID = verID

	launcher := newTestLauncher(log, server.EAC)

	webPoolID, err := launcher.CreatePoolForVersion(ctx, version, "web")
	require.NoError(t, err)
	assert.NotEmpty(t, webPoolID)

	dbPoolID, err := launcher.CreatePoolForVersion(ctx, version, "db")
	require.NoError(t, err)
	assert.NotEmpty(t, dbPoolID)

	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 2, "should create one pool per service")

	var webPool, dbPool *compute_v1alpha.SandboxPool
	for i := range pools {
		switch pools[i].Service {
		case "web":
			webPool = &pools[i]
		case "db":
			dbPool = &pools[i]
		}
	}
	require.NotNil(t, webPool, "web pool should exist")
	require.NotNil(t, dbPool, "db pool should exist")

	assert.Equal(t, int64(1), webPool.DesiredInstances,
		"ephemeral web pool seeds at 1 regardless of config")
	assert.Equal(t, int64(3), dbPool.DesiredInstances,
		"db service on ephemeral version honors its fixed-mode NumInstances")
}

// TestEphemeralVersionDoesNotReuseExistingPool guards the isolation invariant:
// an ephemeral version with an identical SandboxSpec to an existing
// non-ephemeral version must NOT reuse the existing pool. Sharing a pool would
// mean the pool's scaling behavior depends on which version is making the
// request (different strategies for ephemeral vs non-ephemeral), which leaves
// the activator unable to register the ephemeral version on an existing
// running sandbox.
func TestEphemeralVersionDoesNotReuseExistingPool(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	svcCfg := core_v1alpha.Config{
		Port: 3000,
		Services: []core_v1alpha.Services{
			{
				Name: "web",
				ServiceConcurrency: core_v1alpha.ServiceConcurrency{
					Mode:                "auto",
					RequestsPerInstance: 10,
				},
			},
		},
	}

	// First deploy a normal version and let reconcile create its pool.
	normalVer := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "normal",
		ImageUrl: "test:shared-spec",
		Config:   svcCfg,
	}
	normalID, err := server.Client.Create(ctx, "normal-ver", normalVer)
	require.NoError(t, err)
	normalVer.ID = normalID

	app.ActiveVersion = normalVer.ID
	err = server.Client.Update(ctx, app)
	require.NoError(t, err)

	launcher := newTestLauncher(log, server.EAC)
	err = launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err)

	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1, "normal deploy should create exactly one pool")

	// Now deploy an ephemeral version with identical spec. The launcher must
	// create a fresh pool rather than reuse the existing one.
	ephemeralVer := &core_v1alpha.AppVersion{
		App:            app.ID,
		Version:        "ephem",
		ImageUrl:       "test:shared-spec",
		EphemeralLabel: "feat-x",
		Config:         svcCfg,
	}
	verID, err := server.Client.Create(ctx, "ephem-ver", ephemeralVer)
	require.NoError(t, err)
	ephemeralVer.ID = verID

	_, err = launcher.CreatePoolForVersion(ctx, ephemeralVer, "web")
	require.NoError(t, err)

	pools = listAllPools(t, ctx, server)
	require.Len(t, pools, 2, "ephemeral and non-ephemeral pools must not share")

	// Verify the ephemeral pool references only the ephemeral version.
	var ephPool, normalPool *compute_v1alpha.SandboxPool
	for i := range pools {
		refs := pools[i].ReferencedByVersions
		if len(refs) == 1 && refs[0] == ephemeralVer.ID {
			ephPool = &pools[i]
		} else if len(refs) == 1 && refs[0] == normalVer.ID {
			normalPool = &pools[i]
		}
	}
	require.NotNil(t, ephPool, "ephemeral version should have its own pool")
	require.NotNil(t, normalPool, "normal version's pool should remain unshared")
}

// TestStaleStatelessPoolReapedOnSameVersionSpecChange reproduces MIR-1432: a
// binary change (not a new AppVersion) alters the baked sandbox spec, so the old
// pool fails specsMatch and a replacement is created — but the old pool still
// references the *current* version, so cleanupOldVersionPools (which keys on
// version references) never reaps it. On garden this stranded a running duplicate
// of every stateless app for ~15 hours. reapStaleStatelessPools must scale the
// husk to zero.
//
// We simulate the binary-driven spec change by mutating the active version's
// image in place (a scalar field, so the store Put replaces it cleanly) and
// re-reconciling the same version. What matters is the invariant the incident
// exercised: spec mismatch while the pool still references the current version.
func TestStaleStatelessPoolReapedOnSameVersionSpecChange(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{Project: entity.Id("project-1")}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	ver := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "oci.miren.cloud/web:1",
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
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-v1", ver)
	require.NoError(t, err)
	ver.ID = verID

	app.ActiveVersion = ver.ID
	require.NoError(t, server.Client.Update(ctx, app))

	launcher := newTestLauncher(log, server.EAC)
	require.NoError(t, launcher.Reconcile(ctx, app, nil))

	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1, "one pool for the initial deploy")
	oldPoolID := pools[0].ID
	require.Contains(t, pools[0].ReferencedByVersions, ver.ID)

	// Binary-driven spec change on the SAME version: the baked image differs, so
	// the existing pool no longer matches, but the version ID is unchanged.
	ver.ImageUrl = "oci.miren.cloud/web:2"
	require.NoError(t, server.Client.Update(ctx, ver))

	require.NoError(t, launcher.Reconcile(ctx, app, nil))

	pools = listAllPools(t, ctx, server)
	require.Len(t, pools, 2, "replacement pool created alongside the old one")

	var oldPool, newPool *compute_v1alpha.SandboxPool
	for i := range pools {
		if pools[i].ID == oldPoolID {
			oldPool = &pools[i]
		} else {
			newPool = &pools[i]
		}
	}
	require.NotNil(t, oldPool, "old pool should still exist")
	require.NotNil(t, newPool, "new pool should have been created")

	// The core assertion: the stranded husk is reaped, not left running.
	assert.Equal(t, int64(0), oldPool.DesiredInstances,
		"stale same-version pool must be scaled to 0 (MIR-1432)")
	assert.Empty(t, oldPool.ReferencedByVersions,
		"reaped pool should have its version references cleared")

	// And the replacement is the live one.
	assert.Equal(t, "oci.miren.cloud/web:2", newPool.SandboxSpec.Container[0].Image)
	assert.GreaterOrEqual(t, newPool.DesiredInstances, int64(1),
		"replacement pool should be running")
	assert.Contains(t, newPool.ReferencedByVersions, ver.ID)
}

// TestSteadyStateReconcileDoesNotReap guards against the reap firing on a no-op
// pass: when nothing has changed, the single matching pool must be left exactly
// as-is (no scale-to-zero, no churn — the reconciler runs every minute).
func TestSteadyStateReconcileDoesNotReap(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{Project: entity.Id("project-1")}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	ver := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "oci.miren.cloud/web:1",
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
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-v1", ver)
	require.NoError(t, err)
	ver.ID = verID

	app.ActiveVersion = ver.ID
	require.NoError(t, server.Client.Update(ctx, app))

	launcher := newTestLauncher(log, server.EAC)
	require.NoError(t, launcher.Reconcile(ctx, app, nil))

	before := listAllPools(t, ctx, server)
	require.Len(t, before, 1)

	// Reconcile again with nothing changed.
	require.NoError(t, launcher.Reconcile(ctx, app, nil))

	after := listAllPools(t, ctx, server)
	require.Len(t, after, 1, "steady-state reconcile must not create or drop pools")
	assert.Equal(t, before[0].ID, after[0].ID, "same pool reused")
	assert.Equal(t, int64(1), after[0].DesiredInstances,
		"steady-state reconcile must not scale the live pool down")
	assert.Contains(t, after[0].ReferencedByVersions, ver.ID)
}

// TestCreatePoolForVersionSerializesWithReconcile proves Fix B: the activator's
// on-demand CreatePoolForVersion contends on the same per-app mutex Reconcile
// holds. Without the shared lock, an activator-create racing a reconciler-create
// can each miss the other's in-flight pool and both create one, duplicating every
// sandbox (the tempo two-web-pools finding). We hold the app mutex and assert
// CreatePoolForVersion blocks until it's released.
func TestCreatePoolForVersionSerializesWithReconcile(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{Project: entity.Id("project-1")}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	ver := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "oci.miren.cloud/web:1",
		Config: core_v1alpha.Config{
			Port: 3000,
			Services: []core_v1alpha.Services{
				{Name: "web"},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-v1", ver)
	require.NoError(t, err)
	ver.ID = verID

	launcher := newTestLauncher(log, server.EAC)

	// Take the per-app mutex Reconcile would hold, keyed exactly as both paths key
	// it (on the app ID). CreatePoolForVersion keys on ver.App, which is app.ID.
	val, _ := launcher.appMu.LoadOrStore(app.ID, &sync.Mutex{})
	mu := val.(*sync.Mutex)
	mu.Lock()

	done := make(chan error, 1)
	go func() {
		_, cErr := launcher.CreatePoolForVersion(ctx, ver, "web")
		done <- cErr
	}()

	// While we hold the mutex, CreatePoolForVersion must not make progress.
	select {
	case <-done:
		mu.Unlock()
		t.Fatal("CreatePoolForVersion did not block on the per-app mutex — Fix B regressed")
	case <-time.After(100 * time.Millisecond):
		// Still blocked, as expected.
	}

	// Release the mutex; CreatePoolForVersion should now complete.
	mu.Unlock()
	select {
	case cErr := <-done:
		require.NoError(t, cErr)
	case <-time.After(2 * time.Second):
		t.Fatal("CreatePoolForVersion did not complete after mutex release")
	}

	// Exactly one pool — no duplicate creation.
	pools := listAllPools(t, ctx, server)
	assert.Len(t, pools, 1, "should create exactly one pool")
}

// TestReapSkipsServicesWithoutEnsuredReplacement guards the gate that keeps
// reaping from scaling down a service's only pool when this pass failed to
// ensure a replacement for it. A stale pool must survive when its service is
// absent from ensuredServices, and only get reaped once the service is present.
func TestReapSkipsServicesWithoutEnsuredReplacement(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{Project: entity.Id("project-1")}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	ver := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "oci.miren.cloud/web:1",
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
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-v1", ver)
	require.NoError(t, err)
	ver.ID = verID

	app.ActiveVersion = ver.ID
	require.NoError(t, server.Client.Update(ctx, app))

	launcher := newTestLauncher(log, server.EAC)
	require.NoError(t, launcher.Reconcile(ctx, app, nil))

	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1)
	poolID := pools[0].ID
	require.Equal(t, int64(1), pools[0].DesiredInstances)

	// Resolve the same spec reconcile used, then make the live pool stale purely
	// via an image change on the version we pass to the reaper.
	spec, err := coreutil.ResolveConfig(ctx, server.EAC, ver)
	require.NoError(t, err)
	ver.ImageUrl = "oci.miren.cloud/web:2"

	// Replacement NOT ensured this pass: the stale pool must be left alone.
	reaped, err := launcher.reapStaleStatelessPools(ctx, app, ver, spec, map[string]bool{})
	require.NoError(t, err)
	assert.Equal(t, 0, reaped, "no reaping when the service's replacement wasn't ensured")

	got, err := server.EAC.Get(ctx, poolID.String())
	require.NoError(t, err)
	var pool compute_v1alpha.SandboxPool
	pool.Decode(got.Entity().Entity())
	assert.Equal(t, int64(1), pool.DesiredInstances,
		"stale pool must survive while its replacement is missing")

	// Replacement ensured: now the stale pool is reaped.
	reaped, err = launcher.reapStaleStatelessPools(ctx, app, ver, spec, map[string]bool{"web": true})
	require.NoError(t, err)
	assert.Equal(t, 1, reaped, "stale pool reaped once its replacement is ensured")

	got, err = server.EAC.Get(ctx, poolID.String())
	require.NoError(t, err)
	pool.Decode(got.Entity().Entity())
	assert.Equal(t, int64(0), pool.DesiredInstances, "stale pool scaled to 0")
}

// TestConcurrentCreateAndReconcileNoDeadlock fires both lock-taking entry points
// at the same app concurrently and asserts they both return (no deadlock under
// contention) and converge on a single pool. The shared appMu is a single,
// non-nested lock class, so a cycle isn't structurally possible; this is the
// runtime backstop for that reasoning. Run under -race to catch data races too.
func TestConcurrentCreateAndReconcileNoDeadlock(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{Project: entity.Id("project-1")}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	ver := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "oci.miren.cloud/web:1",
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
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-v1", ver)
	require.NoError(t, err)
	ver.ID = verID

	app.ActiveVersion = ver.ID
	require.NoError(t, server.Client.Update(ctx, app))

	launcher := newTestLauncher(log, server.EAC)

	var wg sync.WaitGroup
	var createErr, reconcileErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, createErr = launcher.CreatePoolForVersion(ctx, ver, "web")
	}()
	go func() {
		defer wg.Done()
		reconcileErr = launcher.Reconcile(ctx, app, nil)
	}()

	waited := make(chan struct{})
	go func() {
		wg.Wait()
		close(waited)
	}()

	select {
	case <-waited:
		// Both returned — no deadlock.
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent CreatePoolForVersion + Reconcile deadlocked")
	}

	require.NoError(t, createErr, "CreatePoolForVersion should not error under contention")
	require.NoError(t, reconcileErr, "Reconcile should not error under contention")

	// Whichever ran the findMatchingPool scan second reuses the first pool, so
	// contention converges on exactly one pool rather than duplicating it.
	pools := listAllPools(t, ctx, server)
	assert.Len(t, pools, 1, "concurrent create + reconcile must converge on one pool")
}
