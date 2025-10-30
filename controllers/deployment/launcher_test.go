package deployment

import (
	"context"
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
// desired_instances=0 for auto-mode services (activator will scale up on demand)
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

	// Verify pool was created with desired_instances=0
	pools := listAllPools(t, ctx, server)
	require.Len(t, pools, 1, "should create one pool")

	pool := pools[0]
	assert.Equal(t, "web", pool.Service, "pool should be for web service")
	assert.Equal(t, int64(0), pool.DesiredInstances, "auto mode should start with desired_instances=0")
	assert.Equal(t, version.ID, pool.SandboxSpec.Version, "pool should reference version")
}

// TestPoolReuseOnConfigChange tests that DeploymentLauncher reuses existing
// pools when SandboxSpec matches (e.g., only concurrency settings changed)
func TestPoolReuseOnConfigChange(t *testing.T) {
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
		ImageUrl: "postgres:16",
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

	// Create v2 with same image and env vars, only concurrency settings changed
	v2 := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v2",
		ImageUrl: "postgres:16", // Same image
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
}

// TestNewPoolOnImageChange tests that DeploymentLauncher creates a new pool
// when the image changes (SandboxSpec doesn't match)
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
		ImageUrl: "postgres:16",
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
		ImageUrl: "postgres:17", // Image changed
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

	// Verify old pool was scaled down
	var poolV1 *compute_v1alpha.SandboxPool
	for i := range poolsV2 {
		if poolsV2[i].ID == poolV1ID {
			poolV1 = &poolsV2[i]
			break
		}
	}
	require.NotNil(t, poolV1, "old pool should still exist")
	t.Logf("Old pool state: DesiredInstances=%d, ReferencedByVersions=%v", poolV1.DesiredInstances, poolV1.ReferencedByVersions)
	assert.Equal(t, int64(0), poolV1.DesiredInstances, "old pool should be scaled to 0")
	assert.NotContains(t, poolV1.ReferencedByVersions, v2.ID, "old pool should not reference v2")
	assert.Len(t, poolV1.ReferencedByVersions, 0, "old pool should have no version references")
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

	// Verify postgres pool was scaled to 0
	poolsV2 := listAllPools(t, ctx, server)
	require.Len(t, poolsV2, 1, "pool should still exist")
	assert.Equal(t, int64(0), poolsV2[0].DesiredInstances, "postgres pool should be scaled to 0")
	assert.NotContains(t, poolsV2[0].ReferencedByVersions, v2.ID, "pool should not reference v2")
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
	assert.Equal(t, int64(0), webPool.DesiredInstances, "web (auto) should start at 0")

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
