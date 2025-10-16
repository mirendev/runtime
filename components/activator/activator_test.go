package activator

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	apiserver "miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/concurrency"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
)

// Test that retirement properly removes old sandboxes
func TestActivatorRetireUnusedSandboxes(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create in-memory entity server
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create entities in the store
	ver := &core_v1alpha.AppVersion{
		ID:  entity.Id("ver-1"),
		App: entity.Id("app-1"),
		Config: core_v1alpha.Config{
			Services: []core_v1alpha.Services{
				{
					Name: "web",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:                "auto",
						RequestsPerInstance: 10,
						ScaleDownDelay:      "2m", // 2 minute scale down for testing
					},
				},
			},
		},
	}

	// Create sandbox entities using the entityserver.Client
	sb1 := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.RUNNING,
	}
	sb1ID, err := server.Client.Create(ctx, "sb-1", sb1)
	require.NoError(t, err)
	sb1.ID = sb1ID

	sb2 := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.RUNNING,
	}
	sb2ID, err := server.Client.Create(ctx, "sb-2", sb2)
	require.NoError(t, err)
	sb2.ID = sb2ID

	sb3 := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.STOPPED,
	}
	sb3ID, err := server.Client.Create(ctx, "sb-3", sb3)
	require.NoError(t, err)
	sb3.ID = sb3ID

	// Create trackers for each sandbox
	strategy := concurrency.NewStrategy(&core_v1alpha.ServiceConcurrency{
		Mode:                "auto",
		RequestsPerInstance: 10,
		ScaleDownDelay:      "2m",
	})

	tracker1 := strategy.InitializeTracker()
	tracker2 := strategy.InitializeTracker()
	// Acquire some capacity for sb2 to simulate in-use
	tracker2.AcquireLease()
	tracker2.AcquireLease() // 2 leases = 4 slots used
	tracker3 := strategy.InitializeTracker()

	// Create activator with sandboxes to test
	activator := &localActivator{
		log: log,
		eac: server.EAC,
		versions: map[verKey]*verSandboxes{
			{"ver-1", "web"}: {
				ver: ver,
				sandboxes: []*sandbox{
					// Old sandbox - should be retired
					{
						sandbox:     sb1,
						ent:         server.GetEntity(sb1.ID),
						lastRenewal: time.Now().Add(-3 * time.Minute),
						url:         "http://localhost:3000",
						tracker:     tracker1,
					},
					// Recent sandbox - should NOT be retired
					{
						sandbox:     sb2,
						ent:         server.GetEntity(sb2.ID),
						lastRenewal: time.Now().Add(-30 * time.Second),
						url:         "http://localhost:3001",
						tracker:     tracker2,
					},
					// Already stopped - should be removed from list
					{
						sandbox:     sb3,
						ent:         server.GetEntity(sb3.ID),
						lastRenewal: time.Now().Add(-5 * time.Minute),
						url:         "http://localhost:3002",
						tracker:     tracker3,
					},
				},
				strategy: strategy,
			},
		},
	}

	// Count initial sandboxes
	initialCount := len(activator.versions[verKey{ver: "ver-1", service: "web"}].sandboxes)
	assert.Equal(t, 3, initialCount)

	// Run retirement
	activator.retireUnusedSandboxes()

	// Check that non-RUNNING sandbox was removed and old sandbox was marked for retirement
	vs := activator.versions[verKey{ver: "ver-1", service: "web"}]
	assert.Equal(t, 1, len(vs.sandboxes), "should only have recent sandbox left")
	assert.Equal(t, sb2.ID, vs.sandboxes[0].sandbox.ID, "should be the recent sandbox")

	// Wait for async update with timeout
	require.Eventually(t, func() bool {
		resp, err := server.EAC.Get(ctx, sb1.ID.String())
		if err != nil {
			return false
		}
		var updatedSb compute_v1alpha.Sandbox
		updatedSb.Decode(resp.Entity().Entity())
		return updatedSb.Status == compute_v1alpha.STOPPED
	}, 1*time.Second, 10*time.Millisecond, "sandbox should be marked as stopped")
}

