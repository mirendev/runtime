package integration

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	core "miren.dev/runtime/api/core/core_v1alpha"
	storage "miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/controllers/deployment"
	"miren.dev/runtime/controllers/sandboxpool"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
)

// concurrentReconcileEntities lists all entities matching the index and processes
// them concurrently using the given number of worker goroutines, mimicking the
// production ReconcileController's multi-worker dispatch.
func concurrentReconcileEntities(ctx context.Context, h *TestHarness, index entity.Attr,
	rc *controller.ReconcileController, workers int) {

	resp, err := h.EAC.List(ctx, index)
	if err != nil {
		h.T.Errorf("concurrentReconcileEntities: List(%s) failed: %v", index, err)
		return
	}

	values := resp.Values()
	if len(values) == 0 {
		return
	}

	ch := make(chan controller.Event, len(values))
	for _, e := range values {
		ent := e.Entity()
		id := ent.Id()
		if id == "" {
			continue
		}
		ch <- controller.Event{
			Type:   controller.EventUpdated,
			Id:     id,
			Entity: ent,
		}
	}
	close(ch)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for event := range ch {
				if err := rc.ProcessEventForTest(ctx, event); err != nil {
					h.T.Errorf("concurrentReconcileEntities: ProcessEventForTest(%s) failed: %v", event.Id, err)
				}
			}
		}()
	}
	wg.Wait()
}

// concurrentReconcileRound runs all four disk-lifecycle controller types concurrently,
// each with multiple worker goroutines processing their entities.
func concurrentReconcileRound(ctx context.Context, h *TestHarness, workers int) {
	nodeId := compute.NewNodeId(testNodeId).Id()

	type ctrlDef struct {
		index entity.Attr
		rc    *controller.ReconcileController
	}

	controllers := []ctrlDef{
		{entity.Ref(entity.EntityKind, storage.KindDisk), h.DiskRC},
		{entity.Ref(storage.DiskVolumeNodeIdId, nodeId), h.DiskVolRC},
		{entity.Ref(storage.DiskMountNodeIdId, nodeId), h.DiskMntRC},
		{entity.Ref(entity.EntityKind, storage.KindDiskLease), h.DiskLeaseRC},
	}

	var wg sync.WaitGroup
	for _, c := range controllers {
		wg.Add(1)
		go func(idx entity.Attr, rc *controller.ReconcileController) {
			defer wg.Done()
			concurrentReconcileEntities(ctx, h, idx, rc, workers)
		}(c.index, c.rc)
	}
	wg.Wait()
}

// createAppEntity creates an App entity in the store.
func createAppEntity(t *testing.T, ctx context.Context, h *TestHarness, appID entity.Id, appName string) {
	t.Helper()
	app := &core.App{}
	_, err := h.EAC.Create(ctx, entity.New(
		entity.DBId, appID,
		(&core.Metadata{
			Name: appName,
		}).Encode,
		app.Encode,
	).Attrs())
	require.NoError(t, err, "createAppEntity(%s)", appID)
}

// createAppVersion creates an AppVersion entity with a service that has a disk attached.
func createAppVersion(t *testing.T, ctx context.Context, h *TestHarness,
	verID entity.Id, appID entity.Id, version string, image string,
	diskName string, diskSizeGB int64,
) {
	t.Helper()
	ver := &core.AppVersion{
		App:      appID,
		Version:  version,
		ImageUrl: image,
		Config: core.Config{
			Services: []core.Services{
				{
					Name: "web",
					Disks: []core.Disks{
						{
							Name:      diskName,
							MountPath: "/data",
							SizeGb:    diskSizeGB,
						},
					},
					ServiceConcurrency: core.ServiceConcurrency{
						Mode:         "fixed",
						NumInstances: 1,
					},
				},
			},
			Commands: []core.Commands{
				{
					Service: "web",
					Command: "start",
				},
			},
		},
	}
	_, err := h.EAC.Create(ctx, entity.New(
		entity.DBId, verID,
		ver.Encode,
	).Attrs())
	require.NoError(t, err, "createAppVersion(%s)", verID)
}

