package diskio

import (
	"testing"
	"time"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity/testutils"
)

// fakeWriteTracker records the revisions handed to it so tests can assert the
// disk mount controller reports its direct entity writes.
type fakeWriteTracker struct {
	revs []int64
}

func (f *fakeWriteTracker) RecordWrite(rev int64) {
	f.revs = append(f.revs, rev)
}

// TestDiskMountControllerRecordsSelfWrites verifies that when a write tracker is
// wired, updateMountState reports the revision it wrote so the reconcile watch
// can skip its own events instead of self-retriggering (MIR-1345).
func TestDiskMountControllerRecordsSelfWrites(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskMountOps()

	mc := newTestDiskMountController(log, dataPath, nodeId, es.EAC, state, ops)
	tracker := &fakeWriteTracker{}
	mc.SetWriteTracker(tracker)

	// A mount whose backing volume is missing drives updateMountState (via the
	// error path), which is exactly the write we need to record.
	mount := &storage_v1alpha.DiskMount{
		ID:           "disk_mount/mnt-selfwrite",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		VolumeId:     "disk_volume/vol-missing",
		MountPath:    "/mnt/selfwrite",
		DesiredState: storage_v1alpha.DM_WANT_MOUNTED,
		ActualState:  storage_v1alpha.DM_PENDING,
	}
	createDiskMountEntity(ctx, t, es, mount)

	require.NoError(t, mc.ReconcileWithEntities(ctx))

	require.NotEmpty(t, tracker.revs, "controller should record the revisions it writes")
	for _, rev := range tracker.revs {
		assert.Greater(t, rev, int64(0), "recorded revisions should be positive")
	}
}

// TestDiskMountControllerSelfWritesNilTrackerSafe verifies that without a write
// tracker (as in most unit tests and any un-wired path) updateMountState is a
// no-op with respect to tracking and does not panic.
func TestDiskMountControllerSelfWritesNilTrackerSafe(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	state := NewState()
	mc := newTestDiskMountController(log, t.TempDir(), "test-node-1", es.EAC, state, newMockDiskMountOps())

	mount := &storage_v1alpha.DiskMount{
		ID:           "disk_mount/mnt-nil-tracker",
		NodeId:       compute.NewNodeId("test-node-1").Id(),
		VolumeId:     "disk_volume/vol-missing",
		MountPath:    "/mnt/nil",
		DesiredState: storage_v1alpha.DM_WANT_MOUNTED,
		ActualState:  storage_v1alpha.DM_PENDING,
	}
	createDiskMountEntity(ctx, t, es, mount)

	require.NotPanics(t, func() {
		require.NoError(t, mc.ReconcileWithEntities(ctx))
	})
}

// TestReconcileOrphanMountsDeletesMissingVolume verifies the sweeper deletes a
// disk_mount whose backing volume no longer exists and tears down its local
// state — the doubly-orphaned case from the MIR-1345 incident.
func TestReconcileOrphanMountsDeletesMissingVolume(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskMountOps()

	mount := &storage_v1alpha.DiskMount{
		ID:           "disk_mount/mnt-orphan",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		VolumeId:     "disk_volume/vol-gone",
		MountPath:    "/mnt/orphan",
		DesiredState: storage_v1alpha.DM_WANT_MOUNTED,
		ActualState:  storage_v1alpha.DM_ERROR,
	}
	createDiskMountEntity(ctx, t, es, mount)

	// Local mount state that should be torn down before the entity is deleted.
	state.SetMount("disk_mount/mnt-orphan", &MountState{
		EntityId:   "disk_mount/mnt-orphan",
		VolumeId:   "disk_volume/vol-gone",
		DevicePath: "/dev/loop5",
		MountPath:  "/mnt/orphan",
		Mounted:    true,
	})
	ops.mountedPaths["/mnt/orphan"] = true

	mc := newTestDiskMountController(log, dataPath, nodeId, es.EAC, state, ops)

	require.NoError(t, mc.ReconcileOrphanMounts(ctx, 0))

	// Entity is gone.
	_, err := es.EAC.Get(ctx, "disk_mount/mnt-orphan")
	require.Error(t, err)

	// Local state was torn down.
	assert.Nil(t, state.GetMount("disk_mount/mnt-orphan"))
	assert.Contains(t, ops.unmounts, "/mnt/orphan")
	assert.Contains(t, ops.detachedLoops, "/dev/loop5")
}

// TestReconcileOrphanMountsKeepsMountWithExistingVolume verifies the sweeper
// leaves a mount alone while its backing volume still exists.
func TestReconcileOrphanMountsKeepsMountWithExistingVolume(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	nodeId := "test-node-1"
	state := NewState()

	createDiskVolumeEntity(ctx, t, es, &storage_v1alpha.DiskVolume{
		ID:     "disk_volume/vol-live",
		NodeId: compute.NewNodeId(nodeId).Id(),
		DiskId: "disk/disk-live",
		SizeGb: 10,
	})

	mount := &storage_v1alpha.DiskMount{
		ID:           "disk_mount/mnt-live",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		VolumeId:     "disk_volume/vol-live",
		MountPath:    "/mnt/live",
		DesiredState: storage_v1alpha.DM_WANT_MOUNTED,
		ActualState:  storage_v1alpha.DM_MOUNTED,
	}
	createDiskMountEntity(ctx, t, es, mount)

	mc := newTestDiskMountController(log, t.TempDir(), nodeId, es.EAC, state, newMockDiskMountOps())

	require.NoError(t, mc.ReconcileOrphanMounts(ctx, 0))

	// Entity still present.
	_, err := es.EAC.Get(ctx, "disk_mount/mnt-live")
	require.NoError(t, err)
}

// TestReconcileOrphanMountsRespectsGracePeriod verifies a freshly created mount
// is not swept even when its backing volume is missing, so a mount created just
// before its volume is observed doesn't get deleted in a race.
func TestReconcileOrphanMountsRespectsGracePeriod(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	nodeId := "test-node-1"
	state := NewState()

	mount := &storage_v1alpha.DiskMount{
		ID:           "disk_mount/mnt-young",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		VolumeId:     "disk_volume/vol-gone",
		MountPath:    "/mnt/young",
		DesiredState: storage_v1alpha.DM_WANT_MOUNTED,
		ActualState:  storage_v1alpha.DM_ERROR,
	}
	createDiskMountEntity(ctx, t, es, mount)

	mc := newTestDiskMountController(log, t.TempDir(), nodeId, es.EAC, state, newMockDiskMountOps())

	// Just-created mount is younger than the grace period → left alone.
	require.NoError(t, mc.ReconcileOrphanMounts(ctx, time.Hour))

	_, err := es.EAC.Get(ctx, "disk_mount/mnt-young")
	require.NoError(t, err)
}
