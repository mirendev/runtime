package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	storage "miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
)

// bootSandboxWithDisk creates a sandbox entity, ensures a disk, acquires a lease,
// then runs ReconcileAll until the lease becomes BOUND. Returns all created IDs.
func bootSandboxWithDisk(t *testing.T, ctx context.Context, h *TestHarness, sandboxID entity.Id, diskName string, sizeGB int64) (diskID, leaseID entity.Id) {
	t.Helper()

	// Create sandbox entity
	createSandboxEntity(t, ctx, h, sandboxID, compute.PENDING)

	// Ensure disk exists
	var err error
	diskID, err = h.FakeSandbox.EnsureDisk(ctx, diskName, sizeGB, "ext4")
	require.NoError(t, err, "EnsureDisk failed")

	// Acquire lease
	leaseID, err = h.FakeSandbox.AcquireDiskLease(ctx, diskID, sandboxID, "", "/data", false)
	require.NoError(t, err, "AcquireDiskLease failed")

	// ReconcileAll until the system converges
	h.ReconcileAll(ctx, 20)

	// Mark sandbox running once lease is bound
	lease := getLease(t, ctx, h, leaseID)
	if lease.Status == storage.BOUND {
		markSandboxRunning(t, ctx, h, sandboxID)
	}

	return diskID, leaseID
}

// stopSandbox releases disk leases for a sandbox and marks it DEAD.
func stopSandbox(t *testing.T, ctx context.Context, h *TestHarness, sandboxID entity.Id) {
	t.Helper()

	err := h.FakeSandbox.ReleaseDiskLeases(ctx, sandboxID)
	require.NoError(t, err)

	markSandboxDead(t, ctx, h, sandboxID)
}

func TestSandboxStartWithDisk(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	sandboxID := entity.Id("sandbox/test-start-1")
	diskID, leaseID := bootSandboxWithDisk(t, ctx, h, sandboxID, "test-disk", 10)

	// Verify final state
	disk := getDisk(t, ctx, h, diskID)
	assert.Equal(t, storage.PROVISIONED, disk.Status, "disk should be PROVISIONED")
	assert.NotEmpty(t, disk.VolumeId, "disk should have a VolumeId")

	lease := getLease(t, ctx, h, leaseID)
	assert.Equal(t, storage.BOUND, lease.Status, "lease should be BOUND")
	assert.Equal(t, diskID, lease.DiskId)
	assert.Equal(t, sandboxID, lease.SandboxId)

	// Verify disk_volume entities
	vols := listDiskVolumes(t, ctx, h)
	assert.Len(t, vols, 1, "should have exactly 1 disk_volume")
	assert.Equal(t, storage.DV_READY, vols[0].ActualState)

	mounts := listDiskMounts(t, ctx, h)
	assert.Len(t, mounts, 1, "should have exactly 1 disk_mount")
	assert.Equal(t, storage.DM_MOUNTED, mounts[0].ActualState)

	sb := getSandbox(t, ctx, h, sandboxID)
	assert.Equal(t, compute.RUNNING, sb.Status)
}

func TestSandboxStopReleasesLease(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	sandboxID := entity.Id("sandbox/test-stop-1")
	diskID, leaseID := bootSandboxWithDisk(t, ctx, h, sandboxID, "test-disk-stop", 10)

	// Verify lease is bound before stop
	lease := getLease(t, ctx, h, leaseID)
	require.Equal(t, storage.BOUND, lease.Status)

	// Stop the sandbox
	stopSandbox(t, ctx, h, sandboxID)

	// Verify lease is RELEASED
	lease = getLease(t, ctx, h, leaseID)
	assert.Equal(t, storage.RELEASED, lease.Status)

	// ReconcileAll to process the release
	h.ReconcileAll(ctx, 20)

	// Verify disk is still PROVISIONED (not deleted)
	disk := getDisk(t, ctx, h, diskID)
	assert.Equal(t, storage.PROVISIONED, disk.Status)

	// Verify the disk_mount has been unmounted
	mounts := listDiskMounts(t, ctx, h)
	for _, m := range mounts {
		// Mount should be detached or deleted
		assert.True(t,
			m.ActualState == storage.DM_DETACHED ||
				m.DesiredState == storage.DM_WANT_UNMOUNTED,
			"mount should be detached or want unmounted, got actual=%s desired=%s",
			m.ActualState, m.DesiredState)
	}
}