// setActiveVersion patches an App entity to set its ActiveVersion.
func setActiveVersion(t *testing.T, ctx context.Context, h *TestHarness, appID entity.Id, verID entity.Id) {
	t.Helper()
	_, err := h.EAC.Patch(ctx, entity.New(
		entity.DBId, appID,
		(&core.App{
			ActiveVersion: verID,
		}).Encode,
	).Attrs(), 0)
	require.NoError(t, err, "setActiveVersion(%s -> %s)", appID, verID)
}

// reconcileLauncher calls Launcher.Reconcile for the given app.
func reconcileLauncher(t *testing.T, ctx context.Context, launcher *deployment.Launcher, appID entity.Id) {
	t.Helper()
	app := &core.App{ID: appID}
	err := launcher.Reconcile(ctx, app, nil)
	require.NoError(t, err, "reconcileLauncher(%s)", appID)
}

// reconcileAllPools lists all SandboxPool entities and reconciles each through the Manager.
func reconcileAllPools(t *testing.T, ctx context.Context, h *TestHarness, mgr *sandboxpool.Manager) {
	t.Helper()
	resp, err := h.EAC.List(ctx, entity.Ref(entity.EntityKind, compute.KindSandboxPool))
	require.NoError(t, err, "listing pools")

	for _, e := range resp.Values() {
		var pool compute.SandboxPool
		pool.Decode(e.Entity())
		if err := mgr.Reconcile(ctx, &pool, nil); err != nil {
			t.Errorf("reconcileAllPools: Reconcile(%s) failed: %v", pool.ID, err)
		}
	}
}

// listSandboxes returns all Sandbox entities in the store.
func listAllSandboxes(t *testing.T, ctx context.Context, h *TestHarness) []*compute.Sandbox {
	t.Helper()
	resp, err := h.EAC.List(ctx, entity.Ref(entity.EntityKind, compute.KindSandbox))
	require.NoError(t, err, "listAllSandboxes")

	var sandboxes []*compute.Sandbox
	for _, e := range resp.Values() {
		var sb compute.Sandbox
		sb.Decode(e.Entity())
		sbCopy := sb
		sandboxes = append(sandboxes, &sbCopy)
	}
	return sandboxes
}

// listPools returns all SandboxPool entities in the store.
func listPools(t *testing.T, ctx context.Context, h *TestHarness) []*compute.SandboxPool {
	t.Helper()
	resp, err := h.EAC.List(ctx, entity.Ref(entity.EntityKind, compute.KindSandboxPool))
	require.NoError(t, err, "listPools")

	var pools []*compute.SandboxPool
	for _, e := range resp.Values() {
		var pool compute.SandboxPool
		pool.Decode(e.Entity())
		poolCopy := pool
		pools = append(pools, &poolCopy)
	}
	return pools
}

// retireOldSandboxes finds sandboxes in pools with DesiredInstances==0,
// releases their disk leases, and marks them STOPPED. This matches the
// production flow: Manager.scaleDown marks RUNNING→STOPPED, then
// SandboxController releases leases and transitions STOPPED→DEAD.
// We mark STOPPED (not DEAD) so the Manager's countQuickCrashes doesn't
// misinterpret intentional retirements as crash-loops.
func retireOldSandboxes(t *testing.T, ctx context.Context, h *TestHarness) {
	t.Helper()

	// Build a set of pool IDs that are scaled to 0
	scaledDownPools := make(map[string]bool)
	pools := listPools(t, ctx, h)
	for _, pool := range pools {
		if pool.DesiredInstances == 0 {
			scaledDownPools[pool.ID.String()] = true
		}
	}

	if len(scaledDownPools) == 0 {
		return
	}

	// Find sandboxes belonging to scaled-down pools
	resp, err := h.EAC.List(ctx, entity.Ref(entity.EntityKind, compute.KindSandbox))
	require.NoError(t, err)

	for _, e := range resp.Values() {
		var sb compute.Sandbox
		sb.Decode(e.Entity())

		// Skip non-active sandboxes
		if sb.Status != compute.RUNNING && sb.Status != compute.PENDING {
			continue
		}

		var md core.Metadata
		md.Decode(e.Entity())
		poolLabel, _ := md.Labels.Get("pool")

		if !scaledDownPools[poolLabel] {
			continue
		}

		// Release disk leases and mark STOPPED (not DEAD — matches scaleDown)
		_ = h.FakeSandbox.ReleaseDiskLeases(ctx, sb.ID)
		markSandboxStopped(t, ctx, h, sb.ID)
	}
}