// Test auto mode slot tracking mechanics
func TestActivatorAutoModeSlotTracking(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	// Create in-memory entity server
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app entity
	app := &core_v1alpha.App{}
	appID, err := server.Client.Create(ctx, "test-auto-slots", app)
	require.NoError(t, err)
	app.ID = appID

	// Create app version with auto mode (10 requests per instance = 2 slot leases)
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
						Mode:                "auto",
						RequestsPerInstance: 10,
						ScaleDownDelay:      "15m",
					},
				},
			},
		},
	}

	verID, err := server.Client.Create(ctx, "test-auto-v1", appVer)
	require.NoError(t, err)
	appVer.ID = verID

	// Create activator with one sandbox (10 max slots, 2 in use initially)
	sb1 := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.RUNNING,
		Network: []compute_v1alpha.Network{
			{Address: "10.0.0.1"},
		},
	}

	strategy := concurrency.NewStrategy(&core_v1alpha.ServiceConcurrency{
		Mode:                "auto",
		RequestsPerInstance: 10,
		ScaleDownDelay:      "15m",
	})
	tracker := strategy.InitializeTracker()
	// Acquire one lease to simulate initial state
	tracker.AcquireLease()

	activator := &localActivator{
		log: log.With("module", "activator"),
		eac: server.EAC,
		versions: map[verKey]*verSandboxes{
			{appVer.ID.String(), "web"}: {
				ver: appVer,
				sandboxes: []*sandbox{
					{
						sandbox:     sb1,
						ent:         &entity.Entity{ID: entity.Id("sb-1")},
						lastRenewal: time.Now(),
						url:         "http://10.0.0.1:3000",
						tracker:     tracker,
					},
				},
				strategy: strategy,
			},
		},
	}

	key := verKey{appVer.ID.String(), "web"}
	vs := activator.versions[key]
	s := vs.sandboxes[0]

	// Initial state: 2 slots in use (one lease)
	assert.Equal(t, 2, s.tracker.Used(), "should start with one lease's worth")

	// Acquire first additional lease - should succeed
	lease1, err := activator.AcquireLease(ctx, appVer, "web")
	require.NoError(t, err)
	require.NotNil(t, lease1)
	assert.Equal(t, 2, lease1.Size, "lease size should be 20% of 10 = 2")
	assert.Equal(t, 4, s.tracker.Used(), "should increment to 4 slots")

	// Acquire second additional lease - should succeed
	lease2, err := activator.AcquireLease(ctx, appVer, "web")
	require.NoError(t, err)
	require.NotNil(t, lease2)
	assert.Equal(t, 2, lease2.Size)
	assert.Equal(t, 6, s.tracker.Used(), "should increment to 6 slots")

	// Acquire third additional lease - should succeed
	lease3, err := activator.AcquireLease(ctx, appVer, "web")
	require.NoError(t, err)
	require.NotNil(t, lease3)
	assert.Equal(t, 8, s.tracker.Used(), "should increment to 8 slots")

	// Try to acquire fourth lease - should succeed (8+2=10, at capacity)
	lease4, err := activator.AcquireLease(ctx, appVer, "web")
	require.NoError(t, err)
	require.NotNil(t, lease4)
	assert.Equal(t, 10, s.tracker.Used(), "should be at max capacity")

	// Try to acquire fifth lease - should fail (10+2 > 10)
	// Since we don't have sandbox creation wired up, this will try to acquire
	// and find no capacity - the test setup doesn't include activateApp
	// so we just verify the sandbox is full
	assert.Equal(t, 10, s.tracker.Used(), "sandbox should be full")
	assert.False(t, s.tracker.HasCapacity(), "should have no capacity")

	// Release lease2 - should free 2 slots
	err = activator.ReleaseLease(ctx, lease2)
	require.NoError(t, err)
	assert.Equal(t, 8, s.tracker.Used(), "should decrement to 8 slots")
	assert.True(t, s.tracker.HasCapacity(), "should have capacity again")

	// Release lease1 - should free 2 more slots
	err = activator.ReleaseLease(ctx, lease1)
	require.NoError(t, err)
	assert.Equal(t, 6, s.tracker.Used(), "should decrement to 6 slots")

	// Release lease3 and lease4
	err = activator.ReleaseLease(ctx, lease3)
	require.NoError(t, err)
	assert.Equal(t, 4, s.tracker.Used())

	err = activator.ReleaseLease(ctx, lease4)
	require.NoError(t, err)
	assert.Equal(t, 2, s.tracker.Used(), "should be back to initial state")
}