func TestSandboxReplacementAcquiresDisk(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	// Boot first sandbox
	sandbox1 := entity.Id("sandbox/replace-1")
	diskID, lease1ID := bootSandboxWithDisk(t, ctx, h, sandbox1, "test-disk-replace", 10)

	// Verify first sandbox is running with bound lease
	lease1 := getLease(t, ctx, h, lease1ID)
	require.Equal(t, storage.BOUND, lease1.Status)

	// Stop first sandbox (releases lease)
	stopSandbox(t, ctx, h, sandbox1)

	// ReconcileAll to process the release
	h.ReconcileAll(ctx, 20)

	// Boot replacement sandbox with the same disk
	sandbox2 := entity.Id("sandbox/replace-2")
	createSandboxEntity(t, ctx, h, sandbox2, compute.PENDING)

	// Acquire lease for same disk (should succeed since old lease is released)
	lease2ID, err := h.FakeSandbox.AcquireDiskLease(ctx, diskID, sandbox2, "", "/data", false)
	require.NoError(t, err, "replacement should acquire disk")

	// ReconcileAll until new lease binds
	h.ReconcileAll(ctx, 20)

	// Verify new lease is BOUND
	lease2 := getLease(t, ctx, h, lease2ID)
	assert.Equal(t, storage.BOUND, lease2.Status, "replacement lease should be BOUND")
	assert.Equal(t, sandbox2, lease2.SandboxId)

	// Mark sandbox2 running
	markSandboxRunning(t, ctx, h, sandbox2)

	// Verify old lease is still RELEASED
	lease1 = getLease(t, ctx, h, lease1ID)
	assert.Equal(t, storage.RELEASED, lease1.Status)
}

func TestSandboxCrashAndReplace(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	// Boot first sandbox
	sandbox1 := entity.Id("sandbox/crash-1")
	diskID, lease1ID := bootSandboxWithDisk(t, ctx, h, sandbox1, "test-disk-crash", 10)

	lease1 := getLease(t, ctx, h, lease1ID)
	require.Equal(t, storage.BOUND, lease1.Status)

	// Simulate crash: mark sandbox DEAD directly, then release leases
	// (mimics the real flow where the sandbox controller detects the crash)
	markSandboxDead(t, ctx, h, sandbox1)
	err := h.FakeSandbox.ReleaseDiskLeases(ctx, sandbox1)
	require.NoError(t, err)

	h.ReconcileAll(ctx, 20)

	// Boot replacement
	sandbox2 := entity.Id("sandbox/crash-2")
	createSandboxEntity(t, ctx, h, sandbox2, compute.PENDING)

	lease2ID, err := h.FakeSandbox.AcquireDiskLease(ctx, diskID, sandbox2, "", "/data", false)
	require.NoError(t, err)

	h.ReconcileAll(ctx, 20)

	lease2 := getLease(t, ctx, h, lease2ID)
	assert.Equal(t, storage.BOUND, lease2.Status, "crash replacement lease should be BOUND")
	assert.Equal(t, sandbox2, lease2.SandboxId)
}

func TestMultipleSandboxesDifferentDisks(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	// Boot two sandboxes each with their own disk
	sandbox1 := entity.Id("sandbox/multi-1")
	disk1ID, lease1ID := bootSandboxWithDisk(t, ctx, h, sandbox1, "disk-alpha", 10)

	sandbox2 := entity.Id("sandbox/multi-2")
	disk2ID, lease2ID := bootSandboxWithDisk(t, ctx, h, sandbox2, "disk-beta", 20)

	// Verify both are running with bound leases
	lease1 := getLease(t, ctx, h, lease1ID)
	assert.Equal(t, storage.BOUND, lease1.Status)
	lease2 := getLease(t, ctx, h, lease2ID)
	assert.Equal(t, storage.BOUND, lease2.Status)

	// Disks should be different
	assert.NotEqual(t, disk1ID, disk2ID)

	// Stop one sandbox
	stopSandbox(t, ctx, h, sandbox1)
	h.ReconcileAll(ctx, 20)

	// The other sandbox should be unaffected
	lease2After := getLease(t, ctx, h, lease2ID)
	assert.Equal(t, storage.BOUND, lease2After.Status, "surviving sandbox's lease should still be BOUND")

	sb2 := getSandbox(t, ctx, h, sandbox2)
	assert.Equal(t, compute.RUNNING, sb2.Status, "surviving sandbox should still be RUNNING")
}

