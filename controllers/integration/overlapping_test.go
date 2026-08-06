package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	storage "miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

// Tests in this file verify invariants under overlapping and dependent events.
// Each test creates a specific interleaving of controller actions that can
// expose bugs in how controllers handle concurrent state transitions.

// TestBoundLeaseRecoverFromDetachedMount verifies that a BOUND lease
// recovers when its mount reaches MNT_DETACHED.
//
// Invariant: A BOUND lease must always have a MNT_MOUNTED mount. If the
// mount reaches a terminal non-mounted state, the lease must revert to
// PENDING so a new mount is created.
//
// Scenario: the device backend crashes → mount cleanup transitions the
// mount entity to MNT_DETACHED, but the lease wasn't updated. The lease
// controller should detect the dead mount and trigger recovery.
//
// Bug: handleBoundLease only checks for nil mount (→ PENDING) and
// MNT_ERROR (→ FAILED). MNT_DETACHED falls into the else branch which
// just logs a debug message, leaving the lease BOUND with no working mount.
// The mount controller also doesn't handle MNT_DETACHED in
// reconcileMountMounted, so neither controller self-heals.
func TestBoundLeaseRecoverFromDetachedMount(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	sandboxID := entity.Id("sandbox/recover-detach-1")
	_, leaseID := bootSandboxWithDisk(t, ctx, h, sandboxID, "disk-recover-detach", 10)

	lease := getLease(t, ctx, h, leaseID)
	require.Equal(t, storage.BOUND, lease.Status)

	mount := getMountForLease(t, ctx, h, leaseID)
	require.NotNil(t, mount)
	require.Equal(t, storage.DM_MOUNTED, mount.ActualState)

	// Simulate mount cleanup without lease update: patch mount to DM_DETACHED.
	// desired_state stays DM_WANT_MOUNTED (as it was when the lease was BOUND).
	patchMountActualState(t, ctx, h, mount.ID, storage.DiskMountActualStateDmDetachedId)

	// Run full reconciliation — both controllers get a chance to react
	h.ReconcileAll(ctx, 20)

	// Invariant: lease should have self-healed back to BOUND with a working mount.
	lease = getLease(t, ctx, h, leaseID)
	if lease.Status == storage.BOUND {
		currentMount := getMountForLease(t, ctx, h, leaseID)
		if currentMount != nil {
			assert.Equal(t, storage.DM_MOUNTED, currentMount.ActualState,
				"BOUND lease's mount should be DM_MOUNTED, not %s — "+
					"neither handleBoundLease nor reconcileMountMounted recovers from DM_DETACHED",
				currentMount.ActualState)
		} else {
			t.Error("BOUND lease has no mount entity — stuck without self-healing")
		}
	}
	// If lease reverted to PENDING, that's an acceptable intermediate step
	// as long as ReconcileAll eventually drives it back to BOUND.
	// The key failure is staying BOUND with a DETACHED mount.
}

// TestPendingLeaseNotStuckOnDetachedMount verifies that a PENDING lease
// does not wait indefinitely on an existing mount in MNT_DETACHED.
//
// Invariant: A PENDING lease must either progress toward BOUND or transition
// to FAILED. It must not wait forever on a mount that will never progress.
//
// Scenario: A mount was created for the lease, but something went wrong
// and the mount ended up DETACHED. The lease was reverted to PENDING for
// retry. The stale mount entity in DETACHED state blocks the lease.
//
// Bug: handlePendingLease's switch on existingMount.ActualState has cases
// for MNT_MOUNTED (bind) and MNT_ERROR (fail). The default returns nil
// ("wait"), but MNT_DETACHED is a terminal state that never progresses.
func TestPendingLeaseNotStuckOnDetachedMount(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	sandboxID := entity.Id("sandbox/stuck-detach-1")
	_, leaseID := bootSandboxWithDisk(t, ctx, h, sandboxID, "disk-stuck-detach", 10)

	lease := getLease(t, ctx, h, leaseID)
	require.Equal(t, storage.BOUND, lease.Status)

	mount := getMountForLease(t, ctx, h, leaseID)
	require.NotNil(t, mount)

	// Simulate: mount cleanup completed, lease reverted to PENDING for retry.
	patchMountActualState(t, ctx, h, mount.ID, storage.DiskMountActualStateDmDetachedId)
	patchLeaseStatus(t, ctx, h, leaseID, storage.PENDING)

	// Run full reconciliation
	h.ReconcileAll(ctx, 20)

	// Invariant: lease should have progressed past PENDING.
	lease = getLease(t, ctx, h, leaseID)
	assert.NotEqual(t, storage.PENDING, lease.Status,
		"PENDING lease with DM_DETACHED mount should not stay PENDING — "+
			"handlePendingLease treats DM_DETACHED as 'in progress' but it's terminal")

	// Best case: lease is BOUND with a fresh mount
	if lease.Status == storage.BOUND {
		currentMount := getMountForLease(t, ctx, h, leaseID)
		require.NotNil(t, currentMount)
		assert.Equal(t, storage.DM_MOUNTED, currentMount.ActualState)
	}
}