// Test lease operations
func TestActivatorLeaseOperations(t *testing.T) {
	log := slog.New(slog.NewTextHandler(nil, nil))

	testVer := &core_v1alpha.AppVersion{
		ID:       entity.Id("ver-1"),
		App:      entity.Id("app-1"),
		Version:  "v1",
		ImageUrl: "test:latest",
		Config: core_v1alpha.Config{
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

	strategy := concurrency.NewStrategy(&core_v1alpha.ServiceConcurrency{
		Mode:                "auto",
		RequestsPerInstance: 10,
		ScaleDownDelay:      "15m",
	})
	tracker := strategy.InitializeTracker()

	testSandbox := &sandbox{
		sandbox: &compute_v1alpha.Sandbox{
			ID:     entity.Id("sb-1"),
			Status: compute_v1alpha.RUNNING,
		},
		ent:         &entity.Entity{ID: entity.Id("sb-1")},
		lastRenewal: time.Now(),
		url:         "http://localhost:3000",
		tracker:     tracker,
	}

	activator := &localActivator{
		log: log,
		versions: map[verKey]*verSandboxes{
			{"ver-1", "web"}: {
				ver:       testVer,
				sandboxes: []*sandbox{testSandbox},
				strategy:  strategy,
			},
		},
	}

	// Test ReleaseLease
	lease := &Lease{
		ver:     testVer,
		sandbox: testSandbox.sandbox,
		pool:    "default",
		service: "web",
		Size:    2,
	}

	// Acquire slots
	tracker.AcquireLease()

	// Release lease
	err := activator.ReleaseLease(t.Context(), lease)
	require.NoError(t, err)

	// Verify slots were released
	assert.Equal(t, 0, tracker.Used())
}

// Test concurrent access safety
func TestActivatorConcurrentSafety(t *testing.T) {
	log := slog.New(slog.NewTextHandler(nil, nil))

	activator := &localActivator{
		log:      log,
		versions: make(map[verKey]*verSandboxes),
	}

	// Run multiple goroutines accessing the versions map
	done := make(chan bool, 3)

	// Goroutine 1: Add versions
	go func() {
		for range 100 {
			activator.mu.Lock()
			activator.versions[verKey{ver: "ver-1", service: "web"}] = &verSandboxes{
				sandboxes: []*sandbox{},
			}
			activator.mu.Unlock()
		}
		done <- true
	}()

	// Goroutine 2: Read versions
	go func() {
		for range 100 {
			activator.mu.Lock()
			_ = activator.versions[verKey{ver: "ver-1", service: "web"}]
			activator.mu.Unlock()
		}
		done <- true
	}()

	// Goroutine 3: Delete versions
	go func() {
		for range 100 {
			activator.mu.Lock()
			delete(activator.versions, verKey{ver: "ver-1", service: "web"})
			activator.mu.Unlock()
		}
		done <- true
	}()

	// Wait for all goroutines
	for range 3 {
		<-done
	}

	// Test passed if no race condition occurred
}

// Test activator sandbox recovery with real entity server
func TestActivatorRecoverSandboxesWithEntityServer(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Create in-memory entity server
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create and store app version
	appVer := &core_v1alpha.AppVersion{
		App:      entity.Id("app-recovery-1"),
		Version:  "v1",
		ImageUrl: "test:latest",
		Config: core_v1alpha.Config{
			Port: 8080,
			Services: []core_v1alpha.Services{
				{
					Name: "web",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:                "auto",
						RequestsPerInstance: 10,
						ScaleDownDelay:      "2m",
					},
				},
			},
		},
	}

	verID, err := server.Client.Create(ctx, "test-ver-1", appVer)
	require.NoError(t, err)
	appVer.ID = verID

	// Create running sandbox
	sb := &compute_v1alpha.Sandbox{
		Version: appVer.ID,
		Status:  compute_v1alpha.RUNNING,
		Network: []compute_v1alpha.Network{
			{
				Address: "10.0.0.100",
			},
		},
	}

	// Create sandbox using entityserver.Client with labels
	sbID, err := server.Client.Create(ctx, "sb-recovery-test", sb,
		apiserver.WithLabels(types.LabelSet("app", "app-recovery-1", "pool", "production")))
	require.NoError(t, err)
	sb.ID = sbID

	// Create stopped sandbox (should be ignored)
	sb2 := &compute_v1alpha.Sandbox{
		Version: appVer.ID,
		Status:  compute_v1alpha.STOPPED,
		Network: []compute_v1alpha.Network{
			{
				Address: "10.0.0.101",
			},
		},
	}
	// Create stopped sandbox using entityserver.Client
	_, err = server.Client.Create(ctx, "sb-recovery-test2", sb2,
		apiserver.WithLabels(types.LabelSet("app", "app-recovery-1", "pool", "production")))
	require.NoError(t, err)

	// Create activator with the real entity access client
	activator := &localActivator{
		log:      log,
		eac:      server.EAC,
		versions: make(map[verKey]*verSandboxes),
	}

	// Test recovery
	err = activator.recoverSandboxes(ctx)
	require.NoError(t, err)

	// Verify sandbox was recovered
	key := verKey{ver: appVer.ID.String(), service: "web"}
	vs, ok := activator.versions[key]
	require.True(t, ok, "version should be in map")
	require.Len(t, vs.sandboxes, 1, "should have recovered 1 running sandbox")

	// Verify sandbox details
	recoveredSb := vs.sandboxes[0]
	assert.Equal(t, sb.ID, recoveredSb.sandbox.ID)
	assert.Equal(t, compute_v1alpha.RUNNING, recoveredSb.sandbox.Status)
	assert.Equal(t, "http://10.0.0.100:8080", recoveredSb.url)
	assert.Equal(t, 10, recoveredSb.tracker.Max())
	assert.Equal(t, 0, recoveredSb.tracker.Used())
	assert.WithinDuration(t, time.Now(), recoveredSb.lastRenewal, 5*time.Second)

	// Test that we can retire the sandbox
	vs.sandboxes[0].lastRenewal = time.Now().Add(-3 * time.Minute)
	activator.retireUnusedSandboxes()

	// Verify sandbox was removed from tracking
	assert.Len(t, vs.sandboxes, 0, "sandbox should have been retired")

	// Wait for the async update with timeout
	require.Eventually(t, func() bool {
		updatedResp, err := server.EAC.Get(ctx, sb.ID.String())
		if err != nil {
			return false
		}
		updatedEnt := updatedResp.Entity().Entity()
		var updatedSb compute_v1alpha.Sandbox
		updatedSb.Decode(updatedEnt)
		return updatedSb.Status == compute_v1alpha.STOPPED
	}, 1*time.Second, 10*time.Millisecond, "sandbox should be updated to STOPPED status")
}

// TestActivatorRecoveryIntegration tests the full activator recovery scenario
func TestActivatorRecoveryIntegration(t *testing.T) {
	ctx := context.Background()

	// Create in-memory entity server for testing
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app using entityserver.Client
	app := &core_v1alpha.App{}
	appID, err := server.Client.Create(ctx, "test-recovery-app", app)
	require.NoError(t, err)
	app.ID = appID

	// Create app version using entityserver.Client
	appVer := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "test:latest",
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
				{
					Name: "worker",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 1,
					},
				},
			},
		},
	}

	verID, err := server.Client.Create(ctx, "test-recovery-v1", appVer)
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

	// Simulate existing sandboxes that would be present after a restart
	// Create 3 sandboxes: 1 running web, 1 stopped web, 1 running worker
	services := []string{"web", "web", "worker"}
	statuses := []compute_v1alpha.SandboxStatus{compute_v1alpha.RUNNING, compute_v1alpha.STOPPED, compute_v1alpha.RUNNING}
	addresses := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}

	for i := 0; i < 3; i++ {
		sb := &compute_v1alpha.Sandbox{
			Version: appVer.ID,
			Status:  statuses[i],
			Network: []compute_v1alpha.Network{
				{Address: addresses[i]},
			},
		}

		name := "sb-recovery-" + string(rune('a'+i))
		_, err := server.Client.Create(ctx, name, sb,
			apiserver.WithLabels(types.LabelSet("app", "test-recovery-app", "service", services[i])))
		require.NoError(t, err)
	}

	// Create first activator instance
	log := testutils.TestLogger(t)
	activator1 := NewLocalActivator(ctx, log, server.EAC)

	// Verify sandboxes were recovered (2 running sandboxes)
	time.Sleep(100 * time.Millisecond) // Give recovery time to complete

	// Simulate the activator going away (as if runtime restarted)
	// In real scenario, activator1 would be gone, but we'll just create a new one

	// Create second activator instance (simulating restart)
	activator2 := NewLocalActivator(ctx, log, server.EAC)

	// Verify sandboxes were recovered again
	time.Sleep(100 * time.Millisecond)

	// Now test that the second activator can:
	// 1. Acquire a lease on existing web sandbox (should get the one running sandbox)
	lease, err := activator2.AcquireLease(ctx, appVer, "web")
	require.NoError(t, err)
	assert.NotNil(t, lease)
	assert.Equal(t, "http://10.0.0.1:8080", lease.URL)

	// 2. Release the lease
	err = activator2.ReleaseLease(ctx, lease)
	require.NoError(t, err)

	// 3. Acquire another lease for worker service (should get the worker sandbox)
	lease2, err := activator2.AcquireLease(ctx, appVer, "worker")
	require.NoError(t, err)
	assert.NotNil(t, lease2)
	assert.Equal(t, "http://10.0.0.3:8080", lease2.URL)

	// Note: The activators created by NewLocalActivator do not have background goroutines
	// that need explicit cleanup - they start a ticker that gets garbage collected.
	// If cleanup is needed in the future, consider adding a Close() method to the interface.
	_ = activator1
}