func TestDiskProvisioningRace(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	sandboxID := entity.Id("sandbox/race-1")
	createSandboxEntity(t, ctx, h, sandboxID, compute.PENDING)

	// Create disk and lease
	diskID, err := h.FakeSandbox.EnsureDisk(ctx, "test-disk-race", 10, "ext4")
	require.NoError(t, err)

	leaseID, err := h.FakeSandbox.AcquireDiskLease(ctx, diskID, sandboxID, "", "/data", false)
	require.NoError(t, err)

	// Only reconcile disk (not disk volumes yet) — disk should still be PROVISIONING
	h.reconcileKind(ctx, storage.KindDisk, h.DiskRC)

	disk := getDisk(t, ctx, h, diskID)
	// After one reconcile the disk creates the disk_volume but stays PROVISIONING
	assert.Equal(t, storage.PROVISIONING, disk.Status, "disk should still be PROVISIONING before volume is ready")

	// Lease should still be PENDING because disk isn't provisioned yet
	h.reconcileKind(ctx, storage.KindDiskLease, h.DiskLeaseRC)
	lease := getLease(t, ctx, h, leaseID)
	assert.Equal(t, storage.PENDING, lease.Status, "lease should be PENDING while disk provisions")

	// Now run full reconciliation to completion
	h.ReconcileAll(ctx, 20)

	// Verify everything converges
	disk = getDisk(t, ctx, h, diskID)
	assert.Equal(t, storage.PROVISIONED, disk.Status)

	lease = getLease(t, ctx, h, leaseID)
	assert.Equal(t, storage.BOUND, lease.Status)
}

func TestRapidStopAndRestart(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	// Boot sandbox A with disk
	sandboxA := entity.Id("sandbox/rapid-a")
	diskID, leaseAID := bootSandboxWithDisk(t, ctx, h, sandboxA, "test-disk-rapid", 10)

	leaseA := getLease(t, ctx, h, leaseAID)
	require.Equal(t, storage.BOUND, leaseA.Status)

	// Release A's leases and mark DEAD (but do NOT ReconcileAll yet)
	err := h.FakeSandbox.ReleaseDiskLeases(ctx, sandboxA)
	require.NoError(t, err)
	markSandboxDead(t, ctx, h, sandboxA)

	// Reconcile only the disk-lease controller for A's lease
	// This processes handleReleasedLease which removes from activeLeases
	// and sets mount to MNT_WANT_UNMOUNTED, but does NOT unmount yet.
	h.reconcileKind(ctx, storage.KindDiskLease, h.DiskLeaseRC)

	// Verify A's lease is RELEASED and mount is marked for unmount
	leaseA = getLease(t, ctx, h, leaseAID)
	assert.Equal(t, storage.RELEASED, leaseA.Status)

	// Create sandbox B and acquire the same disk (should succeed since activeLeases cleared)
	sandboxB := entity.Id("sandbox/rapid-b")
	createSandboxEntity(t, ctx, h, sandboxB, compute.PENDING)

	leaseBID, err := h.FakeSandbox.AcquireDiskLease(ctx, diskID, sandboxB, "", "/data", false)
	require.NoError(t, err, "B should acquire disk while A's mount is still unwinding")

	// Now ReconcileAll to converge everything:
	// A's mount unmounts, B's mount mounts, B's lease becomes BOUND
	h.ReconcileAll(ctx, 20)

	// Verify final state
	leaseA = getLease(t, ctx, h, leaseAID)
	assert.Equal(t, storage.RELEASED, leaseA.Status, "A's lease should stay RELEASED")

	leaseB := getLease(t, ctx, h, leaseBID)
	assert.Equal(t, storage.BOUND, leaseB.Status, "B's lease should be BOUND")
	assert.Equal(t, sandboxB, leaseB.SandboxId)

	// Exactly 1 mounted disk_mount should exist (B's)
	assert.Equal(t, 1, countMountedMounts(t, ctx, h), "should have exactly 1 mounted disk_mount")
}

func TestAcquireBlockedByBoundLease(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	// Boot sandbox A with disk
	sandboxA := entity.Id("sandbox/block-a")
	diskID, leaseAID := bootSandboxWithDisk(t, ctx, h, sandboxA, "test-disk-block", 10)

	leaseA := getLease(t, ctx, h, leaseAID)
	require.Equal(t, storage.BOUND, leaseA.Status)

	// Create sandbox B and try to acquire the same disk — should fail
	sandboxB := entity.Id("sandbox/block-b")
	createSandboxEntity(t, ctx, h, sandboxB, compute.PENDING)

	_, err := h.FakeSandbox.AcquireDiskLease(ctx, diskID, sandboxB, "", "/data", false)
	require.Error(t, err, "AcquireDiskLease should fail when BOUND lease exists")
	assert.Contains(t, err.Error(), "active lease")

	// Stop sandbox A (release leases)
	stopSandbox(t, ctx, h, sandboxA)
	h.ReconcileAll(ctx, 20)

	// Retry — should succeed now
	leaseBID, err := h.FakeSandbox.AcquireDiskLease(ctx, diskID, sandboxB, "", "/data", false)
	require.NoError(t, err, "B should acquire disk after A released")

	h.ReconcileAll(ctx, 20)

	leaseB := getLease(t, ctx, h, leaseBID)
	assert.Equal(t, storage.BOUND, leaseB.Status, "B's lease should be BOUND")

	leaseA = getLease(t, ctx, h, leaseAID)
	assert.Equal(t, storage.RELEASED, leaseA.Status, "A's lease should be RELEASED")
}

