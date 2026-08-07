package activator

import (
	"context"
	"fmt"
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
		for range 100 {
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
	for i := range 3 {
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
	assert.Equal(t, 0, ps.strategy.MinInstances()) // Auto mode scales to zero
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
			Spec: compute_v1alpha.SandboxSpec{
				Version: testVer.ID,
			},
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

	for i := range numGoroutines {
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
	for range numGoroutines {
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

// TestWatchPoolsCleansUpCacheOnDeletion verifies that the watchPools background goroutine
// automatically cleans up all activator caches when a pool entity is deleted.
// This prevents stale pool references from causing "pool has reached maximum size" errors.
// Run with: ./hack/dev-exec go test -v -run TestWatchPoolsCleansUpCacheOnDeletion ./components/activator
func TestWatchPoolsCleansUpCacheOnDeletion(t *testing.T) {
	ctx := t.Context()

	// Use in-memory server with realistic WatchIndex implementation
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app entity (Project is optional)
	app := &core_v1alpha.App{}
	appID, err := server.Client.Create(ctx, "test-app-watch", app)
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

	// Pre-create a pool in the entity store
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
		DesiredInstances:     5,
	}
	poolID, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)
	pool.ID = poolID

	// Create activator - this will start watchPools in background
	activator := NewLocalActivator(ctx, testutils.TestLogger(t), server.EAC).(*localActivator)

	// Verify pool was recovered and cached
	key := verKey{testVer.ID.String(), "web"}
	activator.mu.RLock()
	_, poolsExists := activator.pools[key]
	_, versionsExists := activator.versions[key]
	_, poolSandboxesExists := activator.poolSandboxes[pool.ID]
	activator.mu.RUnlock()

	require.True(t, poolsExists, "Pool should be cached in pools map after recovery")
	require.True(t, versionsExists, "Version->pool mapping should exist after recovery")
	require.True(t, poolSandboxesExists, "poolSandboxes entry should exist after recovery")

	// Wait for the pool watch to be established before deleting
	// This ensures the watch will see the delete event
	watchCtx, watchCancel := context.WithTimeout(ctx, 5*time.Second)
	defer watchCancel()
	err = server.Store.WaitForIndexWatcher(watchCtx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandboxPool))
	require.NoError(t, err, "pool watch should be established")

	// Delete the pool from entity store
	_, err = server.EAC.Delete(ctx, pool.ID.String())
	require.NoError(t, err)

	// Wait for watchPools to process the deletion
	// The watch should detect the deletion and clean up all caches
	require.Eventually(t, func() bool {
		activator.mu.RLock()
		defer activator.mu.RUnlock()
		_, poolsExists := activator.pools[key]
		_, versionsExists := activator.versions[key]
		_, poolSandboxesExists := activator.poolSandboxes[pool.ID]
		return !poolsExists && !versionsExists && !poolSandboxesExists
	}, 5*time.Second, 50*time.Millisecond, "watchPools should clean up all caches after pool deletion")

	// Verify all caches are cleaned up
	activator.mu.RLock()
	_, poolsStillExists := activator.pools[key]
	_, versionsStillExists := activator.versions[key]
	_, poolSandboxesStillExists := activator.poolSandboxes[pool.ID]
	activator.mu.RUnlock()

	assert.False(t, poolsStillExists, "pools cache should be cleaned up")
	assert.False(t, versionsStillExists, "versions cache should be cleaned up")
	assert.False(t, poolSandboxesStillExists, "poolSandboxes cache should be cleaned up")
}

// TestActivatorDoesNotFailFastOnStaleDeadSandboxWhenScalingUp verifies that when
// requesting new capacity (incrementPool=true), the activator does NOT fail fast
// just because there are old DEAD sandboxes in tracking from previous scale-down cycles.
//
// This reproduces a bug where:
// 1. App was idle, scaled to 0
// 2. User request arrives after idle period
// 3. Activator increments DesiredInstances and waits for new sandbox
// 4. Fail-fast check sees old DEAD sandbox from previous scale-down
// 5. BUG: Immediately returns ErrSandboxDiedEarly before new sandbox is created
// 6. User sees "failed to boot" error, but refresh works because sandbox was created
//
// The fix tracks whether new sandboxes were created after incrementing the pool,
// and only fails fast if a NEW sandbox was created and then died (real boot failure).
func TestActivatorDoesNotFailFastOnStaleDeadSandboxWhenScalingUp(t *testing.T) {
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
		DesiredInstances:     0, // Scaled to 0 (idle)
	}
	poolID, err := server.Client.Create(ctx, "launcher-pool", launcherPool)
	require.NoError(t, err)
	launcherPool.ID = poolID

	// Create activator with a stale DEAD sandbox from previous scale-down
	// This simulates the state when:
	// 1. A sandbox was running, became idle
	// 2. Scale-down monitor decremented DesiredInstances
	// 3. Pool reconciler marked sandbox as STOPPED, then DEAD
	// 4. The DEAD sandbox is still tracked in activator's poolSandboxes
	ent := entity.Blank()
	ent.SetID("stale-dead-sandbox")

	strategy := concurrency.NewStrategy(&testVer.Config.Services[0].ServiceConcurrency)
	tracker := strategy.InitializeTracker()

	staleDEADSandbox := &sandbox{
		sandbox: &compute_v1alpha.Sandbox{
			ID:     entity.Id("stale-dead-sandbox"),
			Status: compute_v1alpha.DEAD, // From previous scale-down
			Spec: compute_v1alpha.SandboxSpec{
				Version: testVer.ID,
			},
		},
		ent:         ent,
		lastRenewal: time.Now().Add(-10 * time.Minute), // Old
		url:         "http://10.0.0.1:3000",
		tracker:     tracker,
	}

	activator := &localActivator{
		log:             log,
		eac:             server.EAC,
		versions:        make(map[verKey]*versionPoolRef),
		poolSandboxes:   make(map[entity.Id]*poolSandboxes),
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	// Set up version->pool mapping with the stale DEAD sandbox
	key := verKey{testVer.ID.String(), "web"}
	activator.versions[key] = &versionPoolRef{
		ver:      testVer,
		poolID:   poolID,
		service:  "web",
		strategy: strategy,
	}
	activator.poolSandboxes[poolID] = &poolSandboxes{
		pool:      launcherPool,
		sandboxes: []*sandbox{staleDEADSandbox}, // Has stale DEAD sandbox
		service:   "web",
		strategy:  strategy,
	}

	// Simulate a sandbox becoming RUNNING after pool increment
	// This is what normally happens: pool manager creates new sandbox
	go func() {
		time.Sleep(100 * time.Millisecond)

		// Create a NEW RUNNING sandbox
		newEnt := entity.Blank()
		newEnt.SetID("new-running-sandbox")
		newTracker := strategy.InitializeTracker()

		newSandbox := &sandbox{
			sandbox: &compute_v1alpha.Sandbox{
				ID:     entity.Id("new-running-sandbox"),
				Status: compute_v1alpha.RUNNING,
				Spec: compute_v1alpha.SandboxSpec{
					Version: testVer.ID,
				},
			},
			ent:         newEnt,
			lastRenewal: time.Now(),
			url:         "http://10.0.0.2:3000",
			tracker:     newTracker,
		}

		activator.mu.Lock()
		ps := activator.poolSandboxes[poolID]
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

		log.Info("new sandbox became RUNNING", "sandbox", newSandbox.sandbox.ID)
	}()

	// Try to acquire a lease
	// WITHOUT THE FIX: This would immediately return ErrSandboxDiedEarly
	// because the fail-fast check sees the stale DEAD sandbox
	// WITH THE FIX: It waits for the new sandbox to be created
	timeoutCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	lease, err := activator.AcquireLease(timeoutCtx, testVer, "web")
	elapsed := time.Since(start)

	// Should succeed after waiting for the new sandbox
	require.NoError(t, err, "Should NOT fail fast with stale DEAD sandbox when incrementPool=true")
	require.NotNil(t, lease)

	// Verify we got the NEW sandbox, not the stale one
	assert.Equal(t, entity.Id("new-running-sandbox"), lease.sandbox.ID)
	assert.Equal(t, compute_v1alpha.RUNNING, lease.sandbox.Status)

	// Verify we waited (should be ~100ms for sandbox to become ready)
	assert.Greater(t, elapsed, 50*time.Millisecond, "Should have waited for new sandbox")
	assert.Less(t, elapsed, 300*time.Millisecond, "Should not have timed out")
}

// TestActivatorFailsFastOnNewSandboxDeath verifies that when incrementPool=true
// AND a new sandbox was actually created but then died, we DO fail fast.
// This distinguishes real boot failures from stale sandbox tracking.
func TestActivatorFailsFastOnNewSandboxDeath(t *testing.T) {
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

	// Pre-create a pool in the entity store
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
		DesiredInstances:     0,
	}
	poolID, err := server.Client.Create(ctx, "launcher-pool", launcherPool)
	require.NoError(t, err)
	launcherPool.ID = poolID

	// Start with one stale DEAD sandbox (from previous scale-down)
	ent := entity.Blank()
	ent.SetID("stale-dead-sandbox")

	strategy := concurrency.NewStrategy(&testVer.Config.Services[0].ServiceConcurrency)
	tracker := strategy.InitializeTracker()

	staleDEADSandbox := &sandbox{
		sandbox: &compute_v1alpha.Sandbox{
			ID:     entity.Id("stale-dead-sandbox"),
			Status: compute_v1alpha.DEAD,
			Spec: compute_v1alpha.SandboxSpec{
				Version: testVer.ID,
			},
		},
		ent:         ent,
		lastRenewal: time.Now().Add(-10 * time.Minute),
		url:         "http://10.0.0.1:3000",
		tracker:     tracker,
	}

	activator := &localActivator{
		log:             log,
		eac:             server.EAC,
		versions:        make(map[verKey]*versionPoolRef),
		poolSandboxes:   make(map[entity.Id]*poolSandboxes),
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	key := verKey{testVer.ID.String(), "web"}
	activator.versions[key] = &versionPoolRef{
		ver:      testVer,
		poolID:   poolID,
		service:  "web",
		strategy: strategy,
	}
	activator.poolSandboxes[poolID] = &poolSandboxes{
		pool:      launcherPool,
		sandboxes: []*sandbox{staleDEADSandbox},
		service:   "web",
		strategy:  strategy,
	}

	// Simulate a NEW sandbox being created that crashes during boot
	go func() {
		time.Sleep(50 * time.Millisecond)

		// Create a NEW sandbox that goes PENDING -> DEAD (boot crash)
		newEnt := entity.Blank()
		newEnt.SetID("new-crashed-sandbox")
		newTracker := strategy.InitializeTracker()

		newSandbox := &sandbox{
			sandbox: &compute_v1alpha.Sandbox{
				ID:     entity.Id("new-crashed-sandbox"),
				Status: compute_v1alpha.DEAD, // Crashed during boot!
				Spec: compute_v1alpha.SandboxSpec{
					Version: testVer.ID,
				},
			},
			ent:         newEnt,
			lastRenewal: time.Now(),
			url:         "",
			tracker:     newTracker,
		}

		activator.mu.Lock()
		ps := activator.poolSandboxes[poolID]
		ps.sandboxes = append(ps.sandboxes, newSandbox)

		// Notify waiters that a sandbox changed
		if chans, ok := activator.newSandboxChans[key]; ok {
			for _, ch := range chans {
				select {
				case ch <- struct{}{}:
				default:
				}
			}
		}
		activator.mu.Unlock()

		log.Info("new sandbox crashed during boot", "sandbox", newSandbox.sandbox.ID)
	}()

	// Try to acquire a lease
	// This should fail fast because a NEW sandbox was created and died
	timeoutCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	lease, err := activator.AcquireLease(timeoutCtx, testVer, "web")
	elapsed := time.Since(start)

	// Should fail with ErrSandboxDiedEarly because the NEW sandbox crashed
	require.Error(t, err)
	require.ErrorIs(t, err, ErrSandboxDiedEarly, "Should fail fast when NEW sandbox crashes")
	require.Nil(t, lease)

	// Should fail fast (< 200ms), not wait for full timeout
	assert.Less(t, elapsed, 200*time.Millisecond, "Should fail fast on new sandbox death")
}

// TestActivatorDoesNotFailFastWhenPoolIsReusedByNewVersion verifies the fix
// for MIR-1023: when a new app version reuses an existing pool (because the
// sandbox spec matches), dead sandboxes from the previous version sharing the
// pool must not cause the activator to fail-fast for the new version before
// it gets a chance to boot its own sandbox.
//
// Repro of the outage scenario:
//  1. v1 deployed, crashed 9 times, leaving 9 DEAD sandboxes in the pool
//  2. v2 deployed (a fix); deployment controller reused v1's pool
//  3. Activator sees v1's 9 DEAD sandboxes, fail-fasts v2 without trying
//  4. v2 never gets to boot a sandbox; manual rollback required
//
// The fix scopes the fail-fast sandbox accounting to the requesting version.
func TestActivatorDoesNotFailFastWhenPoolIsReusedByNewVersion(t *testing.T) {
	ctx := context.Background()

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)
	app.ID = appID

	makeVer := func(name string) *core_v1alpha.AppVersion {
		v := &core_v1alpha.AppVersion{
			App:      app.ID,
			Version:  name,
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
		vID, err := server.Client.Create(ctx, "test-ver-"+name, v)
		require.NoError(t, err)
		v.ID = vID
		return v
	}

	v1 := makeVer("v1")
	v2 := makeVer("v2")

	// Pool was created for v1 and then reused by v2 when v2 deployed with a
	// matching sandbox spec. DesiredInstances=1 is carried over from v1.
	pool := &compute_v1alpha.SandboxPool{
		Service: "web",
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Version: v1.ID,
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
		ReferencedByVersions: []entity.Id{v1.ID, v2.ID},
		DesiredInstances:     1,
	}
	poolID, err := server.Client.Create(ctx, "shared-pool", pool)
	require.NoError(t, err)
	pool.ID = poolID

	log := testutils.TestLogger(t)
	strategy := concurrency.NewStrategy(&v2.Config.Services[0].ServiceConcurrency)

	// 9 DEAD sandboxes in the shared pool, all tagged as v1's.
	var deadSandboxes []*sandbox
	for i := range 9 {
		id := entity.Id("dead-v1-" + string(rune('0'+i)))
		ent := entity.Blank()
		ent.SetID(id)
		deadSandboxes = append(deadSandboxes, &sandbox{
			sandbox: &compute_v1alpha.Sandbox{
				ID:     id,
				Status: compute_v1alpha.DEAD,
				Spec: compute_v1alpha.SandboxSpec{
					Version: v1.ID,
				},
			},
			ent:         ent,
			lastRenewal: time.Now().Add(-5 * time.Minute),
			url:         "",
			tracker:     strategy.InitializeTracker(),
		})
	}

	activator := &localActivator{
		log:             log,
		eac:             server.EAC,
		versions:        make(map[verKey]*versionPoolRef),
		poolSandboxes:   make(map[entity.Id]*poolSandboxes),
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	// Activator knows about v1 and its pool. v2 is not yet in versions map —
	// requestPoolCapacity will wire it up when AcquireLease is called below.
	activator.poolSandboxes[poolID] = &poolSandboxes{
		pool:      pool,
		sandboxes: deadSandboxes,
		service:   "web",
		strategy:  strategy,
	}
	v1Key := verKey{v1.ID.String(), "web"}
	activator.versions[v1Key] = &versionPoolRef{
		ver:      v1,
		poolID:   poolID,
		service:  "web",
		strategy: strategy,
	}

	// After a short delay, simulate the pool manager booting a fresh sandbox
	// for v2. In the wild, this appears via the watchSandboxes path; here we
	// poke it directly to keep the test focused on the fail-fast decision.
	go func() {
		time.Sleep(100 * time.Millisecond)

		newEnt := entity.Blank()
		newEnt.SetID("new-v2-sandbox")

		newSandbox := &sandbox{
			sandbox: &compute_v1alpha.Sandbox{
				ID:     entity.Id("new-v2-sandbox"),
				Status: compute_v1alpha.RUNNING,
				Spec: compute_v1alpha.SandboxSpec{
					Version: v2.ID,
				},
			},
			ent:         newEnt,
			lastRenewal: time.Now(),
			url:         "http://10.0.0.42:3000",
			tracker:     strategy.InitializeTracker(),
		}

		activator.mu.Lock()
		ps := activator.poolSandboxes[poolID]
		ps.sandboxes = append(ps.sandboxes, newSandbox)

		v2Key := verKey{v2.ID.String(), "web"}
		if chans, ok := activator.newSandboxChans[v2Key]; ok {
			for _, ch := range chans {
				select {
				case ch <- struct{}{}:
				default:
				}
			}
		}
		activator.mu.Unlock()
	}()

	timeoutCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	start := time.Now()
	lease, err := activator.AcquireLease(timeoutCtx, v2, "web")
	elapsed := time.Since(start)

	require.NoError(t, err, "v2 should not fail-fast on v1's stale dead sandboxes sharing the pool")
	require.NotNil(t, lease)
	assert.Equal(t, entity.Id("new-v2-sandbox"), lease.sandbox.ID)
	assert.Equal(t, v2.ID, lease.sandbox.Spec.Version)
	assert.Greater(t, elapsed, 50*time.Millisecond, "Should have waited for v2's sandbox")
}

// TestRemovePoolFromTrackingCleansAllCaches verifies that removePoolFromTracking
// correctly removes entries from all three cache maps.
func TestRemovePoolFromTrackingCleansAllCaches(t *testing.T) {
	log := testutils.TestLogger(t)

	poolID := entity.Id("pool-1")
	poolID2 := entity.Id("pool-2")

	strategy := concurrency.NewStrategy(&core_v1alpha.ServiceConcurrency{
		Mode:                "auto",
		RequestsPerInstance: 10,
		ScaleDownDelay:      "15m",
	})

	testVer := &core_v1alpha.AppVersion{
		ID:       entity.Id("ver-1"),
		App:      entity.Id("app-1"),
		Version:  "v1",
		ImageUrl: "test:latest",
	}

	testVer2 := &core_v1alpha.AppVersion{
		ID:       entity.Id("ver-2"),
		App:      entity.Id("app-1"),
		Version:  "v2",
		ImageUrl: "test:latest",
	}

	pool := &compute_v1alpha.SandboxPool{ID: poolID, Service: "web"}
	pool2 := &compute_v1alpha.SandboxPool{ID: poolID2, Service: "web"}

	activator := &localActivator{
		log: log,
		versions: map[verKey]*versionPoolRef{
			{"ver-1", "web"}: {ver: testVer, poolID: poolID, service: "web", strategy: strategy},
			{"ver-2", "web"}: {ver: testVer2, poolID: poolID2, service: "web", strategy: strategy},
		},
		pools: map[verKey]*poolState{
			{"ver-1", "web"}: {pool: pool, revision: 1},
			{"ver-2", "web"}: {pool: pool2, revision: 2},
		},
		poolSandboxes: map[entity.Id]*poolSandboxes{
			poolID:  {pool: pool, sandboxes: []*sandbox{}, service: "web", strategy: strategy},
			poolID2: {pool: pool2, sandboxes: []*sandbox{}, service: "web", strategy: strategy},
		},
	}

	// Remove pool-1 from tracking
	activator.removePoolFromTracking(poolID)

	// Verify pool-1 entries are gone
	activator.mu.RLock()
	_, versionsExists := activator.versions[verKey{"ver-1", "web"}]
	_, poolsExists := activator.pools[verKey{"ver-1", "web"}]
	_, poolSandboxesExists := activator.poolSandboxes[poolID]

	// Verify pool-2 entries are still there
	_, versions2Exists := activator.versions[verKey{"ver-2", "web"}]
	_, pools2Exists := activator.pools[verKey{"ver-2", "web"}]
	_, poolSandboxes2Exists := activator.poolSandboxes[poolID2]
	activator.mu.RUnlock()

	assert.False(t, versionsExists, "pool-1 should be removed from versions")
	assert.False(t, poolsExists, "pool-1 should be removed from pools")
	assert.False(t, poolSandboxesExists, "pool-1 should be removed from poolSandboxes")

	assert.True(t, versions2Exists, "pool-2 should still be in versions")
	assert.True(t, pools2Exists, "pool-2 should still be in pools")
	assert.True(t, poolSandboxes2Exists, "pool-2 should still be in poolSandboxes")
}

// TestEvictStaleVersionBindingsOnDereference verifies that when a pool stops
// referencing a version but still exists (the launcher drained a superseded
// pool without deleting the entity), the binding for the dropped version is
// evicted while bindings for versions the pool still references are retained
// (MIR-1293).
func TestEvictStaleVersionBindingsOnDereference(t *testing.T) {
	log := testutils.TestLogger(t)

	poolID := entity.Id("pool-1")
	staleKey := verKey{"ver-stale", "web"}
	keptKey := verKey{"ver-kept", "web"}

	pool := &compute_v1alpha.SandboxPool{ID: poolID, Service: "web"}

	activator := &localActivator{
		log: log,
		versions: map[verKey]*versionPoolRef{
			staleKey: {poolID: poolID, service: "web"},
			keptKey:  {poolID: poolID, service: "web"},
		},
		pools: map[verKey]*poolState{
			staleKey: {pool: pool, revision: 1},
			keptKey:  {pool: pool, revision: 1},
		},
		poolSandboxes: map[entity.Id]*poolSandboxes{
			poolID: {pool: pool, sandboxes: []*sandbox{}, service: "web"},
		},
	}

	// The launcher drained the pool for ver-stale, but it still serves ver-kept.
	freshPool := &compute_v1alpha.SandboxPool{
		ID:                   poolID,
		Service:              "web",
		ReferencedByVersions: []entity.Id{entity.Id("ver-kept")},
	}

	activator.mu.Lock()
	activator.evictStaleVersionBindingsLocked(freshPool)
	activator.mu.Unlock()

	activator.mu.RLock()
	_, staleVerExists := activator.versions[staleKey]
	_, stalePoolExists := activator.pools[staleKey]
	_, keptVerExists := activator.versions[keptKey]
	_, keptPoolExists := activator.pools[keptKey]
	activator.mu.RUnlock()

	assert.False(t, staleVerExists, "dereferenced version binding should be evicted")
	assert.False(t, stalePoolExists, "dereferenced version pool state should be evicted")
	assert.True(t, keptVerExists, "still-referenced version binding should be retained")
	assert.True(t, keptPoolExists, "still-referenced version pool state should be retained")
}

// TestWatchPoolsEvictsStaleBindingOnDereference verifies the eviction is wired
// into the pool watch: when the launcher dereferences a still-existing pool (the
// EAC.Replace path updatePool uses to drain a superseded pool), the watch drops
// the now-stale version->pool binding so the next lease request re-resolves to
// the canonical pool (MIR-1293).
//
// This uses the etcd-backed server rather than NewInMemEntityServer because the
// in-mem index watch classifies every mutation as a Create op and never delivers
// the Update op this exercises (the same reason TestConcurrentPoolIncrement uses
// the etcd server for real OCC semantics).
func TestWatchPoolsEvictsStaleBindingOnDereference(t *testing.T) {
	ctx := t.Context()

	server, cleanup := testutils.NewEtcdEntityServer(t)
	defer cleanup()

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
					Name:               "web",
					ServiceConcurrency: core_v1alpha.ServiceConcurrency{Mode: "auto", RequestsPerInstance: 10},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", testVer)
	require.NoError(t, err)
	testVer.ID = verID

	pool := &compute_v1alpha.SandboxPool{
		Service:              "web",
		DesiredInstances:     1,
		ReferencedByVersions: []entity.Id{testVer.ID},
		SandboxSpec:          compute_v1alpha.SandboxSpec{Version: testVer.ID},
	}
	poolID, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)
	pool.ID = poolID

	poolResp, err := server.EAC.Get(ctx, poolID.String())
	require.NoError(t, err)

	log := testutils.TestLogger(t)
	key := verKey{testVer.ID.String(), "web"}

	activator := &localActivator{
		log: log,
		eac: server.EAC,
		versions: map[verKey]*versionPoolRef{
			key: {
				ver:      testVer,
				poolID:   poolID,
				service:  "web",
				strategy: concurrency.NewStrategy(&testVer.Config.Services[0].ServiceConcurrency),
			},
		},
		poolSandboxes: map[entity.Id]*poolSandboxes{
			poolID: {pool: pool, service: "web"},
		},
		pools: map[verKey]*poolState{
			key: {pool: pool, revision: poolResp.Entity().Revision()},
		},
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	go activator.watchPools(ctx)

	// Wait until the pool watch is live before issuing the single dereference
	// below: a late subscription would deliver the pool as part of its initial
	// snapshot (a Create) rather than as the live Update the eviction keys on. Bump
	// a harmless field until the watch reflects it in the cache. Each iteration is
	// a real change so it always produces an op, and the >-threshold check avoids
	// chasing a moving target across polls.
	origDesired := pool.DesiredInstances
	nextDesired := origDesired
	require.Eventually(t, func() bool {
		nextDesired++
		pool.DesiredInstances = nextDesired
		if err := server.Client.Update(ctx, pool); err != nil {
			return false
		}
		activator.mu.RLock()
		defer activator.mu.RUnlock()
		st, ok := activator.pools[key]
		return ok && st.pool != nil && st.pool.DesiredInstances > origDesired
	}, 5*time.Second, 50*time.Millisecond, "pool watch should become live")

	// Dereference the pool the way updatePool does: rebuild its attributes
	// without ReferencedByVersions and Replace, so the pool still exists but no
	// longer references the version.
	var finalAttrs []entity.Attr
	for _, attr := range poolResp.Entity().Attrs() {
		if attr.ID == compute_v1alpha.SandboxPoolReferencedByVersionsId {
			continue
		}
		finalAttrs = append(finalAttrs, attr)
	}
	_, err = server.EAC.Replace(ctx, finalAttrs, 0)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		activator.mu.RLock()
		defer activator.mu.RUnlock()
		_, vOk := activator.versions[key]
		_, pOk := activator.pools[key]
		return !vOk && !pOk
	}, 5*time.Second, 50*time.Millisecond,
		"watch should evict the stale version->pool binding after the pool is dereferenced")
}

// TestActivatorRefreshesStaleMaxPoolSizeCache verifies that when the in-memory cache
// shows DesiredInstances >= MaxPoolSize but the entity store has been reset to a lower
// value, the activator re-reads from the store and continues rather than permanently
// refusing to increment.
func TestActivatorRefreshesStaleMaxPoolSizeCache(t *testing.T) {
	ctx := context.Background()

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app and version
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
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", testVer)
	require.NoError(t, err)
	testVer.ID = verID

	// Create pool in entity store with DesiredInstances = 0 (simulating pool manager reset)
	pool := &compute_v1alpha.SandboxPool{
		Service:              "web",
		DesiredInstances:     0,
		ReferencedByVersions: []entity.Id{testVer.ID},
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
	}
	poolID, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)
	pool.ID = poolID

	// Get the current revision from the store
	poolResp, err := server.EAC.Get(ctx, poolID.String())
	require.NoError(t, err)

	log := testutils.TestLogger(t)
	key := verKey{testVer.ID.String(), "web"}

	// Set up activator with a STALE cache that thinks DesiredInstances = MaxPoolSize
	stalePool := *pool
	stalePool.DesiredInstances = concurrency.MaxPoolSize

	activator := &localActivator{
		log: log,
		eac: server.EAC,
		versions: map[verKey]*versionPoolRef{
			key: {
				ver:      testVer,
				poolID:   poolID,
				service:  "web",
				strategy: concurrency.NewStrategy(&testVer.Config.Services[0].ServiceConcurrency),
			},
		},
		poolSandboxes: map[entity.Id]*poolSandboxes{
			poolID: {
				pool:    &stalePool,
				service: "web",
			},
		},
		pools: map[verKey]*poolState{
			key: {
				pool:     &stalePool,
				revision: poolResp.Entity().Revision(),
			},
		},
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	// Call requestPoolCapacity - with the old code this would return
	// "pool has reached maximum size" without consulting the entity store.
	// With the fix, it should re-read, find DesiredInstances=0, and succeed.
	resultPool, err := activator.requestPoolCapacity(ctx, testVer, "web")
	require.NoError(t, err)
	require.NotNil(t, resultPool)

	// Verify the cache was updated with the fresh value + 1
	activator.mu.RLock()
	cachedState := activator.pools[key]
	activator.mu.RUnlock()

	assert.Equal(t, int64(1), cachedState.pool.DesiredInstances,
		"cache should reflect the incremented value from the fresh store read")
}

// TestWatchPoolsUpdatesDesiredInstances verifies that the watchPools goroutine
// updates the in-memory cache when a pool's DesiredInstances changes externally.
func TestWatchPoolsUpdatesDesiredInstances(t *testing.T) {
	ctx := t.Context()

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create app and version
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
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", testVer)
	require.NoError(t, err)
	testVer.ID = verID

	// Create pool in entity store
	pool := &compute_v1alpha.SandboxPool{
		Service:              "web",
		DesiredInstances:     5,
		ReferencedByVersions: []entity.Id{testVer.ID},
		SandboxSpec: compute_v1alpha.SandboxSpec{
			Version: testVer.ID,
		},
	}
	poolID, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)
	pool.ID = poolID

	poolResp, err := server.EAC.Get(ctx, poolID.String())
	require.NoError(t, err)

	log := testutils.TestLogger(t)
	key := verKey{testVer.ID.String(), "web"}

	activator := &localActivator{
		log: log,
		eac: server.EAC,
		versions: map[verKey]*versionPoolRef{
			key: {
				ver:      testVer,
				poolID:   poolID,
				service:  "web",
				strategy: concurrency.NewStrategy(&testVer.Config.Services[0].ServiceConcurrency),
			},
		},
		poolSandboxes: map[entity.Id]*poolSandboxes{
			poolID: {
				pool:    pool,
				service: "web",
			},
		},
		pools: map[verKey]*poolState{
			key: {
				pool:     pool,
				revision: poolResp.Entity().Revision(),
			},
		},
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	// Start the watch in a goroutine
	go activator.watchPools(ctx)

	// Give the watch a moment to establish
	time.Sleep(100 * time.Millisecond)

	// Update DesiredInstances in the entity store externally
	err = server.Client.Update(ctx, pool)
	require.NoError(t, err)

	// Patch to set new desired instances
	pool.DesiredInstances = 2
	err = server.Client.Update(ctx, pool)
	require.NoError(t, err)

	// Verify the cache is updated via the watch
	require.Eventually(t, func() bool {
		activator.mu.RLock()
		defer activator.mu.RUnlock()
		state, ok := activator.pools[key]
		return ok && state.pool != nil && state.pool.DesiredInstances == 2
	}, 5*time.Second, 50*time.Millisecond,
		"pool cache should be updated by watch to DesiredInstances=2")

	// Also verify poolSandboxes pool pointer was updated
	activator.mu.RLock()
	ps := activator.poolSandboxes[poolID]
	activator.mu.RUnlock()
	assert.Equal(t, int64(2), ps.pool.DesiredInstances,
		"poolSandboxes pool pointer should reflect the updated DesiredInstances")
}

// TestRequestPoolCapacityScalesNormalPool is a regression guard for the
// normal scale-up path: a non-ephemeral cached pool should still get its
// DesiredInstances incremented on capacity requests.
func TestRequestPoolCapacityScalesNormalPool(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

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

	pool := &compute_v1alpha.SandboxPool{
		Service:              "web",
		SandboxSpec:          compute_v1alpha.SandboxSpec{Version: testVer.ID},
		ReferencedByVersions: []entity.Id{testVer.ID},
		DesiredInstances:     1,
		// Ephemeral defaults to false.
	}
	poolID, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)
	pool.ID = poolID

	getRes, err := server.EAC.Get(ctx, poolID.String())
	require.NoError(t, err)
	initialRevision := getRes.Entity().Revision()

	activator := &localActivator{
		log:             log,
		eac:             server.EAC,
		versions:        make(map[verKey]*versionPoolRef),
		poolSandboxes:   make(map[entity.Id]*poolSandboxes),
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	key := verKey{testVer.ID.String(), "web"}
	activator.pools[key] = &poolState{
		pool:       pool,
		revision:   initialRevision,
		inProgress: false,
	}

	resultPool, err := activator.requestPoolCapacity(ctx, testVer, "web")
	require.NoError(t, err)
	assert.Equal(t, int64(2), resultPool.DesiredInstances,
		"non-ephemeral pool should still scale up normally")
}

// TestRequestPoolCapacityAtMaxReturnsExistingPool verifies that when a cached
// pool is already at its strategy-enforced cap (e.g. an ephemeral pool seeded
// at DesiredInstances=1 with MaxInstances=1), requestPoolCapacity returns the
// pool without erroring. The original "at cap" check was a runaway guard that
// errored; with type-driven caps, "at cap" is the normal operating point for
// ephemeral pools and must not surface as a lease-acquisition failure.
func TestRequestPoolCapacityAtMaxReturnsExistingPool(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)

	testVer := &core_v1alpha.AppVersion{
		App:            appID,
		Version:        "v1",
		ImageUrl:       "test:latest",
		EphemeralLabel: "feat-x",
		Config: core_v1alpha.Config{
			Services: []core_v1alpha.Services{
				{Name: "web"},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", testVer)
	require.NoError(t, err)
	testVer.ID = verID

	pool := &compute_v1alpha.SandboxPool{
		Service:              "web",
		SandboxSpec:          compute_v1alpha.SandboxSpec{Version: testVer.ID},
		ReferencedByVersions: []entity.Id{testVer.ID},
		DesiredInstances:     1,
	}
	poolID, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)
	pool.ID = poolID

	getRes, err := server.EAC.Get(ctx, poolID.String())
	require.NoError(t, err)
	initialRevision := getRes.Entity().Revision()

	activator := &localActivator{
		log:             log,
		eac:             server.EAC,
		versions:        make(map[verKey]*versionPoolRef),
		poolSandboxes:   make(map[entity.Id]*poolSandboxes),
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	key := verKey{testVer.ID.String(), "web"}
	activator.pools[key] = &poolState{
		pool:       pool,
		revision:   initialRevision,
		inProgress: false,
	}

	for i := range 5 {
		resultPool, err := activator.requestPoolCapacity(ctx, testVer, "web")
		require.NoError(t, err, "iteration %d: at-cap pool must not error", i)
		require.NotNil(t, resultPool)
		assert.Equal(t, int64(1), resultPool.DesiredInstances,
			"iteration %d: pool must not be incremented past EphemeralStrategy's MaxInstances=1", i)
	}

	finalRes, err := server.EAC.Get(ctx, poolID.String())
	require.NoError(t, err)
	assert.Equal(t, initialRevision, finalRes.Entity().Revision(),
		"no Patch should have been issued; entity revision should not advance")
}

// TestWaitForSandboxSkipsEmptyURL verifies that checkForSandbox does not
// return a Lease for a sandbox that is RUNNING but whose URL has not yet
// been populated. Returning such a lease would cause the proxy to fail
// with "unsupported protocol scheme """ on an empty target URL.
func TestWaitForSandboxSkipsEmptyURL(t *testing.T) {
	ctx := context.Background()

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

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
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", testVer)
	require.NoError(t, err)
	testVer.ID = verID

	log := testutils.TestLogger(t)
	activator := &localActivator{
		log:             log,
		eac:             server.EAC,
		versions:        make(map[verKey]*versionPoolRef),
		poolSandboxes:   make(map[entity.Id]*poolSandboxes),
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	strategy := concurrency.NewStrategy(&testVer.Config.Services[0].ServiceConcurrency)
	tracker := strategy.InitializeTracker()

	// A sandbox that has flipped to RUNNING but whose URL is not yet populated.
	// This mimics the window where watchSandboxes observed the status change
	// in one watch event but the network address has not yet arrived.
	sb := &sandbox{
		sandbox: &compute_v1alpha.Sandbox{
			ID:     entity.Id("sandbox/test-sb"),
			Status: compute_v1alpha.RUNNING,
			Spec:   compute_v1alpha.SandboxSpec{Version: testVer.ID},
		},
		ent:         entity.Blank(),
		lastRenewal: time.Now(),
		url:         "", // intentionally empty
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
		sandboxes: []*sandbox{sb},
		service:   "web",
		strategy:  strategy,
	}
	activator.mu.Unlock()

	// Use incrementPool=false so we don't try to patch a pool; we just want
	// to exercise the polling helper. Give it a short outer context so the
	// test does not have to wait for the 120s internal timeout.
	waitCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()

	lease, err := activator.waitForSandbox(waitCtx, testVer, "web", false)
	require.Error(t, err, "should not return a Lease for an empty-URL sandbox")
	assert.Nil(t, lease)
}

// TestAcquireLeaseSucceedsAfterURLArrives verifies the full path: a sandbox is
// tracked as RUNNING with an empty URL; a waiter is blocked in waitForSandbox;
// once the URL becomes available and the notification fires, the waiter wakes
// and receives a Lease pointing at the real URL.
func TestAcquireLeaseSucceedsAfterURLArrives(t *testing.T) {
	ctx := context.Background()

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

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
					},
				},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", testVer)
	require.NoError(t, err)
	testVer.ID = verID

	log := testutils.TestLogger(t)
	activator := &localActivator{
		log:             log,
		eac:             server.EAC,
		versions:        make(map[verKey]*versionPoolRef),
		poolSandboxes:   make(map[entity.Id]*poolSandboxes),
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	strategy := concurrency.NewStrategy(&testVer.Config.Services[0].ServiceConcurrency)
	tracker := strategy.InitializeTracker()

	sb := &sandbox{
		sandbox: &compute_v1alpha.Sandbox{
			ID:     entity.Id("sandbox/late-url"),
			Status: compute_v1alpha.RUNNING,
			Spec:   compute_v1alpha.SandboxSpec{Version: testVer.ID},
		},
		ent:         entity.Blank(),
		lastRenewal: time.Now(),
		url:         "", // arrives later
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
		sandboxes: []*sandbox{sb},
		service:   "web",
		strategy:  strategy,
	}
	activator.mu.Unlock()

	// Populate the URL after a short delay and notify waiters — this is
	// what Fix 2 would arrange when a watch event populates the URL on a
	// sandbox that was already RUNNING.
	go func() {
		time.Sleep(100 * time.Millisecond)
		activator.mu.Lock()
		sb.url = "http://10.0.0.1:3000"
		chans := activator.newSandboxChans[key]
		for _, ch := range chans {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
		activator.mu.Unlock()
	}()

	// Bound the wait so a regression in the URL-arrival notification path
	// fails fast (within 2s) rather than hanging for the full 120s internal
	// timeout on waitForSandbox.
	waitCtx, cancelWait := context.WithTimeout(ctx, 2*time.Second)
	defer cancelWait()

	start := time.Now()
	lease, err := activator.waitForSandbox(waitCtx, testVer, "web", false)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, lease)
	assert.Equal(t, "http://10.0.0.1:3000", lease.URL, "lease should carry the populated URL")
	assert.Greater(t, elapsed, 50*time.Millisecond, "should have waited for the URL to arrive")
	assert.Less(t, elapsed, 2*time.Second, "should not have timed out")
}

// fakePoolCreator is a test PoolCreator that delegates to a closure, letting a
// test control when and how the on-demand pool is created.
type fakePoolCreator struct {
	create func(ctx context.Context, ver *core_v1alpha.AppVersion, service string) (entity.Id, error)
}

func (f *fakePoolCreator) CreatePoolForVersion(ctx context.Context, ver *core_v1alpha.AppVersion, service string) (entity.Id, error) {
	return f.create(ctx, ver, service)
}

func newEphemeralVersion(t *testing.T, server *testutils.InMemEntityServer, ctx context.Context) *core_v1alpha.AppVersion {
	t.Helper()

	app := &core_v1alpha.App{}
	appID, err := server.Client.Create(ctx, "test-app", app)
	require.NoError(t, err)

	ver := &core_v1alpha.AppVersion{
		App:            appID,
		Version:        "v1",
		ImageUrl:       "test:latest",
		EphemeralLabel: "feat-x",
		Config: core_v1alpha.Config{
			Services: []core_v1alpha.Services{
				{Name: "web"},
			},
		},
	}
	verID, err := server.Client.Create(ctx, "test-ver", ver)
	require.NoError(t, err)
	ver.ID = verID
	return ver
}

// TestRequestPoolCapacityOnDemandSeedsVersionMapping is the direct MIR-1198
// regression test. Ephemeral versions have no pool pre-created by the launcher,
// so requestPoolCapacity creates one on demand via the PoolCreator. That branch
// used to seed only a.pools, leaving a.versions empty — so AcquireLease reported
// "tracked: false" and never handed out a lease even though the pool (and later a
// sandbox) existed. It must now seed a.versions and a.poolSandboxes too.
func TestRequestPoolCapacityOnDemandSeedsVersionMapping(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	testVer := newEphemeralVersion(t, server, ctx)

	activator := &localActivator{
		log:             log,
		eac:             server.EAC,
		versions:        make(map[verKey]*versionPoolRef),
		poolSandboxes:   make(map[entity.Id]*poolSandboxes),
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	var createdPoolID entity.Id
	activator.poolCreator = &fakePoolCreator{
		create: func(ctx context.Context, ver *core_v1alpha.AppVersion, service string) (entity.Id, error) {
			pool := &compute_v1alpha.SandboxPool{
				Service:              service,
				SandboxSpec:          compute_v1alpha.SandboxSpec{Version: ver.ID},
				ReferencedByVersions: []entity.Id{ver.ID},
				DesiredInstances:     1,
			}
			id, err := server.Client.Create(ctx, "ondemand-pool", pool)
			if err != nil {
				return "", err
			}
			createdPoolID = id
			return id, nil
		},
	}

	resultPool, err := activator.requestPoolCapacity(ctx, testVer, "web")
	require.NoError(t, err)
	require.NotNil(t, resultPool)

	key := verKey{testVer.ID.String(), "web"}

	activator.mu.RLock()
	defer activator.mu.RUnlock()

	versionRef, ok := activator.versions[key]
	require.True(t, ok, "on-demand pool creation must seed a.versions for the ephemeral version (MIR-1198)")
	assert.Equal(t, createdPoolID, versionRef.poolID)

	_, ok = activator.poolSandboxes[createdPoolID]
	require.True(t, ok, "on-demand pool creation must seed an a.poolSandboxes entry")
}

// TestRequestPoolCapacityCachedSeedsVersionMapping covers the self-heal path: a
// pool created on-demand before this fix shipped (or one whose watcher mapping was
// lost) leaves a.pools cached but a.versions empty. Every subsequent call hits the
// cached-pool branch, which must now seed a.versions so the version becomes
// resolvable without requiring a process restart.
func TestRequestPoolCapacityCachedSeedsVersionMapping(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	testVer := newEphemeralVersion(t, server, ctx)

	pool := &compute_v1alpha.SandboxPool{
		Service:              "web",
		SandboxSpec:          compute_v1alpha.SandboxSpec{Version: testVer.ID},
		ReferencedByVersions: []entity.Id{testVer.ID},
		DesiredInstances:     1,
	}
	poolID, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)
	pool.ID = poolID

	getRes, err := server.EAC.Get(ctx, poolID.String())
	require.NoError(t, err)

	activator := &localActivator{
		log:             log,
		eac:             server.EAC,
		versions:        make(map[verKey]*versionPoolRef),
		poolSandboxes:   make(map[entity.Id]*poolSandboxes),
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	key := verKey{testVer.ID.String(), "web"}
	// Mimic a pool created on-demand before the fix: a.pools is cached but
	// a.versions was never seeded.
	activator.pools[key] = &poolState{
		pool:       pool,
		revision:   getRes.Entity().Revision(),
		inProgress: false,
	}

	_, err = activator.requestPoolCapacity(ctx, testVer, "web")
	require.NoError(t, err)

	activator.mu.RLock()
	defer activator.mu.RUnlock()

	versionRef, ok := activator.versions[key]
	require.True(t, ok, "cached-pool path must self-heal by seeding a.versions (MIR-1198)")
	assert.Equal(t, poolID, versionRef.poolID)
}

// TestAcquireLeaseEphemeralOnDemand exercises the full lease path for an ephemeral
// version end-to-end: AcquireLease -> waitForSandbox -> requestPoolCapacity creates
// the pool on demand (seeding a.versions), then the watcher (simulated here) injects
// the booted sandbox. Before the fix the version stayed untracked, so AcquireLease
// would hang until the 120s cap; this test bounds the wait to 2s so a regression
// fails fast.
func TestAcquireLeaseEphemeralOnDemand(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	testVer := newEphemeralVersion(t, server, ctx)
	strategy := concurrency.NewStrategyForVersion(testVer, "web", &testVer.Config.Services[0].ServiceConcurrency)

	activator := &localActivator{
		log:             log,
		eac:             server.EAC,
		versions:        make(map[verKey]*versionPoolRef),
		poolSandboxes:   make(map[entity.Id]*poolSandboxes),
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	activator.poolCreator = &fakePoolCreator{
		create: func(ctx context.Context, ver *core_v1alpha.AppVersion, service string) (entity.Id, error) {
			pool := &compute_v1alpha.SandboxPool{
				Service:              service,
				SandboxSpec:          compute_v1alpha.SandboxSpec{Version: ver.ID},
				ReferencedByVersions: []entity.Id{ver.ID},
				DesiredInstances:     1,
			}
			return server.Client.Create(ctx, "ondemand-pool", pool)
		},
	}

	key := verKey{testVer.ID.String(), "web"}

	// Simulate the watcher discovering the booted sandbox shortly after the pool
	// is created on-demand: poll until the version becomes tracked, then inject a
	// RUNNING sandbox with a URL and notify waiters.
	go func() {
		for {
			activator.mu.RLock()
			versionRef, ok := activator.versions[key]
			activator.mu.RUnlock()
			if ok {
				activator.mu.Lock()
				ps := activator.poolSandboxes[versionRef.poolID]
				ent := entity.Blank()
				ent.SetID("eph-sandbox")
				ps.sandboxes = append(ps.sandboxes, &sandbox{
					sandbox: &compute_v1alpha.Sandbox{
						ID:     entity.Id("eph-sandbox"),
						Status: compute_v1alpha.RUNNING,
						Spec:   compute_v1alpha.SandboxSpec{Version: testVer.ID},
					},
					ent:         ent,
					lastRenewal: time.Now(),
					url:         "http://10.0.0.7:3000",
					tracker:     strategy.InitializeTracker(),
				})
				if chans, ok := activator.newSandboxChans[key]; ok {
					for _, ch := range chans {
						select {
						case ch <- struct{}{}:
						default:
						}
					}
				}
				activator.mu.Unlock()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	acqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	lease, err := activator.AcquireLease(acqCtx, testVer, "web")
	require.NoError(t, err)
	require.NotNil(t, lease)
	assert.Equal(t, "http://10.0.0.7:3000", lease.URL)
}

// TestRecoverPoolsPreservesInProgressSentinel verifies recoverPools is safe to run
// as a live re-sync (on watch reconnect): it must not clobber an in-progress
// creation sentinel held by a concurrent requestPoolCapacity, which would strand
// the sentinel's waiters on the abandoned done channel.
func TestRecoverPoolsPreservesInProgressSentinel(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	testVer := newEphemeralVersion(t, server, ctx)

	pool := &compute_v1alpha.SandboxPool{
		Service:              "web",
		SandboxSpec:          compute_v1alpha.SandboxSpec{Version: testVer.ID},
		ReferencedByVersions: []entity.Id{testVer.ID},
		DesiredInstances:     1,
	}
	_, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)

	activator := &localActivator{
		log:             log,
		eac:             server.EAC,
		versions:        make(map[verKey]*versionPoolRef),
		poolSandboxes:   make(map[entity.Id]*poolSandboxes),
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	key := verKey{testVer.ID.String(), "web"}
	sentinel := &poolState{inProgress: true, done: make(chan struct{})}
	activator.pools[key] = sentinel

	require.NoError(t, activator.recoverPools(ctx))

	activator.mu.RLock()
	got := activator.pools[key]
	activator.mu.RUnlock()
	assert.Same(t, sentinel, got, "recoverPools must not replace an in-progress creation sentinel")
	assert.True(t, got.inProgress, "sentinel must remain in-progress")
}

// TestReSyncRecoversUntrackedEphemeralSandbox reproduces the steady state the
// activator lands in after a watch reconnect drops events (MIR-1198): a pool and a
// RUNNING sandbox exist in the store for an ephemeral version, but the activator
// tracks nothing (WatchIndex never replayed them). The re-sync that watchSandboxes
// now runs on reconnect — recoverPools + recoverSandboxes — must adopt the sandbox
// and seed a.versions so AcquireLease can hand out a lease instead of hanging.
func TestReSyncRecoversUntrackedEphemeralSandbox(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	testVer := newEphemeralVersion(t, server, ctx)

	pool := &compute_v1alpha.SandboxPool{
		Service:              "web",
		SandboxSpec:          compute_v1alpha.SandboxSpec{Version: testVer.ID},
		ReferencedByVersions: []entity.Id{testVer.ID},
		DesiredInstances:     1,
	}
	poolID, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)

	sb := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.RUNNING,
		Spec: compute_v1alpha.SandboxSpec{
			Version: testVer.ID,
			Container: []compute_v1alpha.SandboxSpecContainer{
				{Port: []compute_v1alpha.SandboxSpecContainerPort{{Port: 3000, Name: "http", Type: "http"}}},
			},
		},
		Network: []compute_v1alpha.Network{{Address: "10.0.0.7"}},
	}
	var rpcE entityserver_v1alpha.Entity
	rpcE.SetAttrs(entity.New(
		(&core_v1alpha.Metadata{
			Name:   "eph-sandbox",
			Labels: types.LabelSet("service", "web", "pool", poolID.String()),
		}).Encode,
		entity.Ident, entity.MustKeyword("sandbox/eph-sb"),
		sb.Encode,
	).Attrs())
	_, err = server.EAC.Put(ctx, &rpcE)
	require.NoError(t, err)

	activator := &localActivator{
		log:             log,
		eac:             server.EAC,
		versions:        make(map[verKey]*versionPoolRef),
		poolSandboxes:   make(map[entity.Id]*poolSandboxes),
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	key := verKey{testVer.ID.String(), "web"}
	activator.mu.RLock()
	_, tracked := activator.versions[key]
	activator.mu.RUnlock()
	require.False(t, tracked, "precondition: version must start untracked (as after a dropped watch event)")

	// The re-sync watchSandboxes performs on reconnect.
	require.NoError(t, activator.recoverPools(ctx))
	require.NoError(t, activator.recoverSandboxes(ctx))

	acqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	lease, err := activator.AcquireLease(acqCtx, testVer, "web")
	require.NoError(t, err)
	require.NotNil(t, lease)
	assert.Equal(t, "http://10.0.0.7:3000", lease.URL)
}

// TestWakeAllWaitersNonBlocking verifies wakeAllWaiters signals an empty
// notification channel and does not block on one that is already full (the
// buffered channel a parked waiter registers). This is the mechanism
// resyncFromStore relies on to nudge parked waiters after a reconnect reconcile.
func TestWakeAllWaitersNonBlocking(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	activator := &localActivator{
		log:             log,
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	empty := make(chan struct{}, 1)
	full := make(chan struct{}, 1)
	full <- struct{}{} // pre-fill so a second send would block without the default

	activator.newSandboxChans[verKey{"ver-a", "web"}] = []chan struct{}{empty}
	activator.newSandboxChans[verKey{"ver-b", "web"}] = []chan struct{}{full}

	done := make(chan struct{})
	go func() {
		activator.wakeAllWaiters()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("wakeAllWaiters blocked on a full channel")
	}

	select {
	case <-empty:
	default:
		t.Fatal("wakeAllWaiters did not signal the empty channel")
	}
}

// TestResyncWakesParkedWaiter is the regression test for the parked-waiter gap:
// recoverSandboxes adopts sandboxes by appending to poolSandboxes directly,
// bypassing the per-sandbox notify the watcher does. A request already parked in
// waitForSandbox when a reconnect happens would therefore not see its adopted
// sandbox until the 60s fallback ticker. resyncFromStore now calls wakeAllWaiters
// to nudge it immediately; this test parks a real waiter, then drives the re-sync
// and asserts the lease is delivered far faster than the ticker would allow.
func TestResyncWakesParkedWaiter(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	testVer := newEphemeralVersion(t, server, ctx)

	pool := &compute_v1alpha.SandboxPool{
		Service:              "web",
		SandboxSpec:          compute_v1alpha.SandboxSpec{Version: testVer.ID},
		ReferencedByVersions: []entity.Id{testVer.ID},
		DesiredInstances:     1,
	}
	poolID, err := server.Client.Create(ctx, "test-pool", pool)
	require.NoError(t, err)

	activator := &localActivator{
		log:             log,
		eac:             server.EAC,
		versions:        make(map[verKey]*versionPoolRef),
		poolSandboxes:   make(map[entity.Id]*poolSandboxes),
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
	}

	key := verKey{testVer.ID.String(), "web"}

	// Park a real waiter. The pool exists at cap (desired=1, MaxInstances=1) but no
	// sandbox does yet, so AcquireLease resolves the pool, tracks the version, finds
	// no sandbox, and parks in waitForSandbox.
	type result struct {
		lease *Lease
		err   error
	}
	resCh := make(chan result, 1)
	go func() {
		lease, err := activator.AcquireLease(ctx, testVer, "web")
		resCh <- result{lease, err}
	}()

	// Wait until the waiter has registered its notification channel (i.e. it has
	// parked). The channel is buffered, so a wake delivered after this point lands
	// even if the waiter hasn't reached its select yet.
	require.Eventually(t, func() bool {
		activator.mu.RLock()
		defer activator.mu.RUnlock()
		return len(activator.newSandboxChans[key]) > 0
	}, 2*time.Second, 5*time.Millisecond, "waiter never parked")

	// Now a RUNNING sandbox appears in the store, as it would after the pool scaled
	// up during a watch gap.
	sb := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.RUNNING,
		Spec: compute_v1alpha.SandboxSpec{
			Version: testVer.ID,
			Container: []compute_v1alpha.SandboxSpecContainer{
				{Port: []compute_v1alpha.SandboxSpecContainerPort{{Port: 3000, Name: "http", Type: "http"}}},
			},
		},
		Network: []compute_v1alpha.Network{{Address: "10.0.0.7"}},
	}
	var rpcE entityserver_v1alpha.Entity
	rpcE.SetAttrs(entity.New(
		(&core_v1alpha.Metadata{
			Name:   "eph-sandbox",
			Labels: types.LabelSet("service", "web", "pool", poolID.String()),
		}).Encode,
		entity.Ident, entity.MustKeyword("sandbox/eph-sb"),
		sb.Encode,
	).Attrs())
	_, err = server.EAC.Put(ctx, &rpcE)
	require.NoError(t, err)

	// The reconnect re-sync adopts the sandbox and wakes the parked waiter.
	start := time.Now()
	activator.resyncFromStore(ctx)

	select {
	case res := <-resCh:
		require.NoError(t, res.err)
		require.NotNil(t, res.lease)
		assert.Equal(t, "http://10.0.0.7:3000", res.lease.URL)
		// The fallback ticker is 60s; the wake must deliver far sooner.
		assert.Less(t, time.Since(start), 10*time.Second, "waiter was woken by the wake, not the fallback ticker")
	case <-time.After(15 * time.Second):
		t.Fatal("parked waiter was never woken after re-sync")
	}
}

// TestRequestPoolCapacityRetryRaceNoDoubleUnlock reproduces MIR-1306.
//
// Under activation contention, a goroutine sitting in requestPoolCapacity's
// pool-lookup backoff would notice another goroutine had populated the cache
// and take a bare `continue`, re-entering the attempt loop with a.mu released.
// The next backoff iteration then called a.mu.Unlock() on an already-unlocked
// RWMutex — a fatal, unrecoverable runtime throw that took down the whole
// process (embedded etcd and every app on the node). The trigger is a pool that
// becomes visible in the store *while* concurrent callers are mid-retry, so some
// find it and populate a.pools[key] while others are still backing off and about
// to re-check the cache.
//
// There are no ordinary assertions here: the failure mode is a fatal
// double-unlock that crashes the test binary outright, so simply reaching the
// end of the rounds without the process dying is the pass condition. Requires an
// etcd-backed entity store, so run it inside the dev container:
//
//	./hack/dev-exec go test -run TestRequestPoolCapacityRetryRaceNoDoubleUnlock ./components/activator
//
// Deliberately without -race: the increment path has a separate, pre-existing
// data race (MIR-1308) that the detector trips on and that would drown out this
// test's signal. The double-unlock is a fatal throw, so it surfaces without the
// detector anyway.
func TestRequestPoolCapacityRetryRaceNoDoubleUnlock(t *testing.T) {
	ctx := context.Background()

	server, cleanup := testutils.NewEtcdEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{}
	appID, err := server.Client.Create(ctx, "test-app-race", app)
	require.NoError(t, err)
	app.ID = appID

	// The bug is timing-dependent, so we run several rounds. Pre-fix, the very
	// first round reliably crashes; post-fix, every round drains cleanly.
	const rounds = 10
	const numGoroutines = 40

	for round := range rounds {
		// A fresh version each round means a fresh cache key and a store with no
		// pool for it yet, forcing every caller through the store-lookup retry
		// loop from an empty cache.
		testVer := &core_v1alpha.AppVersion{
			App:      app.ID,
			Version:  fmt.Sprintf("v%d", round),
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
		verID, err := server.Client.Create(ctx, fmt.Sprintf("test-ver-race-%d", round), testVer)
		require.NoError(t, err)
		testVer.ID = verID

		activator := &localActivator{
			log:             testutils.TestLogger(t),
			eac:             server.EAC,
			versions:        make(map[verKey]*versionPoolRef),
			poolSandboxes:   make(map[entity.Id]*poolSandboxes),
			pools:           make(map[verKey]*poolState),
			newSandboxChans: make(map[verKey][]chan struct{}),
		}

		// Launch callers that all miss the cache and enter the store-lookup retry
		// loop. The pool does not exist yet, so their first lookups return nil and
		// they begin backing off.
		done := make(chan error, numGoroutines)
		barrier := make(chan struct{})
		for range numGoroutines {
			go func() {
				<-barrier
				_, err := activator.requestPoolCapacity(ctx, testVer, "web")
				done <- err
			}()
		}
		close(barrier)

		// Let the callers reach their first backoff (100ms), then make the pool
		// visible in the store, mimicking DeploymentLauncher creating it
		// mid-activation. Now retry lookups start finding it: whoever finds it
		// first populates a.pools[key] while others are still mid-backoff and
		// about to re-check the cache — the exact MIR-1306 race window.
		time.Sleep(120 * time.Millisecond)
		racePool := &compute_v1alpha.SandboxPool{
			Service:              "web",
			SandboxSpec:          compute_v1alpha.SandboxSpec{Version: testVer.ID},
			ReferencedByVersions: []entity.Id{testVer.ID},
			DesiredInstances:     0,
			CurrentInstances:     0,
		}
		_, err = server.Client.Create(ctx, fmt.Sprintf("test-pool-race-%d", round), racePool)
		require.NoError(t, err)

		// Drain every caller. We don't require success — depending on scheduling a
		// caller may hit a benign OCC conflict or the not-found terminal path. We
		// only require the process to survive: no fatal double-unlock.
		for range numGoroutines {
			<-done
		}
	}
}

func TestRoutePort(t *testing.T) {
	// No observed bound port: fall back to the configured port.
	assert.Equal(t, int64(3000), routePort(nil, 3000))

	// Observed bound port wins over the configured port (the app ignored $PORT).
	observed := []compute_v1alpha.BoundPort{{Port: 8080}}
	assert.Equal(t, int64(8080), routePort(observed, 3000))

	// A zero observed port is ignored (defensive): keep the configured port.
	assert.Equal(t, int64(3000), routePort([]compute_v1alpha.BoundPort{{Port: 0}}, 3000))
}