// TestActivatorRecoverSandboxesWithCIDR tests recovery of sandboxes with CIDR notation in addresses
func TestActivatorRecoverSandboxesWithCIDR(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create and store app version
	appVer := &core_v1alpha.AppVersion{
		App:      entity.Id("app-cidr-test"),
		Version:  "v1",
		ImageUrl: "test:latest",
		Config: core_v1alpha.Config{
			Port: 3000,
			Services: []core_v1alpha.Services{
				{
					Name: "web",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:                "auto",
						RequestsPerInstance: 5,
						ScaleDownDelay:      "15m",
					},
				},
			},
		},
	}

	verID, err := server.Client.Create(ctx, "ver-cidr-test", appVer)
	require.NoError(t, err)
	appVer.ID = verID

	// Create sandbox with CIDR notation in address
	sb := &compute_v1alpha.Sandbox{
		Version: appVer.ID,
		Status:  compute_v1alpha.RUNNING,
		Network: []compute_v1alpha.Network{
			{
				Address: "10.8.24.21/24", // CIDR notation that caused the bug
			},
		},
	}

	// Create sandbox using entityserver.Client with labels
	sbID, err := server.Client.Create(ctx, "sb-cidr-test", sb,
		apiserver.WithLabels(types.LabelSet("app", "app-cidr-test", "pool", "default")))
	require.NoError(t, err)
	sb.ID = sbID

	// Create activator with the real entity access client
	activator := &localActivator{
		eac:      server.EAC,
		log:      log,
		versions: make(map[verKey]*verSandboxes),
	}

	// Test recovery
	err = activator.recoverSandboxes(ctx)
	require.NoError(t, err)

	// Verify sandbox was recovered with correct URL (without CIDR notation)
	key := verKey{ver: appVer.ID.String(), service: "web"}
	vs, ok := activator.versions[key]
	require.True(t, ok, "version should be in map")
	require.Len(t, vs.sandboxes, 1, "should have recovered 1 running sandbox")

	// Verify the URL was built correctly without CIDR notation
	recoveredSb := vs.sandboxes[0]
	assert.Equal(t, "http://10.8.24.21:3000", recoveredSb.url, "URL should not contain CIDR notation")
	assert.Equal(t, sb.ID, recoveredSb.sandbox.ID)
	assert.Equal(t, compute_v1alpha.RUNNING, recoveredSb.sandbox.Status)
}