func TestCrashWithoutReleaseBeforeReplace(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	// Boot sandbox A with disk
	sandboxA := entity.Id("sandbox/crash-nr-a")
	diskID, leaseAID := bootSandboxWithDisk(t, ctx, h, sandboxA, "test-disk-crash-nr", 10)

	leaseA := getLease(t, ctx, h, leaseAID)
	require.Equal(t, storage.BOUND, leaseA.Status)

	// Simulate crash: mark A DEAD directly, do NOT release leases
	markSandboxDead(t, ctx, h, sandboxA)

	// Create sandbox B, try to acquire — should fail (lease still BOUND for A)
	sandboxB := entity.Id("sandbox/crash-nr-b")
	createSandboxEntity(t, ctx, h, sandboxB, compute.PENDING)

	_, err := h.FakeSandbox.AcquireDiskLease(ctx, diskID, sandboxB, "", "/data", false)
	require.Error(t, err, "AcquireDiskLease should fail when unreleased BOUND lease exists")
	assert.Contains(t, err.Error(), "active lease")

	// Now simulate delayed cleanup: release A's leases
	err = h.FakeSandbox.ReleaseDiskLeases(ctx, sandboxA)
	require.NoError(t, err)
	h.ReconcileAll(ctx, 20)

	// Retry — should succeed now
	leaseBID, err := h.FakeSandbox.AcquireDiskLease(ctx, diskID, sandboxB, "", "/data", false)
	require.NoError(t, err, "B should acquire disk after delayed cleanup")

	h.ReconcileAll(ctx, 20)

	leaseB := getLease(t, ctx, h, leaseBID)
	assert.Equal(t, storage.BOUND, leaseB.Status, "B's lease should be BOUND")
	assert.Equal(t, sandboxB, leaseB.SandboxId)

	leaseA = getLease(t, ctx, h, leaseAID)
	assert.Equal(t, storage.RELEASED, leaseA.Status, "A's lease should be RELEASED")
}

func TestDeleteSandboxEntityOrphanedLease(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	// Boot sandbox A with disk
	sandboxA := entity.Id("sandbox/orphan-a")
	diskID, leaseAID := bootSandboxWithDisk(t, ctx, h, sandboxA, "test-disk-orphan", 10)

	leaseA := getLease(t, ctx, h, leaseAID)
	require.Equal(t, storage.BOUND, leaseA.Status)

	// Delete the sandbox entity (NOT stop, NOT release leases)
	deleteSandboxEntity(t, ctx, h, sandboxA)

	// Lease should still be BOUND (orphaned)
	leaseA = getLease(t, ctx, h, leaseAID)
	assert.Equal(t, storage.BOUND, leaseA.Status, "orphaned lease should still be BOUND")

	// Sandbox B can't acquire the disk — orphaned BOUND lease blocks it
	sandboxB := entity.Id("sandbox/orphan-b")
	createSandboxEntity(t, ctx, h, sandboxB, compute.PENDING)

	_, err := h.FakeSandbox.AcquireDiskLease(ctx, diskID, sandboxB, "", "/data", false)
	require.Error(t, err, "AcquireDiskLease should fail with orphaned BOUND lease")

	// Manually patch the orphaned lease to RELEASED
	patchLeaseStatus(t, ctx, h, leaseAID, storage.RELEASED)
	h.ReconcileAll(ctx, 20)

	// Now B can acquire the disk
	leaseBID, err := h.FakeSandbox.AcquireDiskLease(ctx, diskID, sandboxB, "", "/data", false)
	require.NoError(t, err, "B should acquire disk after orphaned lease released")

	h.ReconcileAll(ctx, 20)

	leaseB := getLease(t, ctx, h, leaseBID)
	assert.Equal(t, storage.BOUND, leaseB.Status, "B's lease should be BOUND")

	leaseA = getLease(t, ctx, h, leaseAID)
	assert.Equal(t, storage.RELEASED, leaseA.Status, "orphaned lease should be RELEASED")
}