// finalizeStoppedSandboxes transitions all STOPPED sandboxes to DEAD.
// In production the SandboxController does this after cleanup; we call it
// during convergence to complete the lifecycle.
func finalizeStoppedSandboxes(t *testing.T, ctx context.Context, h *TestHarness) {
	t.Helper()

	sandboxes := listAllSandboxes(t, ctx, h)
	for _, sb := range sandboxes {
		if sb.Status == compute.STOPPED {
			markSandboxDead(t, ctx, h, sb.ID)
		}
	}
}

// bootPendingSandboxes finds PENDING sandboxes with disk volumes and
// acquires disk leases for them, simulating what the real SandboxController does.
func bootPendingSandboxes(t *testing.T, ctx context.Context, h *TestHarness, appID entity.Id) {
	t.Helper()

	sandboxes := listAllSandboxes(t, ctx, h)
	for _, sb := range sandboxes {
		if sb.Status != compute.PENDING {
			continue
		}

		// Process disk volumes from the sandbox's spec
		for _, vol := range sb.Spec.Volume {
			if vol.Provider != "miren" {
				continue
			}

			diskID, err := h.FakeSandbox.EnsureDisk(ctx, vol.DiskName, vol.SizeGb, vol.Filesystem)
			if err != nil {
				t.Logf("bootPendingSandboxes: EnsureDisk(%s) failed: %v", vol.DiskName, err)
				continue
			}

			_, err = h.FakeSandbox.AcquireDiskLease(ctx, diskID, sb.ID, appID, vol.MountPath, vol.ReadOnly)
			if err != nil {
				t.Logf("bootPendingSandboxes: AcquireDiskLease(%s, %s) failed: %v", diskID, sb.ID, err)
				continue
			}
		}
	}
}

// markPendingSandboxesRunning marks PENDING sandboxes as RUNNING if all their
// disk leases are BOUND. This simulates the sandbox boot completing.
func markPendingSandboxesRunning(t *testing.T, ctx context.Context, h *TestHarness) {
	t.Helper()

	sandboxes := listAllSandboxes(t, ctx, h)
	for _, sb := range sandboxes {
		if sb.Status != compute.PENDING {
			continue
		}

		// Check if all disk leases for this sandbox are BOUND
		sbLeases := leasesForSandbox(t, ctx, h, sb.ID)
		if len(sbLeases) == 0 {
			// No disks — sandbox with no volumes can be marked running
			if len(sb.Spec.Volume) == 0 {
				markSandboxRunning(t, ctx, h, sb.ID)
			}
			continue
		}

		allBound := true
		for _, l := range sbLeases {
			if l.Status != storage.BOUND {
				allBound = false
				break
			}
		}

		if allBound {
			markSandboxRunning(t, ctx, h, sb.ID)
		}
	}
}