// TestStopDuringMountCreation verifies that stopping a sandbox while its
// disk mount entity has been created but not yet processed by the mount
// controller results in clean convergence with no leaked resources.
//
// Invariant: After stopping a sandbox and running reconciliation to
// convergence, there must be no DM_MOUNTED mounts and no stuck leases.
//
// Scenario:
//  1. Lease controller creates disk_mount entity (DM_PENDING)
//  2. Before mount controller processes it, sandbox is stopped
//  3. Lease goes RELEASED → mount desired becomes DM_WANT_UNMOUNTED
//  4. Mount controller sees desired=UNMOUNTED, actual=PENDING → cleans up
func TestStopDuringMountCreation(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	sandboxID := entity.Id("sandbox/stopcreate-1")
	createSandboxEntity(t, ctx, h, sandboxID, compute.PENDING)

	diskID, err := h.FakeSandbox.EnsureDisk(ctx, "disk-stopcreate", 10, "ext4")
	require.NoError(t, err)

	leaseID, err := h.FakeSandbox.AcquireDiskLease(ctx, diskID, sandboxID, "", "/data", false)
	require.NoError(t, err)

	// Drive disk to PROVISIONED without processing the mount
	nodeId := compute.NewNodeId(testNodeId).Id()
	for range 5 {
		h.reconcileKind(ctx, storage.KindDisk, h.DiskRC)
		h.reconcileByIndex(ctx, entity.Ref(storage.DiskVolumeNodeIdId, nodeId), h.DiskVolRC)
	}

	// Reconcile ONLY the lease controller to create mount entity
	h.reconcileKind(ctx, storage.KindDiskLease, h.DiskLeaseRC)

	// Verify mount entity exists but hasn't been mounted
	mount := getMountForLease(t, ctx, h, leaseID)
	// If mount wasn't created (disk still provisioning), drive more reconciliation
	if mount == nil {
		for range 3 {
			h.reconcileKind(ctx, storage.KindDisk, h.DiskRC)
			h.reconcileByIndex(ctx, entity.Ref(storage.DiskVolumeNodeIdId, nodeId), h.DiskVolRC)
			h.reconcileKind(ctx, storage.KindDiskLease, h.DiskLeaseRC)
		}
		_ = getMountForLease(t, ctx, h, leaseID)
	}

	// Stop sandbox BEFORE mount controller runs
	err = h.FakeSandbox.ReleaseDiskLeases(ctx, sandboxID)
	require.NoError(t, err)
	markSandboxDead(t, ctx, h, sandboxID)

	// Reconcile everything to convergence
	h.ReconcileAll(ctx, 20)

	// Invariant: lease should be RELEASED
	lease := getLease(t, ctx, h, leaseID)
	assert.Equal(t, storage.RELEASED, lease.Status, "lease should be RELEASED after stop")

	// Invariant: no mounted mounts should exist
	assert.Equal(t, 0, countMountedMounts(t, ctx, h),
		"no mounts should be MOUNTED after sandbox stopped before mount completed")
}