func TestLeaseReleaseIdempotent(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	// Boot sandbox A with disk
	sandboxA := entity.Id("sandbox/idempotent-a")
	_, leaseAID := bootSandboxWithDisk(t, ctx, h, sandboxA, "test-disk-idempotent", 10)

	leaseA := getLease(t, ctx, h, leaseAID)
	require.Equal(t, storage.BOUND, leaseA.Status)

	// Release leases — first time
	err := h.FakeSandbox.ReleaseDiskLeases(ctx, sandboxA)
	require.NoError(t, err)

	leaseA = getLease(t, ctx, h, leaseAID)
	assert.Equal(t, storage.RELEASED, leaseA.Status)

	// Release leases — second time (should be a no-op)
	err = h.FakeSandbox.ReleaseDiskLeases(ctx, sandboxA)
	require.NoError(t, err)

	leaseA = getLease(t, ctx, h, leaseAID)
	assert.Equal(t, storage.RELEASED, leaseA.Status, "lease should still be RELEASED after double release")

	// ReconcileAll should converge normally
	h.ReconcileAll(ctx, 20)

	leaseA = getLease(t, ctx, h, leaseAID)
	assert.Equal(t, storage.RELEASED, leaseA.Status, "lease should remain RELEASED")
}

func TestDeleteLeaseEntity(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	// Boot sandbox A with disk
	sandboxA := entity.Id("sandbox/dellease-a")
	diskID, leaseAID := bootSandboxWithDisk(t, ctx, h, sandboxA, "test-disk-dellease", 10)

	leaseA := getLease(t, ctx, h, leaseAID)
	require.Equal(t, storage.BOUND, leaseA.Status)

	// Verify mount exists and is mounted
	assert.Equal(t, 1, countMountedMounts(t, ctx, h))

	// Delete the lease entity from the store
	deleteLeaseEntity(t, ctx, h, leaseAID)

	// Fire a delete event to the DiskLeaseController so it processes cleanup
	deleteEvent := controller.Event{
		Type: controller.EventDeleted,
		Id:   leaseAID,
	}
	err := h.DiskLeaseRC.ProcessEventForTest(ctx, deleteEvent)
	require.NoError(t, err)

	// ReconcileAll to process mount cleanup
	h.ReconcileAll(ctx, 20)

	// Disk should still be PROVISIONED
	disk := getDisk(t, ctx, h, diskID)
	assert.Equal(t, storage.PROVISIONED, disk.Status, "disk should still be PROVISIONED")

	// New sandbox B should be able to acquire the disk
	sandboxB := entity.Id("sandbox/dellease-b")
	createSandboxEntity(t, ctx, h, sandboxB, compute.PENDING)

	leaseBID, err := h.FakeSandbox.AcquireDiskLease(ctx, diskID, sandboxB, "", "/data", false)
	require.NoError(t, err, "B should acquire disk after lease entity deleted")

	h.ReconcileAll(ctx, 20)

	leaseB := getLease(t, ctx, h, leaseBID)
	assert.Equal(t, storage.BOUND, leaseB.Status, "B's lease should be BOUND")
}

func TestPoolScaleDownReleasesDisks(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	// Set up 3 running sandboxes with disks
	type sbInfo struct {
		id      entity.Id
		diskID  entity.Id
		leaseID entity.Id
	}

	var sandboxes []sbInfo
	for i := range 3 {
		sbID := entity.Id(entity.Id("sandbox/scale-" + string(rune('a'+i))))
		diskName := "scale-disk-" + string(rune('a'+i))
		diskID, leaseID := bootSandboxWithDisk(t, ctx, h, sbID, diskName, 10)
		sandboxes = append(sandboxes, sbInfo{id: sbID, diskID: diskID, leaseID: leaseID})
	}

	// Verify all 3 have bound leases
	for _, sb := range sandboxes {
		lease := getLease(t, ctx, h, sb.leaseID)
		require.Equal(t, storage.BOUND, lease.Status, "sandbox %s lease should be BOUND", sb.id)
	}

	// Simulate pool scale-down: stop 2 sandboxes (keep the first)
	for _, sb := range sandboxes[1:] {
		stopSandbox(t, ctx, h, sb.id)
	}
	h.ReconcileAll(ctx, 20)

	// Verify: first sandbox still running with BOUND lease
	lease0 := getLease(t, ctx, h, sandboxes[0].leaseID)
	assert.Equal(t, storage.BOUND, lease0.Status, "surviving sandbox's lease should be BOUND")

	sb0 := getSandbox(t, ctx, h, sandboxes[0].id)
	assert.Equal(t, compute.RUNNING, sb0.Status)

	// Verify: stopped sandboxes have RELEASED leases
	for _, sb := range sandboxes[1:] {
		lease := getLease(t, ctx, h, sb.leaseID)
		assert.Equal(t, storage.RELEASED, lease.Status, "stopped sandbox %s lease should be RELEASED", sb.id)
	}
}

