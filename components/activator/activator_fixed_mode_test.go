package activator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

// TestActivatorFixedModeRoundRobin tests that fixed mode services
// round-robin across existing sandboxes without creating new ones
func TestActivatorFixedModeRoundRobin(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	// Create in-memory entity server
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app entity
	app := &core_v1alpha.App{}
	appID, err := server.Client.Create(ctx, "test-fixed-app", app)
	require.NoError(t, err)
	app.ID = appID

	// Create app version with fixed mode concurrency
	appVer := &core_v1alpha.AppVersion{
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

	verID, err := server.Client.Create(ctx, "test-fixed-v1", appVer)
	require.NoError(t, err)
	appVer.ID = verID

	// Update app with active version
	app.ActiveVersion = appVer.ID
	var updateEntity entityserver_v1alpha.Entity
	updateEntity.SetId(app.ID.String())
	updateEntity.SetAttrs(entity.Attrs(
		app.Encode,
	))
	_, err = server.EAC.Put(ctx, &updateEntity)
	require.NoError(t, err)

	// Create two running sandboxes
	sb1 := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.RUNNING,
		Network: []compute_v1alpha.Network{
			{Address: "10.0.0.1"},
		},
	}
	sb2 := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.RUNNING,
		Network: []compute_v1alpha.Network{
			{Address: "10.0.0.2"},
		},
	}

	// Create activator with pre-existing sandboxes
	activator := &localActivator{
		log: log.With("module", "activator"),
		eac: server.EAC,
		versions: map[verKey]*verSandboxes{
			{appVer.ID.String(), "default", "web"}: {
				ver: appVer,
				sandboxes: []*sandbox{
					{
						sandbox:     sb1,
						ent:         &entity.Entity{ID: entity.Id("sb-1")},
						lastRenewal: time.Now(),
						url:         "http://10.0.0.1:3000",
						maxSlots:    1,
						inuseSlots:  0,
					},
					{
						sandbox:     sb2,
						ent:         &entity.Entity{ID: entity.Id("sb-2")},
						lastRenewal: time.Now(),
						url:         "http://10.0.0.2:3000",
						maxSlots:    1,
						inuseSlots:  0,
					},
				},
				leaseSlots: 1,
			},
		},
	}

	// Track which sandboxes we get leases for
	sandboxURLs := make(map[string]int)

	// Acquire multiple leases - should round-robin
	for i := 0; i < 10; i++ {
		lease, err := activator.AcquireLease(ctx, appVer, "default", "web")
		require.NoError(t, err)
		require.NotNil(t, lease)

		sandboxURLs[lease.URL]++

		// For fixed mode, slot tracking shouldn't affect anything
		assert.Equal(t, 1, lease.Size)

		// Release the lease
		err = activator.ReleaseLease(ctx, lease)
		require.NoError(t, err)
	}

	// Both sandboxes should have been used
	assert.Equal(t, 2, len(sandboxURLs), "should use both sandboxes")

	// Verify distribution
	// The implementation uses a random starting point for each request,
	// so the distribution is random rather than strict round-robin
	totalRequests := 0
	for url, count := range sandboxURLs {
		t.Logf("Sandbox %s used %d times", url, count)
		assert.Greater(t, count, 0, "each sandbox should be used at least once")
		totalRequests += count
	}
	assert.Equal(t, 10, totalRequests, "all requests should be handled")

	// Verify no slots were tracked for fixed mode
	vs := activator.versions[verKey{appVer.ID.String(), "default", "web"}]
	for _, s := range vs.sandboxes {
		assert.Equal(t, 0, s.inuseSlots, "fixed mode should not track slots")
	}
}

// TestActivatorFixedModeNoSlotExhaustion tests that fixed mode never
// reports "no space" and creates new sandboxes unnecessarily
func TestActivatorFixedModeNoSlotExhaustion(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	// Create in-memory entity server
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app entity
	app := &core_v1alpha.App{}
	appID, err := server.Client.Create(ctx, "test-fixed-exhaust", app)
	require.NoError(t, err)
	app.ID = appID

	// Create app version with fixed mode
	appVer := &core_v1alpha.AppVersion{
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

	// Create activator with one sandbox
	activator := &localActivator{
		log: log.With("module", "activator"),
		eac: server.EAC,
		versions: map[verKey]*verSandboxes{
			{appVer.ID.String(), "default", "web"}: {
				ver: appVer,
				sandboxes: []*sandbox{
					{
						sandbox: &compute_v1alpha.Sandbox{
							ID:     entity.Id("sb-1"),
							Status: compute_v1alpha.RUNNING,
						},
						ent:         &entity.Entity{ID: entity.Id("sb-1")},
						lastRenewal: time.Now(),
						url:         "http://10.0.0.1:3000",
						maxSlots:    1,
						inuseSlots:  0,
					},
				},
				leaseSlots: 1,
			},
		},
	}

	// Acquire many leases simultaneously (without releasing)
	// For auto mode this would exhaust slots, but fixed mode should handle it fine
	leases := make([]*Lease, 0)
	for i := 0; i < 20; i++ {
		lease, err := activator.AcquireLease(ctx, appVer, "default", "web")
		require.NoError(t, err)
		require.NotNil(t, lease)
		assert.Equal(t, "http://10.0.0.1:3000", lease.URL, "should always use the same sandbox")
		leases = append(leases, lease)
	}

	// Verify still only one sandbox exists
	vs := activator.versions[verKey{appVer.ID.String(), "default", "web"}]
	assert.Equal(t, 1, len(vs.sandboxes), "should not create new sandboxes for fixed mode")

	// Release all leases
	for _, lease := range leases {
		err := activator.ReleaseLease(ctx, lease)
		require.NoError(t, err)
	}
}