// TestReplacementMountPathCollision verifies that when a replacement
// sandbox acquires the same disk, the old mount's unmount doesn't destroy
// the new mount at the shared filesystem path.
//
// Invariant: After convergence, the replacement sandbox should have a
// BOUND lease and a functioning MNT_MOUNTED mount.
//
// Scenario:
//  1. Sandbox A running → BOUND lease, MNT_MOUNTED at path P
//  2. A stopped → lease RELEASED, mount desired=UNMOUNTED (actual still MOUNTED)
//  3. B created → acquires same disk → new mount entity at same path P
//  4. Mount controller processes both: if B's mount is processed first, B
//     mounts at P, then A unmounts P → B's mount is destroyed
//
// Bug: The mount controller uses path-based unmount (Unmount(mountPath)) but
// entity-based state tracking. Two mount entities can share the same path
// when they reference the same disk. Unmounting one destroys the other.
func TestReplacementMountPathCollision(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	// Boot sandbox A
	sandboxA := entity.Id("sandbox/pathcol-a")
	diskID, leaseAID := bootSandboxWithDisk(t, ctx, h, sandboxA, "disk-pathcol", 10)

	leaseA := getLease(t, ctx, h, leaseAID)
	require.Equal(t, storage.BOUND, leaseA.Status)

	mountA := getMountForLease(t, ctx, h, leaseAID)
	require.NotNil(t, mountA)
	require.Equal(t, storage.DM_MOUNTED, mountA.ActualState)
	mountAID := mountA.ID
	sharedMountPath := mountA.MountPath
	require.NotEmpty(t, sharedMountPath)

	// Stop A: release lease, do NOT reconcile mount controller yet
	err := h.FakeSandbox.ReleaseDiskLeases(ctx, sandboxA)
	require.NoError(t, err)
	markSandboxDead(t, ctx, h, sandboxA)

	// Reconcile ONLY lease controller — marks mount-A for unmount but
	// mount controller hasn't run so mount-A is still "mounted" in the mock
	h.reconcileKind(ctx, storage.KindDiskLease, h.DiskLeaseRC)

	mountAEntity := getMountByID(t, ctx, h, mountAID)
	require.Equal(t, storage.DM_WANT_UNMOUNTED, mountAEntity.DesiredState,
		"mount-A should be marked for unmount")

	// Boot sandbox B with same disk
	sandboxB := entity.Id("sandbox/pathcol-b")
	createSandboxEntity(t, ctx, h, sandboxB, compute.PENDING)

	leaseBID, err := h.FakeSandbox.AcquireDiskLease(ctx, diskID, sandboxB, "", "/data", false)
	require.NoError(t, err)

	// Reconcile lease controller to create mount-B
	h.reconcileKind(ctx, storage.KindDiskLease, h.DiskLeaseRC)

	mountB := getMountForLease(t, ctx, h, leaseBID)
	// May need more reconcile rounds if disk provisioning check deferred
	if mountB == nil {
		h.reconcileKind(ctx, storage.KindDiskLease, h.DiskLeaseRC)
		mountB = getMountForLease(t, ctx, h, leaseBID)
	}
	require.NotNil(t, mountB, "mount-B should have been created")
	mountBID := mountB.ID

	// Both mounts should target the same path (same underlying disk/volume)
	assert.Equal(t, sharedMountPath, mountB.MountPath,
		"both mounts should share the same path since they reference the same disk")

	// KEY INTERLEAVING: Process mount-B first (attach+mount), then mount-A (unmount).
	// This is the worst case ordering that exposes the path collision bug.
	for range 3 {
		err = h.ReconcileEntity(ctx, mountBID)
		require.NoError(t, err, "ReconcileEntity for mount-B should not fail")
		bState := getMountByID(t, ctx, h, mountBID)
		if bState.ActualState == storage.DM_MOUNTED {
			break
		}
	}

	// Verify mount-B is mounted in the mock
	require.True(t, h.MockMountOps.IsMounted(sharedMountPath),
		"mount-B should have mounted at the shared path")

	// Now process mount-A's unmount — this is where the collision happens
	err = h.ReconcileEntity(ctx, mountAID)
	require.NoError(t, err)

	// Check: A's unmount should NOT have destroyed B's mount
	assert.True(t, h.MockMountOps.IsMounted(sharedMountPath),
		"mount-B's path should still be mounted after mount-A's unmount — "+
			"A's unmount at the shared path destroyed B's active mount")

	// Even if the above fails, run ReconcileAll to check convergence
	h.ReconcileAll(ctx, 20)

	// After convergence, B should be fully functional
	leaseB := getLease(t, ctx, h, leaseBID)
	assert.Equal(t, storage.BOUND, leaseB.Status, "B's lease should be BOUND")
	assert.Equal(t, 1, countMountedMounts(t, ctx, h),
		"should have exactly 1 mounted mount after convergence")
}