func TestBoundLeaseSelfHealsAfterMountDeleted(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	sandboxID := entity.Id("sandbox/self-heal-1")
	diskID, leaseID := bootSandboxWithDisk(t, ctx, h, sandboxID, "test-disk-selfheal", 10)

	// Verify initial state
	lease := getLease(t, ctx, h, leaseID)
	require.Equal(t, storage.BOUND, lease.Status)
	assert.Equal(t, 1, countMountedMounts(t, ctx, h))

	// Find and delete the disk_mount entity
	mount := getMountForLease(t, ctx, h, leaseID)
	require.NotNil(t, mount, "should have a disk_mount")
	deleteMountEntity(t, ctx, h, mount.ID)

	// Reconcile disk-lease controller — handleBoundLease sees no mount, reverts to PENDING
	h.reconcileKind(ctx, storage.KindDiskLease, h.DiskLeaseRC)

	lease = getLease(t, ctx, h, leaseID)
	assert.Equal(t, storage.PENDING, lease.Status, "lease should revert to PENDING when mount is deleted")

	// ReconcileAll — system should self-heal: create new mount, re-bind lease
	h.ReconcileAll(ctx, 20)

	lease = getLease(t, ctx, h, leaseID)
	assert.Equal(t, storage.BOUND, lease.Status, "lease should self-heal back to BOUND")
	assert.Equal(t, 1, countMountedMounts(t, ctx, h), "should have exactly 1 mounted mount after self-heal")

	disk := getDisk(t, ctx, h, diskID)
	assert.Equal(t, storage.PROVISIONED, disk.Status)
}

func TestBoundLeaseMountError(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	// Boot sandbox A with disk
	sandboxA := entity.Id("sandbox/mnterr-a")
	diskID, leaseAID := bootSandboxWithDisk(t, ctx, h, sandboxA, "test-disk-mnterr", 10)

	lease := getLease(t, ctx, h, leaseAID)
	require.Equal(t, storage.BOUND, lease.Status)

	// Patch mount to MNT_ERROR
	mount := getMountForLease(t, ctx, h, leaseAID)
	require.NotNil(t, mount)
	patchMountError(t, ctx, h, mount.ID, "disk I/O error")

	// Reconcile disk-lease — handleBoundLease detects MNT_ERROR
	h.reconcileKind(ctx, storage.KindDiskLease, h.DiskLeaseRC)

	lease = getLease(t, ctx, h, leaseAID)
	assert.Equal(t, storage.FAILED, lease.Status, "lease should be FAILED when mount errors")
	assert.Contains(t, lease.ErrorMessage, "Mount failed")

	// Stop sandbox A so its FAILED lease gets released from tracking
	stopSandbox(t, ctx, h, sandboxA)
	h.ReconcileAll(ctx, 20)

	// New sandbox B should be able to acquire the disk
	sandboxB := entity.Id("sandbox/mnterr-b")
	createSandboxEntity(t, ctx, h, sandboxB, compute.PENDING)

	leaseBID, err := h.FakeSandbox.AcquireDiskLease(ctx, diskID, sandboxB, "", "/data", false)
	require.NoError(t, err, "B should acquire disk after A's lease failed")

	h.ReconcileAll(ctx, 20)

	leaseB := getLease(t, ctx, h, leaseBID)
	assert.Equal(t, storage.BOUND, leaseB.Status, "B's lease should be BOUND")
}

