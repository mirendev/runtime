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

	poolID := entity.Id("pool-1")
	activator := &localActivator{
		log: log,
		versions: map[verKey]*versionPoolRef{
			{"ver-1", "web"}: {
				ver:      testVer,
				poolID:   poolID,
				service:  "web",
				strategy: strategy,
			},
		},
		poolSandboxes: map[entity.Id]*poolSandboxes{
			poolID: {
				pool:      &compute_v1alpha.SandboxPool{ID: poolID},
				sandboxes: []*sandbox{testSandbox},
				service:   "web",
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
		log:           log,
		versions:      make(map[verKey]*versionPoolRef),
		poolSandboxes: make(map[entity.Id]*poolSandboxes),
	}

	// Run multiple goroutines accessing the versions map
	done := make(chan bool, 3)

	poolID := entity.Id("pool-1")

	// Goroutine 1: Add versions
	go func() {
		for i := 0; i < 100; i++ {
			activator.mu.Lock()
			activator.versions[verKey{ver: "ver-1", service: "web"}] = &versionPoolRef{
				poolID:  poolID,
				service: "web",
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
		Status: compute_v1alpha.RUNNING,
		Spec: compute_v1alpha.SandboxSpec{
			Version: appVer.ID,
			Container: []compute_v1alpha.SandboxSpecContainer{
				{
					Port: []compute_v1alpha.SandboxSpecContainerPort{
						{
							Port: 8080,
							Name: "http",
							Type: "http",
						},
					},
				},
			},
		},
		Network: []compute_v1alpha.Network{
			{Address: "10.0.0.100"},
		},
	}

	// Create a pool entity first
	pool := compute_v1alpha.SandboxPool{
		Service:              "web",
		DesiredInstances:     1,
		ReferencedByVersions: []entity.Id{appVer.ID},
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Version: appVer.ID,
		},
	}

	poolID, err := server.Client.Create(ctx, "test-pool", &pool)
	require.NoError(t, err)
	pool.ID = poolID

	var rpcE entityserver_v1alpha.Entity

	// Now create sandbox with pool label
	rpcE.SetAttrs(entity.New(
		(&core_v1alpha.Metadata{
			Name:   "test-sandbox",
			Labels: types.LabelSet("service", "web", "pool", poolID.String()),
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
		log:           log,
		eac:           server.EAC,
		versions:      make(map[verKey]*versionPoolRef),
		poolSandboxes: make(map[entity.Id]*poolSandboxes),
		pools:         make(map[verKey]*poolState),
	}

	// Recover pools first, then sandboxes
	err = activator.recoverPools(ctx)
	require.NoError(t, err)

	err = activator.recoverSandboxes(ctx)
	require.NoError(t, err)

	// Verify sandbox was recovered
	ps, ok := activator.poolSandboxes[poolID]
	require.True(t, ok, "pool should be in map")
	require.Len(t, ps.sandboxes, 1, "should have recovered 1 running sandbox")

	// Verify sandbox details
	recoveredSb := ps.sandboxes[0]
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

	// Create a pool for the sandboxes
	pool := compute_v1alpha.SandboxPool{
		Service:              "api",
		DesiredInstances:     3,
		ReferencedByVersions: []entity.Id{appVer.ID},
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Version: appVer.ID,
		},
	}
	poolID, err := client.Create(ctx, "integration-pool", &pool)
	require.NoError(t, err)
	pool.ID = poolID

	// Create multiple running sandboxes
	for i := 0; i < 3; i++ {
		sb := compute_v1alpha.Sandbox{
			Status: compute_v1alpha.RUNNING,
			Spec: compute_v1alpha.SandboxSpec{
				Version: appVer.ID,
			},
			Network: []compute_v1alpha.Network{
				{Address: "10.0.0.100/32"},
			},
		}

		var rpcE entityserver_v1alpha.Entity
		rpcE.SetAttrs(entity.New(
			(&core_v1alpha.Metadata{
				Name:   "sandbox-" + string(rune('a'+i)),
				Labels: types.LabelSet("service", "api", "pool", poolID.String()),
			}).Encode,
			entity.Ident, entity.MustKeyword("sandbox/sb-"+string(rune('a'+i))),
			sb.Encode,
		).Attrs())

		_, err := es.EAC.Put(ctx, &rpcE)
		require.NoError(t, err)
	}

	// Create activator - should recover all sandboxes
	log := testutils.TestLogger(t)
	activator := NewLocalActivator(ctx, log, es.EAC).(*localActivator)

	// Give a moment for recovery to complete
	time.Sleep(100 * time.Millisecond)

	// Verify recovery
	key := verKey{appVer.ID.String(), "api"}
	versionRef, ok := activator.versions[key]
	require.True(t, ok, "version should be tracked")

	// Get the pool sandboxes
	ps, ok := activator.poolSandboxes[versionRef.poolID]
	require.True(t, ok, "pool should be tracked")
	assert.Len(t, ps.sandboxes, 3, "should recover all 3 sandboxes")

	// Verify strategy configuration
	assert.Equal(t, 0, ps.strategy.DesiredInstances()) // Auto mode scales to zero
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
	poolID := entity.Id("pool-1")
	activator := &localActivator{
		log: log,
		eac: server.EAC,
		versions: map[verKey]*versionPoolRef{
			{testVer.ID.String(), "web"}: {
				ver:      testVer,
				poolID:   poolID,
				service:  "web",
				strategy: strategy,
			},
		},
		poolSandboxes: map[entity.Id]*poolSandboxes{
			poolID: {
				pool:      &compute_v1alpha.SandboxPool{ID: poolID},
				sandboxes: []*sandbox{testSandbox},
				service:   "web",
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
func TestActivatorKeepsDeadSandbox(t *testing.T) {
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

	poolID := entity.Id("pool-1")
	activator := &localActivator{
		log: log,
		versions: map[verKey]*versionPoolRef{
			{"ver-1", "web"}: {
				ver:      testVer,
				poolID:   poolID,
				service:  "web",
				strategy: strategy,
			},
		},
		poolSandboxes: map[entity.Id]*poolSandboxes{
			poolID: {
				pool:      &compute_v1alpha.SandboxPool{ID: poolID},
				sandboxes: []*sandbox{testSandbox},
				service:   "web",
				strategy:  strategy,
			},
		},
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	// Verify sandbox is initially tracked
	require.Len(t, activator.poolSandboxes[poolID].sandboxes, 1, "Should have 1 sandbox initially")

	// Simulate the sandbox transitioning to DEAD status (as watchSandboxes would do)
	activator.mu.Lock()

	// Find the tracked sandbox and pool
	var trackedSandbox *sandbox
	for _, ps := range activator.poolSandboxes {
		for _, s := range ps.sandboxes {
			if s.sandbox.ID == "sb-1" {
				trackedSandbox = s
				break
			}
		}
		if trackedSandbox != nil {
			break
		}
	}

	require.NotNil(t, trackedSandbox, "Should find the tracked sandbox")

	// Update status to DEAD (but don't remove it - this is the new behavior)
	trackedSandbox.sandbox.Status = compute_v1alpha.DEAD

	activator.mu.Unlock()

	// Verify sandbox is still tracked but marked as DEAD
	// This allows fail-fast logic to detect that all sandboxes have failed
	activator.mu.RLock()
	defer activator.mu.RUnlock()

	ps, exists := activator.poolSandboxes[poolID]
	require.True(t, exists, "Pool should still exist")
	require.Len(t, ps.sandboxes, 1, "DEAD sandbox should remain in tracking")
	assert.Equal(t, compute_v1alpha.DEAD, ps.sandboxes[0].sandbox.Status, "Sandbox should be marked as DEAD")
}

// TestActivatorFailsFastWhenAllSandboxesDead verifies that waitForSandbox fails fast
// when all sandboxes transition to DEAD status, instead of waiting for the full timeout
func TestActivatorFailsFastWhenAllSandboxesDead(t *testing.T) {
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

	// Create a DEAD sandbox (simulating a sandbox that crashed during boot)
	deadSandbox := &sandbox{
		sandbox: &compute_v1alpha.Sandbox{
			ID:     entity.Id("sb-1"),
			Status: compute_v1alpha.DEAD,
		},
		ent:         ent,
		lastRenewal: time.Now(),
		url:         "http://localhost:3000",
		tracker:     tracker,
	}

	poolID := entity.Id("pool-1")
	activator := &localActivator{
		log: log,
		versions: map[verKey]*versionPoolRef{
			{"ver-1", "web"}: {
				ver:      testVer,
				poolID:   poolID,
				service:  "web",
				strategy: strategy,
			},
		},
		poolSandboxes: map[entity.Id]*poolSandboxes{
			poolID: {
				pool:      &compute_v1alpha.SandboxPool{ID: poolID},
				sandboxes: []*sandbox{deadSandbox},
				service:   "web",
				strategy:  strategy,
			},
		},
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	// Create a context with timeout - the fail-fast should return before this timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Attempt to wait for a sandbox - should fail fast immediately
	start := time.Now()
	_, err := activator.waitForSandbox(ctx, testVer, "web", false)
	elapsed := time.Since(start)

	// Should error with ErrSandboxDiedEarly
	require.Error(t, err, "Should fail when all sandboxes are DEAD")
	assert.ErrorIs(t, err, ErrSandboxDiedEarly, "Should return ErrSandboxDiedEarly")

	// Should fail fast (< 1 second) not wait for the full 5 second timeout
	assert.Less(t, elapsed, 1*time.Second, "Should fail fast, not wait for timeout")
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
		Status: compute_v1alpha.PENDING,
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
		versions:        make(map[verKey]*versionPoolRef),
		poolSandboxes:   make(map[entity.Id]*poolSandboxes),
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
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

	poolID := entity.Id("pool-1")
	key := verKey{testVer.ID.String(), "web"}
	activator.mu.Lock()
	activator.versions[key] = &versionPoolRef{
		ver:      testVer,
		poolID:   poolID,
		service:  "web",
		strategy: strategy,
	}
	activator.poolSandboxes[poolID] = &poolSandboxes{
		pool:      &compute_v1alpha.SandboxPool{ID: poolID},
		sandboxes: []*sandbox{pendingSb},
		service:   "web",
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

	// Pre-create a pool in the entity store (simulating DeploymentLauncher behavior)
	launcherPool := &compute_v1alpha.SandboxPool{
		Service: "web",
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Version: testVer.ID,
			Container: []compute_v1alpha.SandboxSpecContainer{
				{
					Name:  "app",
					Image: "test:latest",
					Port: []compute_v1alpha.SandboxSpecContainerPort{
						{Port: 3000, Name: "http", Type: "http"},
					},
				},
			},
		},
		ReferencedByVersions: []entity.Id{testVer.ID},
		DesiredInstances:     0, // Launcher starts at 0 for auto mode
	}
	poolID, err := server.Client.Create(ctx, "launcher-pool", launcherPool)
	require.NoError(t, err)
	launcherPool.ID = poolID

	// Create activator with NO sandboxes
	activator := &localActivator{
		log:             log,
		eac:             server.EAC,
		versions:        make(map[verKey]*versionPoolRef),
		poolSandboxes:   make(map[entity.Id]*poolSandboxes),
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
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
		versionRef, ok := activator.versions[key]
		if !ok {
			poolID := entity.Id("pool-1")
			versionRef = &versionPoolRef{
				ver:      testVer,
				poolID:   poolID,
				service:  "web",
				strategy: strategy,
			}
			activator.versions[key] = versionRef
			activator.poolSandboxes[poolID] = &poolSandboxes{
				pool:      &compute_v1alpha.SandboxPool{ID: poolID},
				sandboxes: []*sandbox{},
				service:   "web",
				strategy:  strategy,
			}
		}
		ps := activator.poolSandboxes[versionRef.poolID]
		ps.sandboxes = append(ps.sandboxes, newSandbox)

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

	// Should succeed after finding launcher-created pool and waiting for sandbox
	require.NoError(t, err)
	require.NotNil(t, lease)

	// Verify activator found the launcher-created pool and incremented it
	key := verKey{testVer.ID.String(), "web"}
	activator.mu.RLock()
	poolState, poolExists := activator.pools[key]
	activator.mu.RUnlock()

	assert.True(t, poolExists, "Pool should be found and cached")
	if poolExists {
		assert.NotNil(t, poolState.pool, "Pool state should have pool entity")
		assert.Equal(t, poolID, poolState.pool.ID, "Should have found the launcher-created pool")
		assert.Equal(t, int64(1), poolState.pool.DesiredInstances, "Pool should have incremented desired instances")
	}
}

// TestActivatorDeletedPoolDetection verifies that the activator correctly detects
// when a cached pool has been deleted from the entity store and clears the stale cache.
// In the new architecture, deleted pools are expected to be recreated by the
// DeploymentLauncher, not the activator.
func TestActivatorDeletedPoolDetection(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app entity
	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	// Create app version
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

	// Pre-create a pool in the entity store (simulating DeploymentLauncher)
	pool := &compute_v1alpha.SandboxPool{
		Service: "web",
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Version: testVer.ID,
			Container: []compute_v1alpha.SandboxSpecContainer{
				{
					Name:  "app",
					Image: "test:latest",
					Port: []compute_v1alpha.SandboxSpecContainerPort{
						{Port: 3000, Name: "http", Type: "http"},
					},
				},
			},
		},
		ReferencedByVersions: []entity.Id{testVer.ID},
		DesiredInstances:     0,
	}
	poolID, err := server.Client.Create(ctx, "launcher-pool", pool)
	require.NoError(t, err)
	pool.ID = poolID

	activator := NewLocalActivator(ctx, log, server.EAC).(*localActivator)

	// First AcquireLease - cache the pool and then increment it
	// Use a very short timeout to fail fast (no sandbox will become available)
	timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	_, err = activator.AcquireLease(timeoutCtx, testVer, "web")
	require.Error(t, err, "Should timeout since no sandbox controller is running")
	require.Contains(t, err.Error(), "timeout", "Should be a timeout error")

	// Verify the pool was found and cached
	key := verKey{testVer.ID.String(), "web"}
	activator.mu.RLock()
	_, exists := activator.pools[key]
	activator.mu.RUnlock()
	require.True(t, exists, "Pool should be cached after first acquire")

	originalPoolID := poolID

	// Now simulate cleanup: delete the pool from the entity store
	// (this is what happens during deployment handover after scaling to zero)
	_, err = server.EAC.Delete(ctx, originalPoolID.String())
	require.NoError(t, err)

	// Verify pool is gone from entity store
	_, err = server.EAC.Get(ctx, originalPoolID.String())
	require.Error(t, err, "Pool should be deleted from entity store")

	// Second AcquireLease attempt - should detect deleted pool and clear cache
	// Then fail with "pool not found" error (launcher should recreate)
	timeoutCtx2, cancel2 := context.WithTimeout(ctx, 1*time.Second)
	defer cancel2()

	_, err = activator.AcquireLease(timeoutCtx2, testVer, "web")
	require.Error(t, err, "Should error since pool was deleted")
	require.Contains(t, err.Error(), "pool not found", "Should be pool not found error")
	require.Contains(t, err.Error(), "DeploymentLauncher", "Error should mention DeploymentLauncher")

	// Verify the cache was cleared (activator detected the deletion)
	activator.mu.RLock()
	_, stillCached := activator.pools[key]
	activator.mu.RUnlock()
	require.False(t, stillCached, "Pool should have been removed from cache after detecting deletion")

	// Verify no new pool was created (activator doesn't create pools anymore)
	resp, err := server.EAC.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandboxPool))
	require.NoError(t, err)
	pools := resp.Values()
	require.Len(t, pools, 0, "Activator should not have created a new pool")
}

// TestActivatorFindsLauncherCreatedPool verifies that the activator can find and use
// pools created by the DeploymentLauncher controller instead of creating its own.
func TestActivatorFindsLauncherCreatedPool(t *testing.T) {
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

	// Pre-create a pool in the entity store (simulating DeploymentLauncher behavior)
	launcherPool := &compute_v1alpha.SandboxPool{
		Service: "web",
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Version: testVer.ID,
			Container: []compute_v1alpha.SandboxSpecContainer{
				{
					Name:  "app",
					Image: "test:latest",
					Port: []compute_v1alpha.SandboxSpecContainerPort{
						{Port: 3000, Name: "http", Type: "http"},
					},
				},
			},
		},
		ReferencedByVersions: []entity.Id{testVer.ID},
		DesiredInstances:     0, // Launcher starts at 0 for auto mode
	}
	poolID, err := server.Client.Create(ctx, "launcher-pool", launcherPool)
	require.NoError(t, err)
	launcherPool.ID = poolID

	// Create activator with NO sandboxes and empty cache
	activator := &localActivator{
		log:             log,
		eac:             server.EAC,
		versions:        make(map[verKey]*versionPoolRef),
		poolSandboxes:   make(map[entity.Id]*poolSandboxes),
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	// Simulate a sandbox becoming available (created by SandboxPoolManager)
	go func() {
		time.Sleep(50 * time.Millisecond)

		key := verKey{testVer.ID.String(), "web"}

		// Create a RUNNING sandbox
		strategy := concurrency.NewStrategy(&testVer.Config.Services[0].ServiceConcurrency)
		tracker := strategy.InitializeTracker()

		ent := entity.Blank()
		ent.SetID("sb-launcher")

		newSandbox := &sandbox{
			sandbox: &compute_v1alpha.Sandbox{
				ID:     entity.Id("sb-launcher"),
				Status: compute_v1alpha.RUNNING,
			},
			ent:         ent,
			lastRenewal: time.Now(),
			url:         "http://10.0.0.1:3000",
			tracker:     tracker,
		}

		activator.mu.Lock()
		versionRef, ok := activator.versions[key]
		if !ok {
			poolID := entity.Id("pool-1")
			versionRef = &versionPoolRef{
				ver:      testVer,
				poolID:   poolID,
				service:  "web",
				strategy: strategy,
			}
			activator.versions[key] = versionRef
			activator.poolSandboxes[poolID] = &poolSandboxes{
				pool:      &compute_v1alpha.SandboxPool{ID: poolID},
				sandboxes: []*sandbox{},
				service:   "web",
				strategy:  strategy,
			}
		}
		ps := activator.poolSandboxes[versionRef.poolID]
		ps.sandboxes = append(ps.sandboxes, newSandbox)

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

		log.Info("sandbox ready from launcher pool", "sandbox", newSandbox.sandbox.ID)
	}()

	// Acquire a lease - should find launcher-created pool via retry logic
	timeoutCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	lease, err := activator.AcquireLease(timeoutCtx, testVer, "web")
	require.NoError(t, err)
	require.NotNil(t, lease)

	// Verify the activator found and used the launcher-created pool
	key := verKey{testVer.ID.String(), "web"}
	activator.mu.RLock()
	poolState, poolExists := activator.pools[key]
	activator.mu.RUnlock()

	assert.True(t, poolExists, "Pool should be found and cached")
	if poolExists {
		assert.NotNil(t, poolState.pool, "Pool state should have pool entity")
		assert.Equal(t, poolID, poolState.pool.ID, "Should have found the launcher-created pool, not created a new one")
		assert.Equal(t, int64(1), poolState.pool.DesiredInstances, "Pool should have incremented desired instances to 1")
	}

	// Verify only one pool exists in the store (no duplicate creation)
	resp, err := server.EAC.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandboxPool))
	require.NoError(t, err)
	pools := resp.Values()
	assert.Len(t, pools, 1, "Should have exactly one pool (the launcher-created one)")
}

// TestFindPoolInStore verifies that findPoolInStore correctly queries the entity store
// for pools created by the DeploymentLauncher controller.
func TestFindPoolInStore(t *testing.T) {
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
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", testVer)
	require.NoError(t, err)
	testVer.ID = verID

	log := testutils.TestLogger(t)

	// Create activator
	activator := &localActivator{
		log: log,
		eac: server.EAC,
	}

	t.Run("finds existing pool", func(t *testing.T) {
		// Create a pool in the entity store (simulating DeploymentLauncher)
		pool := &compute_v1alpha.SandboxPool{
			Service: "web",
			SandboxSpec: compute_v1alpha.SandboxSpec{
				Version: testVer.ID,
				Container: []compute_v1alpha.SandboxSpecContainer{
					{
						Name:  "app",
						Image: "test:latest",
						Port: []compute_v1alpha.SandboxSpecContainerPort{
							{Port: 3000, Name: "http"},
						},
					},
				},
			},
			ReferencedByVersions: []entity.Id{testVer.ID},
			DesiredInstances:     1,
		}

		poolID, err := server.Client.Create(ctx, "test-pool", pool)
		require.NoError(t, err)
		pool.ID = poolID

		// Try to find the pool
		foundPool, err := activator.findPoolInStore(ctx, testVer.ID, "web")
		require.NoError(t, err)
		require.NotNil(t, foundPool, "Should find the pool")
		assert.Equal(t, poolID, foundPool.pool.ID)
		assert.Equal(t, "web", foundPool.pool.Service)
		assert.Equal(t, testVer.ID, foundPool.pool.SandboxSpec.Version)
	})

	t.Run("returns nil for wrong service", func(t *testing.T) {
		// Try to find pool with wrong service name
		foundPool, err := activator.findPoolInStore(ctx, testVer.ID, "worker")
		require.NoError(t, err)
		assert.Nil(t, foundPool, "Should not find pool with wrong service name")
	})

	t.Run("returns nil for wrong version", func(t *testing.T) {
		// Try to find pool with wrong version
		wrongVersionID := entity.Id("ver-wrong")
		foundPool, err := activator.findPoolInStore(ctx, wrongVersionID, "web")
		require.NoError(t, err)
		assert.Nil(t, foundPool, "Should not find pool with wrong version")
	})

	t.Run("returns nil when no pools exist", func(t *testing.T) {
		// Create a fresh entity server with no pools
		freshServer, cleanup := testutils.NewInMemEntityServer(t)
		defer cleanup()

		freshActivator := &localActivator{
			log: log,
			eac: freshServer.EAC,
		}

		foundPool, err := freshActivator.findPoolInStore(ctx, testVer.ID, "web")
		require.NoError(t, err)
		assert.Nil(t, foundPool, "Should return nil when no pools exist")
	})
}

// TestFindPoolByReferencedByVersions tests that pools can be found when a version
// is in the referenced_by_versions list (pool reuse across deployments).
func TestFindPoolByReferencedByVersions(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create entity server
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create two app versions
	oldVersion := entity.Id("app_version/db-app-v1")
	newVersion := entity.Id("app_version/db-app-v2")

	// Create a pool that was originally created for oldVersion
	// but now references both oldVersion and newVersion (due to pool reuse)
	pool := &compute_v1alpha.SandboxPool{
		Service: "web",
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Version: oldVersion, // Original version
		},
		ReferencedByVersions: []entity.Id{oldVersion, newVersion}, // Now references both
		DesiredInstances:     1,
		CurrentInstances:     1,
		ReadyInstances:       1,
	}

	// Store the pool
	poolID, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)
	pool.ID = poolID

	// Create activator
	activator := &localActivator{
		log:             log,
		eac:             server.EAC,
		versions:        make(map[verKey]*versionPoolRef),
		poolSandboxes:   make(map[entity.Id]*poolSandboxes),
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	// Test: Should find pool by oldVersion (in referenced_by_versions)
	t.Run("finds pool by old version", func(t *testing.T) {
		foundPoolWithRev, err := activator.findPoolInStore(ctx, oldVersion, "web")
		require.NoError(t, err)
		require.NotNil(t, foundPoolWithRev)
		assert.Equal(t, pool.ID, foundPoolWithRev.pool.ID)
		assert.Equal(t, "web", foundPoolWithRev.pool.Service)
		assert.Greater(t, foundPoolWithRev.revision, int64(0), "Should have non-zero revision")
	})

	// Test: Should find pool by newVersion (in referenced_by_versions)
	t.Run("finds pool by new version via referenced_by_versions", func(t *testing.T) {
		foundPoolWithRev, err := activator.findPoolInStore(ctx, newVersion, "web")
		require.NoError(t, err)
		require.NotNil(t, foundPoolWithRev, "Should find pool even though SandboxSpec.Version != newVersion")
		assert.Equal(t, pool.ID, foundPoolWithRev.pool.ID)
		assert.Equal(t, "web", foundPoolWithRev.pool.Service)
	})

	// Test: Should NOT find pool by version not in referenced_by_versions
	t.Run("does not find pool by unrelated version", func(t *testing.T) {
		unrelatedVersion := entity.Id("app_version/db-app-v3")
		foundPoolWithRev, err := activator.findPoolInStore(ctx, unrelatedVersion, "web")
		require.NoError(t, err)
		assert.Nil(t, foundPoolWithRev, "Should not find pool for version not in referenced_by_versions")
	})
}

// TestPoolIncrementWithOCC tests that the activator uses optimistic concurrency control
// when incrementing pool desired_instances to prevent stale cache writes.
func TestPoolIncrementWithOCC(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create entity server
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app and version entities
	app := &core_v1alpha.App{}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)

	testVer := &core_v1alpha.AppVersion{
		App:      appID,
		Version:  "v1",
		ImageUrl: "test:latest",
		Config: core_v1alpha.Config{
			Services: []core_v1alpha.Services{
				{Name: "web"},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", testVer)
	require.NoError(t, err)
	testVer.ID = verID

	// Create initial pool
	pool := &compute_v1alpha.SandboxPool{
		Service:              "web",
		SandboxSpec:          compute_v1alpha.SandboxSpec{Version: testVer.ID},
		ReferencedByVersions: []entity.Id{testVer.ID},
		DesiredInstances:     0,
		CurrentInstances:     0,
		ReadyInstances:       0,
	}

	poolID, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)
	pool.ID = poolID

	// Get the revision from the created pool
	getRes, err := server.EAC.Get(ctx, poolID.String())
	require.NoError(t, err)
	initialRevision := getRes.Entity().Revision()

	// Create activator with cached pool state
	activator := &localActivator{
		log:             log,
		eac:             server.EAC,
		versions:        make(map[verKey]*versionPoolRef),
		poolSandboxes:   make(map[entity.Id]*poolSandboxes),
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	// Cache the pool
	key := verKey{testVer.ID.String(), "web"}
	activator.pools[key] = &poolState{
		pool:       pool,
		revision:   initialRevision,
		inProgress: false,
	}

	t.Run("succeeds when no concurrent modification", func(t *testing.T) {
		// Request pool capacity - should succeed and increment from 0 to 1
		resultPool, err := activator.requestPoolCapacity(ctx, testVer, "web")
		require.NoError(t, err)
		require.NotNil(t, resultPool)

		// Verify pool was incremented
		assert.Equal(t, int64(1), resultPool.DesiredInstances)

		// Verify cache was updated with new revision
		cachedState := activator.pools[key]
		assert.Equal(t, int64(1), cachedState.pool.DesiredInstances)
		assert.Greater(t, cachedState.revision, initialRevision, "Revision should have been updated")
	})

	t.Run("retries on revision conflict", func(t *testing.T) {
		// Get current state from store
		getRes, err := server.EAC.Get(ctx, pool.ID.String())
		require.NoError(t, err)
		var currentPool compute_v1alpha.SandboxPool
		currentPool.Decode(getRes.Entity().Entity())
		currentRevision := getRes.Entity().Revision()

		// Simulate stale cache: Set cache to an old revision
		activator.pools[key] = &poolState{
			pool:       &currentPool,
			revision:   initialRevision, // Stale revision!
			inProgress: false,
		}

		// Meanwhile, simulate another process modifying the pool
		// (e.g., pool manager scales it up)
		currentPool.DesiredInstances = 5
		patchAttrs := []entity.Attr{
			{ID: entity.DBId, Value: entity.AnyValue(pool.ID)},
			{ID: compute_v1alpha.SandboxPoolDesiredInstancesId, Value: entity.AnyValue(int64(5))},
		}
		_, err = server.EAC.Patch(ctx, patchAttrs, currentRevision)
		require.NoError(t, err)

		// Now request pool capacity with stale cache
		// Should detect conflict and retry with fresh state
		resultPool, err := activator.requestPoolCapacity(ctx, testVer, "web")
		require.NoError(t, err)
		require.NotNil(t, resultPool)

		// Should have incremented from the FRESH value (5) not the stale cache (1)
		assert.Equal(t, int64(6), resultPool.DesiredInstances, "Should increment from fresh value after conflict")

		// Verify cache was updated
		cachedState := activator.pools[key]
		assert.Equal(t, int64(6), cachedState.pool.DesiredInstances)
	})
}

// TestConcurrentPoolIncrement tests that concurrent calls to requestPoolCapacity
// handle optimistic concurrency control correctly.
//
// IMPORTANT: This test requires etcd to properly enforce OCC. Run with:
//
//	./hack/dev-exec go test -v -run TestConcurrentPoolIncrement ./components/activator
//
// Key behaviors tested:
// 1. Each goroutine calculates its target DesiredInstances ONCE (before retry loop)
// 2. After OCC conflicts, goroutines check if target is already reached (early return)
// 3. The etcd store properly rejects stale revisions, triggering conflict retry logic
//
// Expected behavior with proper OCC:
// - All 5 goroutines start with DesiredInstances=1, calculate target=2
// - One succeeds in patching to 2, others get revision conflicts
// - Conflicting goroutines retry, refetch state, see DesiredInstances=2 >= target=2
// - Early return prevents redundant increments
// - Final result: DesiredInstances=2 (exactly one increment)
//
// What the bug looked like:
// Without the fix, goroutines recalculated target on each retry:
//   - Goroutine sees conflict, refetches DesiredInstances=2
//   - BUG: Recalculates target = 2+1 = 3 (should stay at original target=2)
//   - Patches to 3, causing redundant increments
//   - Result: DesiredInstances=3, 4, or even 5
func TestConcurrentPoolIncrement(t *testing.T) {
	ctx := context.Background()

	// Create etcd-backed entity server for proper OCC testing
	// Run with: ./hack/dev-exec go test -v -run TestConcurrentPoolIncrement ./components/activator
	server, cleanup := testutils.NewEtcdEntityServer(t)
	defer cleanup()

	// Create test app
	app := &core_v1alpha.App{}
	appID, err := server.Client.Create(ctx, "test-app-concurrent", app)
	require.NoError(t, err)
	app.ID = appID

	// Create test version
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
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver-concurrent", testVer)
	require.NoError(t, err)
	testVer.ID = verID

	// Create a sandbox pool with DesiredInstances = 1
	pool := &compute_v1alpha.SandboxPool{
		Service: "web",
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Version: testVer.ID,
		},
		ReferencedByVersions: []entity.Id{
			testVer.ID,
		},
		DesiredInstances: 1,
		CurrentInstances: 0,
	}

	poolEnt := entity.New(
		(&core_v1alpha.Metadata{
			Name:   "test-pool-concurrent",
			Labels: types.LabelSet("service", "web"),
		}).Encode,
		entity.Ident, entity.MustKeyword("sandboxpool/concurrent-pool"),
		pool.Encode,
	)

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetAttrs(poolEnt.Attrs())
	poolRes, err := server.EAC.Put(ctx, &rpcE)
	require.NoError(t, err)
	pool.ID = entity.Id(poolRes.Id())
	poolRevision := poolRes.Revision()

	// Create activator and pre-populate cache
	log := testutils.TestDebugLogger(t)
	activator := &localActivator{
		log:           log,
		eac:           server.EAC,
		versions:      make(map[verKey]*versionPoolRef),
		poolSandboxes: make(map[entity.Id]*poolSandboxes),
		pools:         make(map[verKey]*poolState),
	}

	key := verKey{testVer.ID.String(), "web"}
	activator.pools[key] = &poolState{
		pool:       pool,
		revision:   poolRevision,
		inProgress: false,
	}

	// Launch 5 concurrent goroutines that all try to increment the pool
	// Use a barrier to ensure all goroutines start at approximately the same time
	const numGoroutines = 5
	results := make(chan *compute_v1alpha.SandboxPool, numGoroutines)
	errors := make(chan error, numGoroutines)
	barrier := make(chan struct{})

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			// Wait for all goroutines to be ready
			<-barrier

			result, err := activator.requestPoolCapacity(ctx, testVer, "web")
			if err != nil {
				errors <- err
				return
			}
			results <- result
		}(i)
	}

	// Release all goroutines simultaneously
	close(barrier)

	// Collect all results
	var returnedPools []*compute_v1alpha.SandboxPool
	for i := 0; i < numGoroutines; i++ {
		select {
		case pool := <-results:
			returnedPools = append(returnedPools, pool)
		case err := <-errors:
			t.Fatalf("goroutine returned error: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("test timed out waiting for goroutines")
		}
	}

	// All goroutines should have succeeded
	require.Len(t, returnedPools, numGoroutines, "all goroutines should return a pool")

	// Log what each goroutine saw
	for i, pool := range returnedPools {
		t.Logf("Goroutine %d saw DesiredInstances: %d", i, pool.DesiredInstances)
	}

	// Fetch the final pool state from the entity store
	finalPoolEnt, err := server.EAC.Get(ctx, pool.ID.String())
	require.NoError(t, err)

	var finalPool compute_v1alpha.SandboxPool
	finalPool.Decode(finalPoolEnt.Entity().Entity())

	t.Logf("Final DesiredInstances: %d (started at 1)", finalPool.DesiredInstances)

	// With proper OCC enforcement from etcd, we should see exactly 2:
	// - All 5 goroutines start with DesiredInstances=1, calculate target=2
	// - One succeeds, others get conflicts and early-return after seeing target reached
	// - Result: exactly one increment from 1 to 2
	t.Logf("Final DesiredInstances after %d concurrent increments: %d", numGoroutines, finalPool.DesiredInstances)
	assert.Equal(t, int64(2), finalPool.DesiredInstances,
		"With OCC enforcement, should get exactly one increment despite %d concurrent calls", numGoroutines)
}

// TestActivatorDeletedPoolAtMaxSize verifies that when the activator has a cached pool
// at max size (DesiredInstances >= 20) and that pool has been deleted, it correctly
// detects the stale reference and clears the cache instead of returning a max size error.
// This is a regression test for a bug where the max size check short-circuited before
// attempting any operation that would detect the deleted pool.
func TestActivatorDeletedPoolAtMaxSize(t *testing.T) {
	ctx := context.Background()
	log := testutils.TestLogger(t)

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app entity
	app := &core_v1alpha.App{
		Project: entity.Id("project-1"),
	}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	// Create app version
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

	// Pre-create a pool at MAX size (simulating a pool that scaled up to max)
	pool := &compute_v1alpha.SandboxPool{
		Service: "web",
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Version: testVer.ID,
			Container: []compute_v1alpha.SandboxSpecContainer{
				{
					Name:  "app",
					Image: "test:latest",
					Port: []compute_v1alpha.SandboxSpecContainerPort{
						{Port: 3000, Name: "http", Type: "http"},
					},
				},
			},
		},
		ReferencedByVersions: []entity.Id{testVer.ID},
		DesiredInstances:     MaxPoolSize, // Pool is at max size!
	}
	poolID, err := server.Client.Create(ctx, "maxed-pool", pool)
	require.NoError(t, err)
	pool.ID = poolID

	activator := NewLocalActivator(ctx, log, server.EAC).(*localActivator)

	// Manually prime the cache with the maxed-out pool
	// (simulating what happens after recovery or previous use)
	key := verKey{testVer.ID.String(), "web"}
	poolEnt, err := server.EAC.Get(ctx, poolID.String())
	require.NoError(t, err)

	activator.mu.Lock()
	activator.pools[key] = &poolState{
		pool:       pool,
		revision:   poolEnt.Entity().Revision(),
		inProgress: false,
	}
	activator.mu.Unlock()

	// Delete the pool from entity store (simulating cleanup after scale-to-zero)
	_, err = server.EAC.Delete(ctx, poolID.String())
	require.NoError(t, err)

	// Verify cache still has the stale pool at max size
	activator.mu.RLock()
	cachedState, exists := activator.pools[key]
	activator.mu.RUnlock()
	require.True(t, exists, "Pool should still be in cache")
	require.Equal(t, int64(MaxPoolSize), cachedState.pool.DesiredInstances, "Cached pool should be at max size")

	// Now try to acquire a lease - this SHOULD detect the deleted pool
	// rather than immediately returning "pool has reached maximum size"
	timeoutCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	_, err = activator.AcquireLease(timeoutCtx, testVer, "web")
	require.Error(t, err, "Should error since pool was deleted")

	// The key assertion: error should NOT be about max pool size
	// It should be about pool not found (since we detected deletion and cleared cache)
	require.NotContains(t, err.Error(), "maximum size",
		"Should NOT return max size error for deleted pool - should detect deletion instead")
	require.Contains(t, err.Error(), "pool not found",
		"Should return pool not found error after detecting deleted pool")

	// Verify the stale cache was cleared
	activator.mu.RLock()
	_, stillCached := activator.pools[key]
	activator.mu.RUnlock()
	require.False(t, stillCached, "Stale pool should have been removed from cache")
}
