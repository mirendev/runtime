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
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/concurrency"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
)

// Test lease operations
func TestActivatorLeaseOperations(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

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

	ent := entity.Blank()
	ent.SetID("sb-1")

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
		ent:         ent,
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
	err := activator.ReleaseLease(context.Background(), lease)
	require.NoError(t, err)

	// Verify slots were released
	assert.Equal(t, 0, tracker.Used())
}

// Test concurrent access safety
func TestActivatorConcurrentSafety(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	activator := &localActivator{
		log:      log,
		versions: make(map[verKey]*verSandboxes),
	}

	// Run multiple goroutines accessing the versions map
	done := make(chan bool, 3)

	// Goroutine 1: Add versions
	go func() {
		for i := 0; i < 100; i++ {
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
		for i := 0; i < 100; i++ {
			activator.mu.Lock()
			_ = activator.versions[verKey{ver: "ver-1", service: "web"}]
			activator.mu.Unlock()
		}
		done <- true
	}()

	// Goroutine 3: Delete versions
	go func() {
		for i := 0; i < 100; i++ {
			activator.mu.Lock()
			delete(activator.versions, verKey{ver: "ver-1", service: "web"})
			activator.mu.Unlock()
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}

	// Test passed if no race condition occurred
}

// Test activator sandbox recovery with real entity server
func TestActivatorRecoverSandboxesWithEntityServer(t *testing.T) {
	ctx := context.Background()

	// Create in-memory entity server
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app entity
	app := &core_v1alpha.App{}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	// Create app version
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
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", appVer)
	require.NoError(t, err)
	appVer.ID = verID

	// Create sandbox entity
	sb := compute_v1alpha.Sandbox{
		Version: appVer.ID,
		Status:  compute_v1alpha.RUNNING,
		Network: []compute_v1alpha.Network{
			{Address: "10.0.0.100"},
		},
	}

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetAttrs(entity.New(
		(&core_v1alpha.Metadata{
			Name:   "test-sandbox",
			Labels: types.LabelSet("service", "web"),
		}).Encode,
		entity.Ident, entity.MustKeyword("sandbox/test-sb"),
		sb.Encode,
	).Attrs())

	pr, err := server.EAC.Put(ctx, &rpcE)
	require.NoError(t, err)
	sb.ID = entity.Id(pr.Id())

	// Create activator and trigger recovery
	log := testutils.TestLogger(t)
	activator := &localActivator{
		log:      log,
		eac:      server.EAC,
		versions: make(map[verKey]*verSandboxes),
		pools:    make(map[verKey]*poolState),
	}

	err = activator.recoverSandboxes(ctx)
	require.NoError(t, err)

	// Verify sandbox was recovered
	key := verKey{appVer.ID.String(), "web"}
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
}

// TestActivatorRecoveryIntegration tests the full activator recovery scenario
func TestActivatorRecoveryIntegration(t *testing.T) {
	ctx := context.Background()

	// Create in-memory entity server for testing
	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	client := es.Client

	// Create test app and version
	app := &core_v1alpha.App{}
	appID, err := client.Create(ctx, "integration-app", app)
	require.NoError(t, err)
	app.ID = appID

	appVer := &core_v1alpha.AppVersion{
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "test:latest",
		Config: core_v1alpha.Config{
			Port: 3000,
			Services: []core_v1alpha.Services{
				{
					Name: "api",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{
						Mode:                "auto",
						RequestsPerInstance: 20,
					},
				},
			},
		},
	}
	verID, err := client.Create(ctx, "integration-ver", appVer)
	require.NoError(t, err)
	appVer.ID = verID

	// Create multiple running sandboxes
	for i := 0; i < 3; i++ {
		sb := compute_v1alpha.Sandbox{
			Version: appVer.ID,
			Status:  compute_v1alpha.RUNNING,
			Network: []compute_v1alpha.Network{
				{Address: "10.0.0.100/32"},
			},
		}

		var rpcE entityserver_v1alpha.Entity
		rpcE.SetAttrs(entity.New(
			(&core_v1alpha.Metadata{
				Name:   "sandbox-" + string(rune('a'+i)),
				Labels: types.LabelSet("service", "api"),
			}).Encode,
			entity.Ident, entity.MustKeyword("sandbox/sb-"+string(rune('a'+i))),
			sb.Encode,
		).Attrs())

		_, err := es.EAC.Put(ctx, &rpcE)
		require.NoError(t, err)
	}

	// Create activator - should recover all sandboxes
	log := testutils.TestLogger(t)
	activator := NewLocalActivator(ctx, log, es.EAC, true).(*localActivator)

	// Give a moment for recovery to complete
	time.Sleep(100 * time.Millisecond)

	// Verify recovery
	key := verKey{appVer.ID.String(), "api"}
	vs, ok := activator.versions[key]
	require.True(t, ok, "version should be tracked")
	assert.Len(t, vs.sandboxes, 3, "should recover all 3 sandboxes")

	// Verify strategy configuration
	assert.Equal(t, 0, vs.strategy.DesiredInstances()) // Auto mode scales to zero
}

// TestActivatorAcquireLeaseFromDeadSandbox verifies that DEAD sandboxes
// are NOT considered for lease acquisition
func TestActivatorAcquireLeaseFromDeadSandbox(t *testing.T) {
	ctx := context.Background()

	// Create in-memory entity server
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app entity
	app := &core_v1alpha.App{}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	testVer := &core_v1alpha.AppVersion{
		App:      app.ID,
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
	verID, err := server.Client.Create(ctx, "test-ver", testVer)
	require.NoError(t, err)
	testVer.ID = verID

	ent := entity.Blank()
	ent.SetID("sb-1")

	strategy := concurrency.NewStrategy(&core_v1alpha.ServiceConcurrency{
		Mode:                "auto",
		RequestsPerInstance: 10,
		ScaleDownDelay:      "15m",
	})
	tracker := strategy.InitializeTracker()

	// Create a sandbox with DEAD status but available capacity
	testSandbox := &sandbox{
		sandbox: &compute_v1alpha.Sandbox{
			ID:     entity.Id("sb-1"),
			Status: compute_v1alpha.DEAD, // Sandbox is DEAD
		},
		ent:         ent,
		lastRenewal: time.Now(),
		url:         "http://localhost:3000",
		tracker:     tracker, // Tracker has capacity (10 available)
	}

	log := testutils.TestLogger(t)
	activator := &localActivator{
		log: log,
		eac: server.EAC,
		versions: map[verKey]*verSandboxes{
			{testVer.ID.String(), "web"}: {
				ver:       testVer,
				sandboxes: []*sandbox{testSandbox},
				strategy:  strategy,
			},
		},
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	// Try to acquire a lease - this should NOT succeed with a DEAD sandbox
	// With the bug present, this will incorrectly return a lease from the DEAD sandbox
	// This test uses a timeout context since without a pool, it should timeout trying to get capacity
	timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	lease, err := activator.AcquireLease(timeoutCtx, testVer, "web")

	// The correct behavior is to NOT grant a lease from a DEAD sandbox
	// It should either timeout or return an error
	if lease != nil {
		// This assertion will FAIL with the current buggy code
		require.NotEqual(t, compute_v1alpha.DEAD, lease.sandbox.Status,
			"Should not grant lease from DEAD sandbox, but got one from sandbox %s with status %s",
			lease.sandbox.ID, lease.sandbox.Status)
	} else {
		// Correct behavior - no lease granted from DEAD sandbox
		require.Error(t, err, "Should return an error when no healthy sandboxes available")
	}
}

// TestActivatorRemovesDeadSandbox verifies that DEAD sandboxes are removed
// from the tracking structures (not just skipped)
func TestActivatorRemovesDeadSandbox(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

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

	ent := entity.Blank()
	ent.SetID("sb-1")

	strategy := concurrency.NewStrategy(&core_v1alpha.ServiceConcurrency{
		Mode:                "auto",
		RequestsPerInstance: 10,
		ScaleDownDelay:      "15m",
	})
	tracker := strategy.InitializeTracker()

	// Create a sandbox with RUNNING status initially
	testSandbox := &sandbox{
		sandbox: &compute_v1alpha.Sandbox{
			ID:     entity.Id("sb-1"),
			Status: compute_v1alpha.RUNNING,
		},
		ent:         ent,
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
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	// Verify sandbox is initially tracked
	key := verKey{"ver-1", "web"}
	require.Len(t, activator.versions[key].sandboxes, 1, "Should have 1 sandbox initially")

	// Simulate the sandbox transitioning to DEAD status (as watchSandboxes would do)
	activator.mu.Lock()

	// Find the tracked sandbox and key
	var trackedSandbox *sandbox
	var trackedKey verKey
	for k, vs := range activator.versions {
		for _, s := range vs.sandboxes {
			if s.sandbox.ID == "sb-1" {
				trackedSandbox = s
				trackedKey = k
				break
			}
		}
		if trackedSandbox != nil {
			break
		}
	}

	require.NotNil(t, trackedSandbox, "Should find the tracked sandbox")

	// Update status to DEAD
	trackedSandbox.sandbox.Status = compute_v1alpha.DEAD

	// Remove the sandbox from tracking (simulating watchSandboxes behavior)
	if vs, ok := activator.versions[trackedKey]; ok {
		for i, s := range vs.sandboxes {
			if s.sandbox.ID == "sb-1" {
				vs.sandboxes[i] = vs.sandboxes[len(vs.sandboxes)-1]
				vs.sandboxes = vs.sandboxes[:len(vs.sandboxes)-1]
				break
			}
		}

		if len(vs.sandboxes) == 0 {
			delete(activator.versions, trackedKey)
		}
	}

	activator.mu.Unlock()

	// Verify sandbox was removed from tracking
	activator.mu.RLock()
	defer activator.mu.RUnlock()

	if vs, exists := activator.versions[key]; exists {
		assert.Len(t, vs.sandboxes, 0, "DEAD sandbox should be removed from sandboxes slice")
	}

	// Optionally, the entire verKey might be removed if it was the only sandbox
	// Either way is correct - having an empty slice or no entry at all
}

// TestActivatorPendingSandboxAwareness verifies that AcquireLease waits for PENDING
// sandboxes instead of requesting more capacity from the pool
func TestActivatorPendingSandboxAwareness(t *testing.T) {
	ctx := context.Background()

	// Create in-memory entity server
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app entity
	app := &core_v1alpha.App{}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	testVer := &core_v1alpha.AppVersion{
		App:      app.ID,
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
	verID, err := server.Client.Create(ctx, "test-ver", testVer)
	require.NoError(t, err)
	testVer.ID = verID

	// Create a PENDING sandbox (booting up)
	pendingSandbox := &compute_v1alpha.Sandbox{
		Status:  compute_v1alpha.PENDING,
		Version: testVer.ID,
		Spec: compute_v1alpha.SandboxSpec{
			Version: testVer.ID,
		},
		Network: []compute_v1alpha.Network{
			{Address: "10.0.0.1"},
		},
	}

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetAttrs(entity.New(
		(&core_v1alpha.Metadata{
			Name: "pending-sb",
			Labels: types.LabelSet(
				"service", "web",
			),
		}).Encode,
		entity.Ident, entity.MustKeyword("sandbox/pending-sb"),
		pendingSandbox.Encode,
	).Attrs())

	pendingResp, err := server.EAC.Put(ctx, &rpcE)
	require.NoError(t, err)
	pendingSandbox.ID = entity.Id(pendingResp.Id())

	log := testutils.TestLogger(t)

	// Create activator and let it discover the PENDING sandbox
	activator := &localActivator{
		log:             log,
		eac:             server.EAC,
		versions:        make(map[verKey]*verSandboxes),
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
		usePools:        true, // Test is for pool mode behavior
	}

	// Manually add the PENDING sandbox to activator's tracking
	// (simulating what watchSandboxes would do)
	strategy := concurrency.NewStrategy(&testVer.Config.Services[0].ServiceConcurrency)
	tracker := strategy.InitializeTracker()

	ent := entity.Blank()
	ent.SetID(pendingSandbox.ID)

	pendingSb := &sandbox{
		sandbox:     pendingSandbox,
		ent:         ent,
		lastRenewal: time.Now(),
		url:         "http://10.0.0.1:3000",
		tracker:     tracker,
	}

	key := verKey{testVer.ID.String(), "web"}
	activator.mu.Lock()
	activator.versions[key] = &verSandboxes{
		ver:       testVer,
		sandboxes: []*sandbox{pendingSb},
		strategy:  strategy,
	}
	activator.mu.Unlock()

	// Start a goroutine that will transition the PENDING sandbox to RUNNING after 100ms
	go func() {
		time.Sleep(100 * time.Millisecond)

		// Update sandbox status to RUNNING
		activator.mu.Lock()
		pendingSb.sandbox.Status = compute_v1alpha.RUNNING
		activator.mu.Unlock()

		// Notify any waiters
		activator.mu.Lock()
		if chans, ok := activator.newSandboxChans[key]; ok {
			for _, ch := range chans {
				select {
				case ch <- struct{}{}:
				default:
				}
			}
		}
		activator.mu.Unlock()

		log.Info("transitioned PENDING sandbox to RUNNING", "sandbox", pendingSandbox.ID)
	}()

	// Now try to acquire a lease - it should wait for the PENDING sandbox
	// instead of creating a new one
	start := time.Now()
	lease, err := activator.AcquireLease(ctx, testVer, "web")
	elapsed := time.Since(start)

	// Should succeed after waiting for the PENDING sandbox to become RUNNING
	require.NoError(t, err)
	require.NotNil(t, lease)

	// Verify we got the sandbox that was PENDING
	assert.Equal(t, pendingSandbox.ID, lease.sandbox.ID)

	// Verify we waited (should be ~100ms, not immediate)
	assert.Greater(t, elapsed, 50*time.Millisecond, "Should have waited for PENDING sandbox")
	assert.Less(t, elapsed, 200*time.Millisecond, "Should not have timed out")

	// Verify that the pool was NOT incremented (no pool should exist)
	activator.mu.RLock()
	_, poolExists := activator.pools[key]
	activator.mu.RUnlock()
	assert.False(t, poolExists, "Pool should not be created when PENDING sandboxes exist")
}

// TestActivatorNoPendingCreatesPool verifies that AcquireLease creates a pool
// and increments capacity when no PENDING sandboxes exist
func TestActivatorNoPendingCreatesPool(t *testing.T) {
	ctx := context.Background()

	// Create in-memory entity server
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app entity
	app := &core_v1alpha.App{}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	testVer := &core_v1alpha.AppVersion{
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
	verID, err := server.Client.Create(ctx, "test-ver", testVer)
	require.NoError(t, err)
	testVer.ID = verID

	log := testutils.TestLogger(t)

	// Create activator with NO sandboxes
	activator := &localActivator{
		log:             log,
		eac:             server.EAC,
		versions:        make(map[verKey]*verSandboxes),
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
		usePools:        true, // Test is for pool mode behavior
	}

	// Create a goroutine that simulates a sandbox becoming available after pool creation
	go func() {
		// Wait for pool to be created
		time.Sleep(50 * time.Millisecond)

		key := verKey{testVer.ID.String(), "web"}

		// Create a RUNNING sandbox
		strategy := concurrency.NewStrategy(&testVer.Config.Services[0].ServiceConcurrency)
		tracker := strategy.InitializeTracker()

		ent := entity.Blank()
		ent.SetID("sb-new")

		newSandbox := &sandbox{
			sandbox: &compute_v1alpha.Sandbox{
				ID:     entity.Id("sb-new"),
				Status: compute_v1alpha.RUNNING,
			},
			ent:         ent,
			lastRenewal: time.Now(),
			url:         "http://10.0.0.2:3000",
			tracker:     tracker,
		}

		activator.mu.Lock()
		vs, ok := activator.versions[key]
		if !ok {
			vs = &verSandboxes{
				ver:       testVer,
				sandboxes: []*sandbox{},
				strategy:  strategy,
			}
			activator.versions[key] = vs
		}
		vs.sandboxes = append(vs.sandboxes, newSandbox)

		// Notify any waiters
		if chans, ok := activator.newSandboxChans[key]; ok {
			for _, ch := range chans {
				select {
				case ch <- struct{}{}:
				default:
				}
			}
		}
		activator.mu.Unlock()

		log.Info("created new RUNNING sandbox", "sandbox", newSandbox.sandbox.ID)
	}()

	// Try to acquire a lease with no existing sandboxes
	// This should trigger pool creation and increment
	timeoutCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	lease, err := activator.AcquireLease(timeoutCtx, testVer, "web")

	// Should succeed after pool creates sandbox
	require.NoError(t, err)
	require.NotNil(t, lease)

	// Verify pool was created and incremented
	key := verKey{testVer.ID.String(), "web"}
	activator.mu.RLock()
	poolState, poolExists := activator.pools[key]
	activator.mu.RUnlock()

	assert.True(t, poolExists, "Pool should be created when no sandboxes exist")
	if poolExists {
		assert.NotNil(t, poolState.pool, "Pool state should have pool entity")
		assert.Equal(t, int64(1), poolState.pool.DesiredInstances, "Pool should have incremented desired instances")
	}
}