func TestPendingLeaseMountError(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	sandboxID := entity.Id("sandbox/pendmnterr-1")
	createSandboxEntity(t, ctx, h, sandboxID, compute.PENDING)

	// Create disk and lease
	diskID, err := h.FakeSandbox.EnsureDisk(ctx, "test-disk-pendmnterr", 10, "ext4")
	require.NoError(t, err)

	leaseID, err := h.FakeSandbox.AcquireDiskLease(ctx, diskID, sandboxID, "", "/data", false)
	require.NoError(t, err)

	// ReconcileAll to drive disk to PROVISIONED and create the disk_mount entity
	h.ReconcileAll(ctx, 20)

	// If the lease is already BOUND, the mount was created and is MNT_MOUNTED.
	// Revert to PENDING to test the pending-lease mount-error path.
	lease := getLease(t, ctx, h, leaseID)
	if lease.Status == storage.BOUND {
		// Patch lease back to PENDING
		patchLeaseStatus(t, ctx, h, leaseID, storage.PENDING)
	}

	// Find the mount and patch it to MNT_ERROR
	mount := getMountForLease(t, ctx, h, leaseID)
	require.NotNil(t, mount, "should have a disk_mount")
	patchMountError(t, ctx, h, mount.ID, "filesystem corruption detected")

	// Reconcile disk-lease — handlePendingLease sees existing mount in MNT_ERROR
	h.reconcileKind(ctx, storage.KindDiskLease, h.DiskLeaseRC)

	lease = getLease(t, ctx, h, leaseID)
	assert.Equal(t, storage.FAILED, lease.Status, "lease should be FAILED when mount has error")
	assert.Contains(t, lease.ErrorMessage, "Mount failed")

	// Disk should still be PROVISIONED
	disk := getDisk(t, ctx, h, diskID)
	assert.Equal(t, storage.PROVISIONED, disk.Status, "disk should still be PROVISIONED")
}

func TestDiskDeletionLifecycle(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	sandboxID := entity.Id("sandbox/diskdel-1")
	diskID, leaseID := bootSandboxWithDisk(t, ctx, h, sandboxID, "test-disk-del", 10)

	lease := getLease(t, ctx, h, leaseID)
	require.Equal(t, storage.BOUND, lease.Status)

	// Stop sandbox, reconcile to release lease and unmount
	stopSandbox(t, ctx, h, sandboxID)
	h.ReconcileAll(ctx, 20)

	lease = getLease(t, ctx, h, leaseID)
	require.Equal(t, storage.RELEASED, lease.Status)

	// Patch disk to DELETING
	patchDiskStatus(t, ctx, h, diskID, storage.DELETING)

	// Reconcile disk controller — should set disk_volume desired_state to DV_ABSENT
	h.reconcileKind(ctx, storage.KindDisk, h.DiskRC)

	// Check that volume desired_state is VOL_ABSENT
	vols := listDiskVolumes(t, ctx, h)
	require.NotEmpty(t, vols, "should still have disk_volume")
	assert.Equal(t, storage.DV_ABSENT, vols[0].DesiredState, "volume desired_state should be DV_ABSENT")

	// Reconcile disk-volume — volume controller processes deletion
	nodeId := compute.NewNodeId(testNodeId).Id()
	h.reconcileByIndex(ctx, entity.Ref(storage.DiskVolumeNodeIdId, nodeId), h.DiskVolRC)

	// Volume should now be VOL_DELETED
	vols = listDiskVolumes(t, ctx, h)
	require.NotEmpty(t, vols, "volume entity should still exist")
	assert.Equal(t, storage.DV_DELETED, vols[0].ActualState, "volume actual_state should be DV_DELETED")

	// Reconcile disk again — should delete the volume entity and then the disk entity
	h.reconcileKind(ctx, storage.KindDisk, h.DiskRC)
	// The first reconcile deletes the volume entity. The disk entity may need another pass.
	h.reconcileKind(ctx, storage.KindDisk, h.DiskRC)

	// Verify cleanup
	disks := listDisks(t, ctx, h)
	assert.Empty(t, disks, "all disks should be deleted")

	vols = listDiskVolumes(t, ctx, h)
	assert.Empty(t, vols, "all disk_volumes should be deleted")
}

func TestDiskErrorBlocksLease(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	sandboxID := entity.Id("sandbox/diskerr-1")
	createSandboxEntity(t, ctx, h, sandboxID, compute.PENDING)

	// Create disk (starts as PROVISIONING) and lease (starts as PENDING)
	diskID, err := h.FakeSandbox.EnsureDisk(ctx, "test-disk-err", 10, "ext4")
	require.NoError(t, err)

	leaseID, err := h.FakeSandbox.AcquireDiskLease(ctx, diskID, sandboxID, "", "/data", false)
	require.NoError(t, err)

	// Patch disk to ERROR before it finishes provisioning
	patchDiskStatus(t, ctx, h, diskID, storage.ERROR)

	// Reconcile disk-lease — handlePendingLease sees ERROR disk
	h.reconcileKind(ctx, storage.KindDiskLease, h.DiskLeaseRC)

	lease := getLease(t, ctx, h, leaseID)
	assert.Equal(t, storage.FAILED, lease.Status, "lease should be FAILED when disk is in ERROR")
	assert.Contains(t, lease.ErrorMessage, "not provisioned")

	// Disk should still be ERROR (unchanged)
	disk := getDisk(t, ctx, h, diskID)
	assert.Equal(t, storage.ERROR, disk.Status)
}