func TestRapidRedeployWithDisk(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	const (
		numRedeploys = 10
		concWorkers  = 3
		diskName     = "redeploy-disk"
		diskSizeGB   = 10
	)

	appID := entity.Id("app/redeploy-test")
	appName := "redeploy-test"

	// Create Launcher and Manager (the real deployment machinery)
	launcher := deployment.NewLauncher(h.Log, h.EAC)
	launcher.PoolReadyTimeout = 100 * time.Millisecond
	launcher.Init(ctx) //nolint:errcheck

	mgr := sandboxpool.NewManager(h.Log, h.EAC)
	// Don't call mgr.Init — it starts a background scale-down goroutine we don't need.

	// Create App entity
	createAppEntity(t, ctx, h, appID, appName)

	// Phase 1: Deploy v1 through the real Launcher + Manager pipeline
	ver1ID := entity.Id("app-version/redeploy-test-v1")
	createAppVersion(t, ctx, h, ver1ID, appID, "v1", "test:v1", diskName, diskSizeGB)
	setActiveVersion(t, ctx, h, appID, ver1ID)

	// Launcher creates pool
	reconcileLauncher(t, ctx, launcher, appID)

	// Manager creates sandbox
	reconcileAllPools(t, ctx, h, mgr)

	// FakeSandbox acquires disk leases for PENDING sandboxes
	bootPendingSandboxes(t, ctx, h, appID)

	// Disk controllers converge: disk provisions, volume ready, mount mounted, lease BOUND
	h.ReconcileAll(ctx, 20)

	// Mark sandboxes running
	markPendingSandboxesRunning(t, ctx, h)

	// Verify Phase 1 state
	allLeases := listLeases(t, ctx, h)
	require.Len(t, allLeases, 1, "v1: should have 1 lease")
	require.Equal(t, storage.BOUND, allLeases[0].Status, "v1 lease should be BOUND")
	assert.Equal(t, 1, countMountedMounts(t, ctx, h), "v1: should have 1 mounted mount")

	v1Sandboxes := listAllSandboxes(t, ctx, h)
	require.Len(t, v1Sandboxes, 1, "v1: should have 1 sandbox")
	assert.Equal(t, compute.RUNNING, v1Sandboxes[0].Status, "v1 sandbox should be RUNNING")

	t.Log("Phase 1 complete: v1 deployed and running with BOUND lease")

	// Phase 2: Rapid redeployments v2 through v(N+1)
	// Each version uses a different image so the Launcher creates a new pool
	// (specsMatch compares images, different image = new pool).
	for v := 2; v <= numRedeploys+1; v++ {
		version := fmt.Sprintf("v%d", v)
		verID := entity.Id(fmt.Sprintf("app-version/redeploy-test-%s", version))
		image := fmt.Sprintf("test:%s", version)

		// Create new AppVersion and set as active
		createAppVersion(t, ctx, h, verID, appID, version, image, diskName, diskSizeGB)
		setActiveVersion(t, ctx, h, appID, verID)

		// Launcher reconcile: creates new pool, scales old pool(s) to 0
		reconcileLauncher(t, ctx, launcher, appID)

		// Retire sandboxes in scaled-down pools (release leases + mark DEAD)
		retireOldSandboxes(t, ctx, h)

		// Manager reconcile: creates sandbox for the new pool
		reconcileAllPools(t, ctx, h, mgr)

		// Run 0-2 concurrent disk controller reconciliation rounds per iteration
		// to create varying interleaving patterns
		reconcileRounds := v % 3
		for r := 0; r < reconcileRounds; r++ {
			concurrentReconcileRound(ctx, h, concWorkers)
		}

		// Boot pending sandboxes (acquire disk leases)
		bootPendingSandboxes(t, ctx, h, appID)

		t.Logf("  %s: created version, launched pool+sandbox (%d concurrent rounds)", version, reconcileRounds)
	}

	t.Logf("Phase 2 complete: %d rapid redeployments done", numRedeploys)

	// Phase 3: Convergence
	// First, finalize stopped sandboxes → DEAD (simulates SandboxController cleanup)
	finalizeStoppedSandboxes(t, ctx, h)

	// Concurrent disk controller rounds
	for i := 0; i < 5; i++ {
		concurrentReconcileRound(ctx, h, concWorkers)
	}
	h.ReconcileAll(ctx, 30)

	// Mark any remaining PENDING sandboxes as RUNNING if their leases are BOUND
	markPendingSandboxesRunning(t, ctx, h)

	// One more convergence pass now that sandboxes are RUNNING
	h.ReconcileAll(ctx, 10)

	t.Log("Phase 3 complete: convergence rounds done")

	// Phase 4: Invariant validation
	finalLeases := listLeases(t, ctx, h)
	finalMounts := listDiskMounts(t, ctx, h)
	finalSandboxes := listAllSandboxes(t, ctx, h)
	finalPools := listPools(t, ctx, h)

	// 4a: Exactly 1 BOUND lease exists (exclusive disk access)
	boundCount := 0
	var boundLease *storage.DiskLease
	for _, l := range finalLeases {
		if l.Status == storage.BOUND {
			boundCount++
			boundLease = l
			t.Logf("  BOUND lease: %s (sandbox=%s, disk=%s)", l.ID, l.SandboxId, l.DiskId)
		}
	}
	assert.Equal(t, 1, boundCount, "should have exactly 1 BOUND lease, got %d", boundCount)

	// 4b: BOUND lease has exactly 1 MNT_MOUNTED mount
	if boundLease != nil {
		latestMount := getMountForLease(t, ctx, h, boundLease.ID)
		if assert.NotNil(t, latestMount, "BOUND lease should have a mount") {
			assert.Equal(t, storage.DM_MOUNTED, latestMount.ActualState,
				"BOUND lease mount should be MNT_MOUNTED, got %s", latestMount.ActualState)
		}
	}

	// 4c: All non-BOUND leases are RELEASED or FAILED
	for _, l := range finalLeases {
		if boundLease != nil && l.ID == boundLease.ID {
			continue
		}
		assert.True(t, l.Status == storage.RELEASED || l.Status == storage.FAILED,
			"non-BOUND lease %s should be RELEASED or FAILED, got %s (sandbox=%s)",
			l.ID, l.Status, l.SandboxId)
	}

	// 4d: Exactly 1 MNT_MOUNTED mount exists (no orphans)
	mountedCount := 0
	for _, m := range finalMounts {
		if m.ActualState == storage.DM_MOUNTED {
			mountedCount++
		}
	}
	assert.Equal(t, 1, mountedCount, "should have exactly 1 MNT_MOUNTED mount, got %d", mountedCount)

	// 4e: Disk is still PROVISIONED
	disks := listDisks(t, ctx, h)
	require.Len(t, disks, 1, "should have exactly 1 disk")
	assert.Equal(t, storage.PROVISIONED, disks[0].Status,
		"disk should still be PROVISIONED after all redeployments")

	// 4f: Exactly 1 active pool with DesiredInstances > 0
	activePoolCount := 0
	for _, pool := range finalPools {
		if pool.DesiredInstances > 0 {
			activePoolCount++
		}
	}
	assert.Equal(t, 1, activePoolCount,
		"should have exactly 1 active pool, got %d (total pools: %d)", activePoolCount, len(finalPools))

	// 4g: Exactly 1 RUNNING sandbox
	runningCount := 0
	for _, sb := range finalSandboxes {
		if sb.Status == compute.RUNNING {
			runningCount++
		}
	}
	assert.Equal(t, 1, runningCount,
		"should have exactly 1 RUNNING sandbox, got %d", runningCount)

	// Summary
	t.Logf("Final state: %d leases, %d mounts, %d sandboxes, %d pools | "+
		"%d BOUND, %d MNT_MOUNTED, %d RUNNING",
		len(finalLeases), len(finalMounts), len(finalSandboxes), len(finalPools),
		boundCount, mountedCount, runningCount)
}