func TestLeaseConflictDetection(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	// Boot sandbox A with disk — lease A is BOUND and tracked in activeLeases
	sandboxA := entity.Id("sandbox/conflict-a")
	diskID, leaseAID := bootSandboxWithDisk(t, ctx, h, sandboxA, "test-disk-conflict", 10)

	leaseA := getLease(t, ctx, h, leaseAID)
	require.Equal(t, storage.BOUND, leaseA.Status)

	// Directly create a second lease entity for the same disk/node, different sandbox,
	// with BOUND status (simulates entity corruption or race)
	sandboxB := entity.Id("sandbox/conflict-b")
	createSandboxEntity(t, ctx, h, sandboxB, compute.PENDING)

	conflictLease := &storage.DiskLease{
		DiskId:     diskID,
		SandboxId:  sandboxB,
		Status:     storage.BOUND,
		AcquiredAt: time.Now(),
		Mount: storage.Mount{
			Path:    "/data",
			Options: "rw",
		},
		NodeId: compute.NewNodeId(testNodeId).Id(),
	}

	conflictLeaseID := entity.Id("disk-lease/" + idgen.GenNS("disk-lease"))
	_, err := h.EAC.Create(ctx, entity.New(
		entity.DBId, conflictLeaseID,
		conflictLease.Encode,
	).Attrs())
	require.NoError(t, err, "should be able to create conflicting lease entity")

	// Reconcile disk-lease controller — handleBoundLease should detect conflict
	h.reconcileKind(ctx, storage.KindDiskLease, h.DiskLeaseRC)

	// The conflicting lease (the one NOT tracked in activeLeases) should be FAILED
	conflictResult := getLease(t, ctx, h, conflictLeaseID)
	assert.Equal(t, storage.FAILED, conflictResult.Status, "conflicting lease should be FAILED")
	assert.Contains(t, conflictResult.ErrorMessage, "conflict", "error should mention conflict")

	// Lease A should still be BOUND
	leaseA = getLease(t, ctx, h, leaseAID)
	assert.Equal(t, storage.BOUND, leaseA.Status, "original lease should still be BOUND")

	// Mount should still be healthy
	assert.Equal(t, 1, countMountedMounts(t, ctx, h), "should still have exactly 1 mounted mount")
}

func TestConvergenceStability(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	sandboxID := entity.Id("sandbox/stable-1")
	diskID, leaseID := bootSandboxWithDisk(t, ctx, h, sandboxID, "test-disk-stable", 10)

	// Snapshot entity states
	disk := getDisk(t, ctx, h, diskID)
	require.Equal(t, storage.PROVISIONED, disk.Status)

	lease := getLease(t, ctx, h, leaseID)
	require.Equal(t, storage.BOUND, lease.Status)

	vols := listDiskVolumes(t, ctx, h)
	require.Len(t, vols, 1)
	volState := vols[0].ActualState
	volDesired := vols[0].DesiredState

	mounts := listDiskMounts(t, ctx, h)
	require.Len(t, mounts, 1)
	mountActual := mounts[0].ActualState
	mountDesired := mounts[0].DesiredState

	// Run reconciliation again (5 iterations)
	h.ReconcileAll(ctx, 5)

	// Re-read all entities and assert no state changes
	disk2 := getDisk(t, ctx, h, diskID)
	assert.Equal(t, disk.Status, disk2.Status, "disk status should not change on re-reconcile")

	lease2 := getLease(t, ctx, h, leaseID)
	assert.Equal(t, lease.Status, lease2.Status, "lease status should not change on re-reconcile")

	vols2 := listDiskVolumes(t, ctx, h)
	require.Len(t, vols2, 1)
	assert.Equal(t, volState, vols2[0].ActualState, "volume actual_state should not change")
	assert.Equal(t, volDesired, vols2[0].DesiredState, "volume desired_state should not change")

	mounts2 := listDiskMounts(t, ctx, h)
	require.Len(t, mounts2, 1)
	assert.Equal(t, mountActual, mounts2[0].ActualState, "mount actual_state should not change")
	assert.Equal(t, mountDesired, mounts2[0].DesiredState, "mount desired_state should not change")
}
