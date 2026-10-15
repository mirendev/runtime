package diskio

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func newTestDiskMountController(log *slog.Logger, dataPath, nodeId string, eac *entityserver_v1alpha.EntityAccessClient, state *State, ops DiskMountOps) *DiskMountController {
	state.SetPath(dataPath)
	mc := NewDiskMountController(log, dataPath, compute.NewNodeId(nodeId), state, ops)
	mc.SetEAC(eac)
	return mc
}

func createDiskMountEntity(ctx context.Context, t *testing.T, es *testutils.InMemEntityServer, mount *storage_v1alpha.DiskMount) {
	_, err := es.EAC.Create(ctx, entity.New(
		entity.DBId, mount.ID,
		mount.Encode,
	).Attrs())
	require.NoError(t, err)
}

func TestDiskMountControllerReconcileMountMounted(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskMountOps()

	// Pre-populate volume state (mount requires an existing volume)
	state.SetVolume("disk_volume/vol-123", &VolumeState{
		EntityId:   "disk_volume/vol-123",
		VolumeId:   "vol-123",
		DiskPath:   "/data/volumes/vol-123",
		SizeBytes:  10 * 1024 * 1024 * 1024,
		Filesystem: "ext4",
	})

	mc := newTestDiskMountController(log, dataPath, nodeId, es.EAC, state, ops)

	mount := &storage_v1alpha.DiskMount{
		ID:           "disk_mount/mnt-123",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		VolumeId:     "disk_volume/vol-123",
		MountPath:    "/mnt/data",
		ReadOnly:     false,
		DesiredState: storage_v1alpha.DM_WANT_MOUNTED,
		ActualState:  storage_v1alpha.DM_PENDING,
	}
	createDiskMountEntity(ctx, t, es, mount)

	err := mc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// Verify loop device was attached
	assert.Len(t, ops.attachedLoops, 1)
	assert.Equal(t, "/data/volumes/vol-123/disk.img", ops.attachedLoops[0])

	// Verify filesystem was formatted
	assert.Len(t, ops.formatCalls, 1)
	assert.Equal(t, "/dev/loop0", ops.formatCalls[0].device)
	assert.Equal(t, "ext4", ops.formatCalls[0].filesystem)

	// Verify mount was performed
	assert.Len(t, ops.mounts, 1)
	assert.Equal(t, "/dev/loop0", ops.mounts[0].device)
	assert.Equal(t, "/mnt/data", ops.mounts[0].mountPath)
	assert.Equal(t, "ext4", ops.mounts[0].filesystem)
	assert.False(t, ops.mounts[0].readOnly)

	// Verify state was updated
	mountState := state.GetMount("disk_mount/mnt-123")
	require.NotNil(t, mountState)
	assert.True(t, mountState.Mounted)
	assert.Equal(t, "/mnt/data", mountState.MountPath)
	assert.Equal(t, "/dev/loop0", mountState.DevicePath)

	// Verify entity was updated to MOUNTED
	resp, err := es.EAC.Get(ctx, "disk_mount/mnt-123")
	require.NoError(t, err)
	var updated storage_v1alpha.DiskMount
	updated.Decode(resp.Entity().Entity())
	assert.Equal(t, storage_v1alpha.DM_MOUNTED, updated.ActualState)
	assert.Equal(t, "/dev/loop0", updated.DevicePath)
	assert.Equal(t, "/dev/loop0", updated.LoopDevice)
}

// TestDiskMountControllerAdoptsExistingLoopDevice verifies that when a disk
// image is already attached to a loop device in the kernel (e.g. left over
// from a SIGKILL'd miren process whose container kept running), the
// controller adopts that existing device rather than allocating a second
// loop for the same backing file. Double-attach would produce two
// incoherent page caches and corrupt the filesystem.
func TestDiskMountControllerAdoptsExistingLoopDevice(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskMountOps()

	state.SetVolume("disk_volume/vol-972", &VolumeState{
		EntityId:   "disk_volume/vol-972",
		VolumeId:   "vol-972",
		DiskPath:   "/data/volumes/vol-972",
		SizeBytes:  10 * 1024 * 1024 * 1024,
		Filesystem: "ext4",
	})

	// Simulate a stale loop device backed by the disk image. This mimics
	// the kernel state after an unclean miren shutdown: the old loop
	// device is still present in the kernel, the filesystem has already
	// been formatted, but the volume is not currently mounted at our
	// target path.
	imagePath := "/data/volumes/vol-972/disk.img"
	const staleLoopDev = "/dev/loop7"
	ops.loopBacking = map[string]string{imagePath: staleLoopDev}
	ops.formattedDevs[staleLoopDev] = "ext4"

	mc := newTestDiskMountController(log, dataPath, nodeId, es.EAC, state, ops)

	mount := &storage_v1alpha.DiskMount{
		ID:           "disk_mount/mnt-972",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		VolumeId:     "disk_volume/vol-972",
		MountPath:    "/mnt/data",
		ReadOnly:     false,
		DesiredState: storage_v1alpha.DM_WANT_MOUNTED,
		ActualState:  storage_v1alpha.DM_PENDING,
	}
	createDiskMountEntity(ctx, t, es, mount)

	err := mc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// Critically: LoopAttach must NOT have been called. A second loop
	// backing the same file would be a double-attach.
	assert.Empty(t, ops.attachedLoops, "LoopAttach must not be called when backing file is already attached")

	// The existing device should have been adopted and mounted at the
	// requested mount path.
	require.Len(t, ops.mounts, 1)
	assert.Equal(t, staleLoopDev, ops.mounts[0].device)
	assert.Equal(t, "/mnt/data", ops.mounts[0].mountPath)

	// It was already formatted, so FormatDevice must not be called.
	assert.Empty(t, ops.formatCalls, "pre-formatted adopted device must not be re-formatted")

	// Entity should report MOUNTED with the adopted device path.
	resp, err := es.EAC.Get(ctx, "disk_mount/mnt-972")
	require.NoError(t, err)
	var updated storage_v1alpha.DiskMount
	updated.Decode(resp.Entity().Entity())
	assert.Equal(t, storage_v1alpha.DM_MOUNTED, updated.ActualState)
	assert.Equal(t, staleLoopDev, updated.DevicePath)
}

func TestDiskMountControllerReconcileMountUnmounted(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskMountOps()

	// Pre-populate state with existing mount
	state.SetMount("disk_mount/mnt-456", &MountState{
		EntityId:    "disk_mount/mnt-456",
		VolumeId:    "disk_volume/vol-456",
		DevicePath:  "/dev/loop5",
		BackingPath: "/data/vol-456/disk.img",
		MountPath:   "/mnt/data",
		Mounted:     true,
		ReadOnly:    false,
	})
	ops.mountedPaths["/mnt/data"] = true
	ops.mountDevices["/mnt/data"] = "/dev/loop5"
	ops.loopBacking = map[string]string{"/data/vol-456/disk.img": "/dev/loop5"}

	mc := newTestDiskMountController(log, dataPath, nodeId, es.EAC, state, ops)

	mount := &storage_v1alpha.DiskMount{
		ID:           "disk_mount/mnt-456",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		VolumeId:     "disk_volume/vol-456",
		MountPath:    "/mnt/data",
		DesiredState: storage_v1alpha.DM_WANT_UNMOUNTED,
		ActualState:  storage_v1alpha.DM_MOUNTED,
	}
	createDiskMountEntity(ctx, t, es, mount)

	err := mc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// Verify unmount was called
	assert.Len(t, ops.unmounts, 1)
	assert.Equal(t, "/mnt/data", ops.unmounts[0])

	// Verify loop device was detached
	assert.Len(t, ops.detachedLoops, 1)
	assert.Equal(t, "/dev/loop5", ops.detachedLoops[0])

	// Verify state was cleaned up
	assert.Nil(t, state.GetMount("disk_mount/mnt-456"))

	// Verify entity was updated to DETACHED
	resp, err := es.EAC.Get(ctx, "disk_mount/mnt-456")
	require.NoError(t, err)
	var updated storage_v1alpha.DiskMount
	updated.Decode(resp.Entity().Entity())
	assert.Equal(t, storage_v1alpha.DM_DETACHED, updated.ActualState)
}

func TestDiskMountControllerReconcileSkipsOtherNodes(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskMountOps()

	mc := newTestDiskMountController(log, dataPath, nodeId, es.EAC, state, ops)

	mount := &storage_v1alpha.DiskMount{
		ID:           "disk_mount/mnt-other",
		NodeId:       compute.NewNodeId("other-node").Id(),
		VolumeId:     "disk_volume/vol-other",
		MountPath:    "/mnt/other",
		DesiredState: storage_v1alpha.DM_WANT_MOUNTED,
		ActualState:  storage_v1alpha.DM_PENDING,
	}
	createDiskMountEntity(ctx, t, es, mount)

	err := mc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	assert.Empty(t, ops.attachedLoops)
	assert.Empty(t, ops.mounts)
}

func TestDiskMountControllerReconcileMountAlreadyMounted(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskMountOps()

	// Pre-populate state with existing mounted volume
	state.SetMount("disk_mount/mnt-ready", &MountState{
		EntityId:   "disk_mount/mnt-ready",
		VolumeId:   "disk_volume/vol-ready",
		DevicePath: "/dev/loop3",
		MountPath:  "/mnt/ready",
		Mounted:    true,
	})
	ops.mountedPaths["/mnt/ready"] = true

	mc := newTestDiskMountController(log, dataPath, nodeId, es.EAC, state, ops)

	mount := &storage_v1alpha.DiskMount{
		ID:           "disk_mount/mnt-ready",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		VolumeId:     "disk_volume/vol-ready",
		MountPath:    "/mnt/ready",
		DesiredState: storage_v1alpha.DM_WANT_MOUNTED,
		ActualState:  storage_v1alpha.DM_MOUNTED,
	}
	createDiskMountEntity(ctx, t, es, mount)

	err := mc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// No new operations should be performed
	assert.Empty(t, ops.attachedLoops)
	assert.Empty(t, ops.mounts)
	assert.Empty(t, ops.formatCalls)
}

func TestDiskMountControllerReconcileVolumeNotFound(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskMountOps()

	mc := newTestDiskMountController(log, dataPath, nodeId, es.EAC, state, ops)

	mount := &storage_v1alpha.DiskMount{
		ID:           "disk_mount/mnt-missing",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		VolumeId:     "disk_volume/vol-missing",
		MountPath:    "/mnt/missing",
		DesiredState: storage_v1alpha.DM_WANT_MOUNTED,
		ActualState:  storage_v1alpha.DM_PENDING,
	}
	createDiskMountEntity(ctx, t, es, mount)

	err := mc.ReconcileWithEntities(ctx)
	require.NoError(t, err) // ReconcileWithEntities logs errors but doesn't return them

	// Verify entity was updated to error state
	resp, err := es.EAC.Get(ctx, "disk_mount/mnt-missing")
	require.NoError(t, err)
	var updated storage_v1alpha.DiskMount
	updated.Decode(resp.Entity().Entity())
	assert.Equal(t, storage_v1alpha.DM_ERROR, updated.ActualState)
	assert.Contains(t, updated.ErrorMessage, "not found")
}

func TestDiskMountControllerReconcileMountReadOnly(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskMountOps()

	state.SetVolume("disk_volume/vol-ro", &VolumeState{
		EntityId:   "disk_volume/vol-ro",
		VolumeId:   "vol-ro",
		DiskPath:   "/data/volumes/vol-ro",
		SizeBytes:  5 * 1024 * 1024 * 1024,
		Filesystem: "xfs",
	})

	// Pre-format the device to skip formatting
	ops.formattedDevs["/dev/loop0"] = "xfs"

	mc := newTestDiskMountController(log, dataPath, nodeId, es.EAC, state, ops)

	mount := &storage_v1alpha.DiskMount{
		ID:           "disk_mount/mnt-ro",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		VolumeId:     "disk_volume/vol-ro",
		MountPath:    "/mnt/readonly",
		ReadOnly:     true,
		DesiredState: storage_v1alpha.DM_WANT_MOUNTED,
		ActualState:  storage_v1alpha.DM_PENDING,
	}
	createDiskMountEntity(ctx, t, es, mount)

	err := mc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// Verify mount was called with readOnly flag
	assert.Len(t, ops.mounts, 1)
	assert.True(t, ops.mounts[0].readOnly)
	assert.Equal(t, "xfs", ops.mounts[0].filesystem)

	// Verify no formatting occurred (already formatted)
	assert.Empty(t, ops.formatCalls)

	// Verify state reflects read-only
	mountState := state.GetMount("disk_mount/mnt-ro")
	require.NotNil(t, mountState)
	assert.True(t, mountState.ReadOnly)
}

func TestDiskMountControllerReconcileAlreadyDetached(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskMountOps()

	mc := newTestDiskMountController(log, dataPath, nodeId, es.EAC, state, ops)

	mount := &storage_v1alpha.DiskMount{
		ID:           "disk_mount/mnt-detached",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		VolumeId:     "disk_volume/vol-detached",
		MountPath:    "/mnt/detached",
		DesiredState: storage_v1alpha.DM_WANT_UNMOUNTED,
		ActualState:  storage_v1alpha.DM_DETACHED,
	}
	createDiskMountEntity(ctx, t, es, mount)

	err := mc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	assert.Empty(t, ops.unmounts)
	assert.Empty(t, ops.detachedLoops)
}

func TestDiskMountControllerReconcileErrorRecovery(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskMountOps()

	state.SetVolume("disk_volume/vol-err", &VolumeState{
		EntityId:   "disk_volume/vol-err",
		VolumeId:   "vol-err",
		DiskPath:   "/data/volumes/vol-err",
		SizeBytes:  10 * 1024 * 1024 * 1024,
		Filesystem: "ext4",
	})

	mc := newTestDiskMountController(log, dataPath, nodeId, es.EAC, state, ops)

	mount := &storage_v1alpha.DiskMount{
		ID:           "disk_mount/mnt-err",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		VolumeId:     "disk_volume/vol-err",
		MountPath:    "/mnt/recover",
		DesiredState: storage_v1alpha.DM_WANT_MOUNTED,
		ActualState:  storage_v1alpha.DM_ERROR,
	}
	createDiskMountEntity(ctx, t, es, mount)

	err := mc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// Should have attempted recovery (attach + mount)
	assert.Len(t, ops.attachedLoops, 1)
	assert.Len(t, ops.mounts, 1)
}

func TestDiskMountControllerMultipleMounts(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskMountOps()

	for i := 1; i <= 3; i++ {
		volId := entity.Id("disk_volume/vol-" + string(rune('0'+i)))
		state.SetVolume(string(volId), &VolumeState{
			EntityId:   string(volId),
			VolumeId:   "vol-" + string(rune('0'+i)),
			DiskPath:   "/data/volumes/vol-" + string(rune('0'+i)),
			SizeBytes:  int64(i * 10 * 1024 * 1024 * 1024),
			Filesystem: "ext4",
		})
	}

	mc := newTestDiskMountController(log, dataPath, nodeId, es.EAC, state, ops)

	for i := 1; i <= 3; i++ {
		mount := &storage_v1alpha.DiskMount{
			ID:           entity.Id("disk_mount/mnt-" + string(rune('0'+i))),
			NodeId:       compute.NewNodeId(nodeId).Id(),
			VolumeId:     entity.Id("disk_volume/vol-" + string(rune('0'+i))),
			MountPath:    "/mnt/data" + string(rune('0'+i)),
			DesiredState: storage_v1alpha.DM_WANT_MOUNTED,
			ActualState:  storage_v1alpha.DM_PENDING,
		}
		createDiskMountEntity(ctx, t, es, mount)
	}

	err := mc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	assert.Len(t, ops.attachedLoops, 3)
	assert.Len(t, ops.mounts, 3)
	assert.Len(t, state.Mounts, 3)
}

func TestDiskMountControllerReconcileCleansUpOrphanedMounts(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskMountOps()

	state.SetMount("disk_mount/mnt-orphan", &MountState{
		EntityId:    "disk_mount/mnt-orphan",
		VolumeId:    "disk_volume/vol-orphan",
		DevicePath:  "/dev/loop7",
		BackingPath: "/data/vol-orphan/disk.img",
		MountPath:   "/mnt/orphan",
		Mounted:     true,
		ReadOnly:    false,
	})
	ops.mountedPaths["/mnt/orphan"] = true
	ops.mountDevices["/mnt/orphan"] = "/dev/loop7"
	ops.loopBacking = map[string]string{"/data/vol-orphan/disk.img": "/dev/loop7"}

	mc := newTestDiskMountController(log, dataPath, nodeId, es.EAC, state, ops)

	err := mc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	assert.Contains(t, ops.unmounts, "/mnt/orphan")
	assert.Contains(t, ops.detachedLoops, "/dev/loop7")
	assert.Nil(t, state.GetMount("disk_mount/mnt-orphan"))
}

func TestDiskMountControllerReconcileKeepsNonOrphanedMounts(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskMountOps()

	state.SetVolume("disk_volume/vol-keep", &VolumeState{
		EntityId:   "disk_volume/vol-keep",
		VolumeId:   "vol-keep",
		DiskPath:   "/data/volumes/vol-keep",
		SizeBytes:  10 * 1024 * 1024 * 1024,
		Filesystem: "ext4",
	})

	state.SetMount("disk_mount/mnt-keep", &MountState{
		EntityId:   "disk_mount/mnt-keep",
		VolumeId:   "disk_volume/vol-keep",
		DevicePath: "/dev/loop8",
		MountPath:  "/mnt/keep",
		Mounted:    true,
		ReadOnly:   false,
	})
	ops.mountedPaths["/mnt/keep"] = true

	state.SetMount("disk_mount/mnt-orphan2", &MountState{
		EntityId:    "disk_mount/mnt-orphan2",
		VolumeId:    "disk_volume/vol-orphan2",
		DevicePath:  "/dev/loop9",
		BackingPath: "/data/vol-orphan2/disk.img",
		MountPath:   "/mnt/orphan2",
		Mounted:     true,
		ReadOnly:    false,
	})
	ops.mountedPaths["/mnt/orphan2"] = true
	ops.mountDevices["/mnt/orphan2"] = "/dev/loop9"
	ops.loopBacking = map[string]string{"/data/vol-orphan2/disk.img": "/dev/loop9"}

	mc := newTestDiskMountController(log, dataPath, nodeId, es.EAC, state, ops)

	mount := &storage_v1alpha.DiskMount{
		ID:           "disk_mount/mnt-keep",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		VolumeId:     "disk_volume/vol-keep",
		MountPath:    "/mnt/keep",
		DesiredState: storage_v1alpha.DM_WANT_MOUNTED,
		ActualState:  storage_v1alpha.DM_MOUNTED,
	}
	createDiskMountEntity(ctx, t, es, mount)

	err := mc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	assert.NotNil(t, state.GetMount("disk_mount/mnt-keep"))
	assert.Nil(t, state.GetMount("disk_mount/mnt-orphan2"))
	assert.Contains(t, ops.unmounts, "/mnt/orphan2")
	assert.NotContains(t, ops.unmounts, "/mnt/keep")
}

func TestDiskMountControllerShutdown(t *testing.T) {
	log := testutils.TestLogger(t)

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskMountOps()

	mc := NewDiskMountController(log, dataPath, compute.NewNodeId(nodeId), state, ops)

	// Mount state with no cloud client — Shutdown should be a no-op for unmount/detach
	// (DiskVolumeController handles that now)
	state.SetMount("disk_mount/mnt-a", &MountState{
		EntityId:   "disk_mount/mnt-a",
		DevicePath: "/dev/loop1",
		MountPath:  "/mnt/a",
		Mounted:    true,
	})

	mc.Shutdown()

	// DiskMountController.Shutdown no longer unmounts/detaches — that's DiskVolumeController's job
	assert.Empty(t, ops.unmounts)
	assert.Empty(t, ops.detachedLoops)
}

func TestDiskMountControllerUnmountNotInState(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskMountOps()

	mc := newTestDiskMountController(log, dataPath, nodeId, es.EAC, state, ops)

	// Mount not in local state but entity requests unmount
	mount := &storage_v1alpha.DiskMount{
		ID:           "disk_mount/mnt-gone",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		VolumeId:     "disk_volume/vol-gone",
		MountPath:    "/mnt/gone",
		DesiredState: storage_v1alpha.DM_WANT_UNMOUNTED,
		ActualState:  storage_v1alpha.DM_MOUNTED,
	}
	createDiskMountEntity(ctx, t, es, mount)

	err := mc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// Should still transition to DETACHED even without local state
	resp, err := es.EAC.Get(ctx, "disk_mount/mnt-gone")
	require.NoError(t, err)
	var updated storage_v1alpha.DiskMount
	updated.Decode(resp.Entity().Entity())
	assert.Equal(t, storage_v1alpha.DM_DETACHED, updated.ActualState)
}

func TestDiskMountControllerUniversalSkipsMountOnAttach(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskMountOps()

	// Volume is already mounted by volume controller (alwaysMount)
	state.SetVolume("disk_volume/vol-uni", &VolumeState{
		EntityId:   "disk_volume/vol-uni",
		VolumeId:   "vol-uni",
		DiskPath:   "/data/volumes/vol-uni",
		SizeBytes:  10 * 1024 * 1024 * 1024,
		Filesystem: "ext4",
		Mode:       storage_v1alpha.VM_UNIVERSAL,
		DevicePath: "/dev/loop5",
		MountPath:  "/mnt/vol-uni",
		Mounted:    true,
	})

	mc := newTestDiskMountController(log, dataPath, nodeId, es.EAC, state, ops)

	mount := &storage_v1alpha.DiskMount{
		ID:           "disk_mount/mnt-uni",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		VolumeId:     "disk_volume/vol-uni",
		MountPath:    "/mnt/vol-uni",
		DesiredState: storage_v1alpha.DM_WANT_MOUNTED,
		ActualState:  storage_v1alpha.DM_PENDING,
	}
	createDiskMountEntity(ctx, t, es, mount)

	err := mc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// No actual loop attach or mount should have been called
	assert.Empty(t, ops.attachedLoops, "should not loop attach for universal mode")
	assert.Empty(t, ops.mounts, "should not mount for universal mode")
	assert.Empty(t, ops.formatCalls, "should not format for universal mode")

	// State should be tracking the mount
	mountState := state.GetMount("disk_mount/mnt-uni")
	require.NotNil(t, mountState)
	assert.True(t, mountState.Mounted)
	assert.Equal(t, "/dev/loop5", mountState.DevicePath)
	assert.Equal(t, "/mnt/vol-uni", mountState.MountPath)
	assert.Equal(t, storage_v1alpha.VM_UNIVERSAL, mountState.Mode)

	// Entity should be MOUNTED
	resp, err := es.EAC.Get(ctx, "disk_mount/mnt-uni")
	require.NoError(t, err)
	var updated storage_v1alpha.DiskMount
	updated.Decode(resp.Entity().Entity())
	assert.Equal(t, storage_v1alpha.DM_MOUNTED, updated.ActualState)
}

func TestDiskMountControllerUniversalSkipsUnmountOnDetach(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskMountOps()

	// Pre-populate with mounted universal volume
	state.SetMount("disk_mount/mnt-uni-off", &MountState{
		EntityId:   "disk_mount/mnt-uni-off",
		VolumeId:   "disk_volume/vol-uni",
		DevicePath: "/dev/loop5",
		MountPath:  "/mnt/vol-uni",
		Mounted:    true,
		Mode:       storage_v1alpha.VM_UNIVERSAL,
	})
	ops.mountedPaths["/mnt/vol-uni"] = true

	mc := newTestDiskMountController(log, dataPath, nodeId, es.EAC, state, ops)

	mount := &storage_v1alpha.DiskMount{
		ID:           "disk_mount/mnt-uni-off",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		VolumeId:     "disk_volume/vol-uni",
		MountPath:    "/mnt/vol-uni",
		DesiredState: storage_v1alpha.DM_WANT_UNMOUNTED,
		ActualState:  storage_v1alpha.DM_MOUNTED,
	}
	createDiskMountEntity(ctx, t, es, mount)

	err := mc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// No actual unmount or detach should have been called
	assert.Empty(t, ops.unmounts, "should not unmount for universal mode")
	assert.Empty(t, ops.detachedLoops, "should not detach for universal mode")

	// Mount state should be cleaned up
	assert.Nil(t, state.GetMount("disk_mount/mnt-uni-off"))

	// Entity should be DETACHED
	resp, err := es.EAC.Get(ctx, "disk_mount/mnt-uni-off")
	require.NoError(t, err)
	var updated storage_v1alpha.DiskMount
	updated.Decode(resp.Entity().Entity())
	assert.Equal(t, storage_v1alpha.DM_DETACHED, updated.ActualState)
}

func TestDiskMountControllerUniversalShutdownSkips(t *testing.T) {
	log := testutils.TestLogger(t)

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskMountOps()

	mc := NewDiskMountController(log, dataPath, compute.NewNodeId(nodeId), state, ops)

	ops.mountedPaths["/mnt/uni"] = true
	ops.mountedPaths["/mnt/acc"] = true

	state.SetMount("disk_mount/mnt-uni", &MountState{
		EntityId:   "disk_mount/mnt-uni",
		DevicePath: "/dev/loop1",
		MountPath:  "/mnt/uni",
		Mounted:    true,
		Mode:       storage_v1alpha.VM_UNIVERSAL,
	})
	state.SetMount("disk_mount/mnt-acc", &MountState{
		EntityId:   "disk_mount/mnt-acc",
		DevicePath: "/dev/loop2",
		MountPath:  "/mnt/acc",
		Mounted:    true,
		Mode:       storage_v1alpha.VM_ACCELERATOR,
	})

	mc.Shutdown()

	// DiskMountController no longer unmounts/detaches anything — DiskVolumeController handles that
	assert.Empty(t, ops.unmounts)
	assert.Empty(t, ops.detachedLoops)
}

func TestDiskMountControllerUniversalOrphanSkipsUnmount(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskMountOps()

	// Orphan mount with universal mode
	state.SetMount("disk_mount/mnt-orphan-uni", &MountState{
		EntityId:   "disk_mount/mnt-orphan-uni",
		VolumeId:   "disk_volume/vol-orphan-uni",
		DevicePath: "/dev/loop7",
		MountPath:  "/mnt/orphan-uni",
		Mounted:    true,
		Mode:       storage_v1alpha.VM_UNIVERSAL,
	})

	mc := newTestDiskMountController(log, dataPath, nodeId, es.EAC, state, ops)

	err := mc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// Should NOT unmount or detach (volume controller owns it)
	assert.Empty(t, ops.unmounts)
	assert.Empty(t, ops.detachedLoops)

	// Mount state should still be cleaned up from mount controller's perspective
	assert.Nil(t, state.GetMount("disk_mount/mnt-orphan-uni"))
}

// TestDiskMountControllerTeardownReleasesWithCloudId asserts the orphan-cleanup
// lease release uses the volume's cloud id, not the local entity id stored on
// the mount. The two are deliberately different here (MIR-1634).
func TestDiskMountControllerTeardownReleasesWithCloudId(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)
	state := NewState()
	ops := newMockDiskMountOps()

	state.SetVolume("disk_volume/vol-acc", &VolumeState{
		EntityId:      "disk_volume/vol-acc",
		VolumeId:      "vol-acc",
		DiskPath:      t.TempDir(),
		CloudVolumeId: "vol-cloud-acc",
		Mode:          storage_v1alpha.VM_ACCELERATOR,
	})
	mountState := &MountState{
		EntityId:   "disk_mount/mnt-acc",
		VolumeId:   "disk_volume/vol-acc",
		DevicePath: "/dev/loop3",
		Mode:       storage_v1alpha.VM_ACCELERATOR,
		LeaseNonce: "nonce-acc",
	}
	state.SetMount("disk_mount/mnt-acc", mountState)

	cloud := &mockCloudDiskClient{}
	mc := newTestDiskMountController(log, t.TempDir(), "test-node", nil, state, ops)
	mc.SetCloudClient(cloud)

	mc.teardownLocalMount(ctx, mountState)

	require.Len(t, cloud.releaseLeaseCalls, 1)
	assert.Equal(t, "vol-cloud-acc", cloud.releaseLeaseCalls[0].volumeID)
	assert.Equal(t, "nonce-acc", cloud.releaseLeaseCalls[0].nonce)
}

func TestLoopCleanupBackingIdentity(t *testing.T) {
	for _, mounted := range []bool{true, false} {
		for _, scenario := range []string{"rename", "unlink", "path-replaced", "device-reused", "filesystem-reused", "status-error", "legacy-live", "legacy-missing", "legacy-replaced"} {
			t.Run(fmt.Sprintf("%s/mounted=%t", scenario, mounted), func(t *testing.T) {
				dir := t.TempDir()
				state, err := LoadState(dir)
				require.NoError(t, err)
				original := &BackingIdentity{Device: 19, Inode: 201}
				other := &BackingIdentity{Device: 19, Inode: 299}
				image, reported := "/volumes/original/disk.img", "/volumes/original/disk.img"
				ops := newMockDiskMountOps()
				ops.loopIdentities = map[string]*BackingIdentity{"/dev/loop7": original}
				ops.fileIdentities = map[string]*BackingIdentity{image: original}
				ms := &MountState{EntityId: "mount", DevicePath: "/dev/loop7", BackingPath: image,
					BackingIdentity: original, MountPath: "/mnt/old", Mounted: mounted, CloudVolumeId: "cloud", LeaseNonce: "nonce", LeaseOwned: true}
				blocked := false
				switch scenario {
				case "rename":
					reported = "/deleted-volumes/moved/disk.img"
					ops.fileIdentityErr = os.ErrNotExist
				case "unlink":
					ops.deletedBacking = map[string]bool{image: true}
					ops.fileIdentityErr = os.ErrNotExist
				case "path-replaced":
					ops.fileIdentities[image] = other
				case "device-reused":
					ops.loopIdentities["/dev/loop7"] = other
					blocked = mounted
				case "filesystem-reused":
					ops.loopIdentities["/dev/loop7"] = &BackingIdentity{Device: 23, Inode: 201}
					blocked = mounted
				case "status-error":
					ops.loopIdentityErr = syscall.EIO
					blocked = true
				case "legacy-live":
					ms.BackingIdentity = nil
					ops.detachErr = syscall.EBUSY
				case "legacy-missing":
					ms.BackingIdentity = nil
					reported = "/deleted-volumes/untrusted/disk.img"
					ops.fileIdentityErr = os.ErrNotExist
					blocked = true
				case "legacy-replaced":
					ms.BackingIdentity = nil
					ops.fileIdentities[image] = other
					blocked = true
				}
				ops.loopBacking = map[string]string{reported: "/dev/loop7"}
				if mounted {
					require.NoError(t, ops.Mount("/dev/loop7", "/mnt/old", "ext4", false))
				}
				state.SetMount("mount", ms)
				require.NoError(t, state.Save())
				state, err = LoadState(dir)
				require.NoError(t, err)
				cloud := &mockCloudDiskClient{}
				controller := newTestDiskMountController(slog.Default(), dir, "node", nil, state, ops)
				controller.SetCloudClient(cloud)
				mount := &storage_v1alpha.DiskMount{ID: "mount", DesiredState: storage_v1alpha.DM_WANT_UNMOUNTED, ActualState: storage_v1alpha.DM_MOUNTED}
				err = controller.Reconcile(t.Context(), mount, nil)
				if scenario == "legacy-live" {
					require.Error(t, err)
					require.Empty(t, cloud.releaseLeaseCalls)
					state, err = LoadState(dir)
					require.NoError(t, err)
					require.Equal(t, original, state.GetMount("mount").BackingIdentity)
					// Restart after bootstrap, then rename: recovery must use the
					// durable tuple rather than the no-longer-resolvable old path.
					ops.fileIdentityErr, ops.detachErr = os.ErrNotExist, nil
					ops.loopBacking = map[string]string{"/deleted-volumes/moved/disk.img": "/dev/loop7"}
					controller = newTestDiskMountController(slog.Default(), dir, "node", nil, state, ops)
					controller.SetCloudClient(cloud)
					err = controller.Reconcile(t.Context(), mount, nil)
				}
				if blocked {
					require.Error(t, err)
					require.Empty(t, cloud.releaseLeaseCalls)
					require.Empty(t, ops.detachedLoops)
					require.Equal(t, mounted, ops.IsMounted("/mnt/old"))
					require.Equal(t, "nonce", state.GetMount("mount").LeaseNonce)
					return
				}
				require.NoError(t, err)
				require.Nil(t, state.GetMount("mount"))
				require.Len(t, cloud.releaseLeaseCalls, 1)
				if scenario == "device-reused" || scenario == "filesystem-reused" {
					require.Empty(t, ops.detachedLoops)
					require.NotNil(t, ops.loopIdentities["/dev/loop7"], "replacement attachment survives stale cleanup")
				} else {
					require.Nil(t, ops.loopIdentities["/dev/loop7"], "owned attachment is detached despite rename/unlink")
					require.False(t, ops.IsMounted("/mnt/old"))
				}
			})
		}
	}
}

func TestDiskMountCleanupPartialProgress(t *testing.T) {
	for _, failed := range []string{"detach", "release"} {
		t.Run(failed, func(t *testing.T) {
			dir := t.TempDir()
			state, err := LoadState(dir)
			require.NoError(t, err)
			state.SetMount("mount", &MountState{
				EntityId: "mount", CloudVolumeId: "cloud", DevicePath: "/dev/lbd0",
				BackingPath: filepath.Join(dir, "disk.img"),
				MountPath:   "/mnt/volume", Mounted: true, Mode: storage_v1alpha.VM_ACCELERATOR,
				LeaseNonce: "nonce", LeaseOwned: true,
			})
			require.NoError(t, state.Save())
			ops := newMockDiskMountOps()
			ops.lbdBackings["/dev/lbd0"] = filepath.Join(dir, "disk.img")
			require.NoError(t, ops.Mount("/dev/lbd0", "/mnt/volume", "ext4", false))
			cloud := &mockCloudDiskClient{}
			if failed == "detach" {
				ops.lbdDetachErr = syscall.EBUSY
			} else {
				cloud.releaseLeaseErr = syscall.EIO
			}
			controller := newTestDiskMountController(slog.Default(), dir, "node", nil, state, ops)
			controller.SetCloudClient(cloud)
			mount := &storage_v1alpha.DiskMount{ID: "mount", DesiredState: storage_v1alpha.DM_WANT_UNMOUNTED, ActualState: storage_v1alpha.DM_MOUNTED}
			require.Error(t, controller.Reconcile(t.Context(), mount, nil))
			state, err = LoadState(dir)
			require.NoError(t, err)
			partial := state.GetMount("mount")
			require.NotNil(t, partial)
			if failed == "detach" {
				require.Equal(t, "nonce", partial.LeaseNonce)
				require.True(t, partial.LeaseOwned)
				require.Empty(t, cloud.releaseLeaseCalls)
				require.Equal(t, "/dev/lbd0", partial.DevicePath)
			} else {
				require.Empty(t, partial.DevicePath)
				require.Equal(t, "nonce", state.LeaseNonceForCloudVolume("cloud"))
			}
			require.False(t, partial.Mounted)
			ops.lbdDetachErr, cloud.releaseLeaseErr = nil, nil
			controller = newTestDiskMountController(slog.Default(), dir, "node", nil, state, ops)
			controller.SetCloudClient(cloud)
			mount.ActualState = storage_v1alpha.DM_DETACHING
			require.NoError(t, controller.Reconcile(t.Context(), mount, nil))
			require.Nil(t, state.GetMount("mount"))
			require.Len(t, ops.unmounts, 1)
			require.Len(t, ops.detachedLbds, 1)
			if failed == "detach" {
				require.Len(t, cloud.releaseLeaseCalls, 1, "released nonce must not be retried")
			} else {
				require.Len(t, cloud.releaseLeaseCalls, 2)
			}
		})
	}
}

func TestDiskMountStaleCleanupProtectsReusedDevice(t *testing.T) {
	state := NewState()
	ops := newMockDiskMountOps()
	state.SetMount("old", &MountState{EntityId: "old", DevicePath: "/dev/lbd0", BackingPath: "/old/disk.img", MountPath: "/mnt/shared", Mounted: true, Mode: storage_v1alpha.VM_ACCELERATOR})
	state.SetMount("live", &MountState{EntityId: "live", DevicePath: "/dev/lbd0", MountPath: "/mnt/live", Mounted: true, LeaseNonce: "current", CloudVolumeId: "cloud"})
	ops.lbdBackings["/dev/lbd0"] = "/live/disk.img"
	ops.mountedPaths["/mnt/live"] = true
	ops.mountDevices["/mnt/live"] = "/dev/lbd0"
	controller := newTestDiskMountController(slog.Default(), t.TempDir(), "node", nil, state, ops)
	require.NoError(t, controller.Reconcile(t.Context(), &storage_v1alpha.DiskMount{ID: "old", DesiredState: storage_v1alpha.DM_WANT_UNMOUNTED}, nil))
	require.Empty(t, ops.unmounts)
	require.Empty(t, ops.detachedLbds)
	require.True(t, state.GetMount("live").Mounted)
	require.Equal(t, "current", state.LeaseNonceForCloudVolume("cloud"))
}

func TestDiskMountCleanupMountedBackingIdentity(t *testing.T) {
	for _, mismatch := range []bool{true, false} {
		t.Run(fmt.Sprint(mismatch), func(t *testing.T) {
			dir := t.TempDir()
			realPath := filepath.Join(dir, "disk.img")
			require.NoError(t, os.WriteFile(realPath, nil, 0600))
			alias := filepath.Join(dir, "alias.img")
			require.NoError(t, os.Symlink(realPath, alias))
			state, err := LoadState(dir)
			require.NoError(t, err)
			state.SetMount("mount", &MountState{
				EntityId: "mount", CloudVolumeId: "cloud", DevicePath: "/dev/lbd0",
				BackingPath: alias, MountPath: "/mnt/volume", Mounted: true,
				Mode: storage_v1alpha.VM_ACCELERATOR, LeaseNonce: "nonce", LeaseOwned: true,
			})
			require.NoError(t, state.Save())
			ops := newMockDiskMountOps()
			ops.lbdBackings["/dev/lbd0"] = realPath
			if mismatch {
				ops.lbdBackings["/dev/lbd0"] = filepath.Join(dir, "different.img")
			}
			require.NoError(t, ops.Mount("/dev/lbd0", "/mnt/volume", "ext4", false))
			cloud := &mockCloudDiskClient{}
			controller := newTestDiskMountController(slog.Default(), dir, "node", nil, state, ops)
			controller.SetCloudClient(cloud)
			err = controller.Reconcile(t.Context(), &storage_v1alpha.DiskMount{ID: "mount", DesiredState: storage_v1alpha.DM_WANT_UNMOUNTED}, nil)
			if mismatch {
				require.Error(t, err)
				require.True(t, ops.IsMounted("/mnt/volume"))
				require.Empty(t, cloud.releaseLeaseCalls)
				reloaded, err := LoadState(dir)
				require.NoError(t, err)
				require.Equal(t, "nonce", reloaded.LeaseNonceForCloudVolume("cloud"))
			} else {
				require.NoError(t, err)
				require.False(t, ops.IsMounted("/mnt/volume"))
				require.Len(t, cloud.releaseLeaseCalls, 1)
			}
		})
	}
}

func TestLegacyLoopDeviceReuse(t *testing.T) {
	for _, scenario := range []string{"volume-owner", "mount-owner", "owner-with-unavailable-peer", "tuple-only", "missing-original", "missing-owner", "inspection-error", "mounted-collision"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			state, err := LoadState(dir)
			require.NoError(t, err)
			original := filepath.Join(dir, "original", "disk.img")
			other := filepath.Join(dir, "other", "disk.img")
			identity := &BackingIdentity{Device: 7, Inode: 29}
			ops := newMockDiskMountOps()
			ops.fileIdentities = map[string]*BackingIdentity{original: {Device: 7, Inode: 11}, other: identity}
			ops.loopIdentities = map[string]*BackingIdentity{"/dev/loop0": identity}
			state.SetMount("legacy", &MountState{EntityId: "legacy", VolumeId: "original", BackingPath: original,
				DevicePath: "/dev/loop0", MountPath: "/mnt/old", Mounted: true, CloudVolumeId: "cloud", LeaseNonce: "nonce", LeaseOwned: true})
			if scenario == "mount-owner" {
				state.SetMount("other", &MountState{EntityId: "other", VolumeId: "other", BackingPath: other, DevicePath: "/dev/loop9"})
			} else {
				state.SetVolume("other", &VolumeState{EntityId: "other", DiskPath: filepath.Dir(other), BackingIdentity: identity})
			}
			switch scenario {
			case "owner-with-unavailable-peer":
				unknown := filepath.Join(dir, "unknown", "disk.img")
				state.SetVolume("unknown", &VolumeState{EntityId: "unknown", DiskPath: filepath.Dir(unknown)})
				ops.fileIdentityErrors = map[string]error{unknown: os.ErrPermission}
			case "tuple-only":
				state.SetVolume("other", &VolumeState{EntityId: "other", BackingIdentity: identity})
			case "missing-owner":
				ops.fileIdentityErrors = map[string]error{other: os.ErrNotExist}
			case "missing-original":
				ops.fileIdentityErrors = map[string]error{original: os.ErrNotExist}
			case "inspection-error":
				ops.loopIdentityErr = syscall.EIO
			case "mounted-collision":
				require.NoError(t, ops.Mount("/dev/loop0", "/mnt/old", "ext4", false))
			}
			require.NoError(t, state.Save())
			cloud := &mockCloudDiskClient{}
			controller := newTestDiskMountController(slog.Default(), dir, "node", nil, state, ops)
			controller.SetCloudClient(cloud)
			mount := &storage_v1alpha.DiskMount{ID: "legacy", ActualState: storage_v1alpha.DM_MOUNTED, DesiredState: storage_v1alpha.DM_WANT_UNMOUNTED}
			err = controller.Reconcile(t.Context(), mount, nil)
			if scenario == "volume-owner" || scenario == "mount-owner" || scenario == "owner-with-unavailable-peer" {
				require.NoError(t, err)
				require.Nil(t, state.GetMount("legacy"))
				require.Len(t, cloud.releaseLeaseCalls, 1)
			} else {
				require.Error(t, err)
				if scenario == "inspection-error" {
					require.ErrorIs(t, err, syscall.EIO)
				}
				if scenario == "missing-owner" || scenario == "missing-original" {
					require.ErrorIs(t, err, os.ErrNotExist)
				}
				require.NotNil(t, state.GetMount("legacy"))
				require.Empty(t, cloud.releaseLeaseCalls)
			}
			require.Empty(t, ops.detachedLoops)
			current, err := ops.LoopIdentity("/dev/loop0")
			if scenario != "inspection-error" {
				require.NoError(t, err)
				require.Equal(t, identity, current, "the current backing must survive stale cleanup")
			}
		})
	}
}

func TestDiskMountCleanupKernelReality(t *testing.T) {
	for _, scenario := range []string{"reboot", "current-first", "lease-only", "inspection-error", "unknown-backing", "ambiguous"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			state, err := LoadState(dir)
			require.NoError(t, err)
			state.SetVolume("volume", &VolumeState{EntityId: "volume", DiskPath: dir, Filesystem: "ext4", Mode: storage_v1alpha.VM_ACCELERATOR})
			ops := newMockDiskMountOps()
			state.SetMount("current", &MountState{EntityId: "current", VolumeId: "volume", DevicePath: "/dev/lbd0", MountPath: "/mnt/current", Mounted: true, Mode: storage_v1alpha.VM_ACCELERATOR})
			mount := &storage_v1alpha.DiskMount{ID: "current", VolumeId: "volume", MountPath: "/mnt/current", ActualState: storage_v1alpha.DM_MOUNTED, DesiredState: storage_v1alpha.DM_WANT_UNMOUNTED}
			if scenario == "reboot" {
				mount.DesiredState = storage_v1alpha.DM_WANT_MOUNTED
				ops.unmountErr = syscall.EINVAL
				ops.lbdDetachErr = syscall.ENODEV
			} else {
				_, err := ops.LbdAttach(t.Context(), filepath.Join(dir, "disk.img"), filepath.Join(dir, "logs"))
				require.NoError(t, err)
				require.NoError(t, ops.Mount("/dev/lbd0", "/mnt/current", "ext4", false))
				state.SetMount("stale", &MountState{EntityId: "stale", DevicePath: "/dev/lbd0", MountPath: "/mnt/old", Mounted: true, Mode: storage_v1alpha.VM_ACCELERATOR})
				if scenario == "lease-only" {
					state.SetMount("stale", &MountState{EntityId: "stale", MountPath: "/mnt/current", LeaseOwned: true, LeaseNonce: "old"})
				}
				if scenario == "inspection-error" {
					ops.findLoopErr = syscall.EIO
				}
				if scenario == "unknown-backing" {
					state.DeleteVolume("volume")
				}
				if scenario == "ambiguous" {
					state.SetMount("stale", &MountState{EntityId: "stale", VolumeId: "volume", DevicePath: "/dev/lbd0", MountPath: "/mnt/current", Mounted: true})
				}
			}
			require.NoError(t, state.Save())
			state, err = LoadState(dir)
			require.NoError(t, err)
			controller := newTestDiskMountController(slog.Default(), dir, "node", nil, state, ops)
			if scenario == "inspection-error" || scenario == "unknown-backing" || scenario == "ambiguous" {
				require.Error(t, controller.Reconcile(t.Context(), mount, nil))
				require.True(t, ops.IsMounted("/mnt/current"))
				require.True(t, state.GetMount("current").Mounted)
				require.Empty(t, ops.detachedLbds)
				return
			}
			require.NoError(t, controller.Reconcile(t.Context(), mount, nil))
			if scenario == "reboot" {
				require.True(t, state.GetMount("current").Mounted)
				require.Len(t, ops.attachedLbds, 1)
			} else {
				require.False(t, ops.IsMounted("/mnt/current"), "current owner must actually be unmounted")
				require.Len(t, ops.detachedLbds, 1)
			}
		})
	}
}

func TestDiskMountTransitionsWaitForCloudUpload(t *testing.T) {
	for _, transition := range []string{"acquire", "release"} {
		t.Run(transition, func(t *testing.T) {
			dir := t.TempDir()
			state, err := LoadState(dir)
			require.NoError(t, err)
			state.SetVolume("volume", &VolumeState{EntityId: "volume", CloudVolumeId: "cloud", Mode: storage_v1alpha.VM_ACCELERATOR})
			mount := &storage_v1alpha.DiskMount{ID: "mount", VolumeId: "volume", DesiredState: storage_v1alpha.DM_WANT_MOUNTED, ActualState: storage_v1alpha.DM_PENDING}
			if transition == "release" {
				state.SetMount("mount", &MountState{EntityId: "mount", CloudVolumeId: "cloud", LeaseNonce: "current", LeaseOwned: true})
				mount.DesiredState = storage_v1alpha.DM_WANT_UNMOUNTED
			}
			cloud := &mockCloudDiskClient{}
			controller := newTestDiskMountController(slog.Default(), dir, "node", nil, state, newMockDiskMountOps())
			controller.SetCloudClient(cloud)
			unlock, err := state.LockCloudVolume(t.Context(), "cloud")
			require.NoError(t, err)
			require.NoError(t, state.SetPendingCloudUpload("cloud", "begin-succeeded"))
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			require.ErrorIs(t, controller.Reconcile(ctx, mount, nil), context.Canceled)
			unlock()
			state, err = LoadState(dir)
			require.NoError(t, err)
			controller.state = state
			require.ErrorContains(t, controller.Reconcile(t.Context(), mount, nil), "pending cloud upload")
			require.Empty(t, cloud.acquireLeaseCalls)
			require.Empty(t, cloud.releaseLeaseCalls)
			require.NoError(t, state.SetPendingCloudUpload("cloud", ""))
			if transition == "release" {
				require.NoError(t, controller.Reconcile(t.Context(), mount, nil))
				require.Len(t, cloud.releaseLeaseCalls, 1)
			} else {
				cloud.acquireLeaseErr = syscall.EIO
				require.ErrorContains(t, controller.Reconcile(t.Context(), mount, nil), "failed to acquire")
				require.Len(t, cloud.acquireLeaseCalls, 1)
			}
		})
	}
}

// Signal entry to the cancelable lock wait without timing-dependent sleeps.
type cloudLockWaitContext struct {
	context.Context
	waiting chan struct{}
}

func (c cloudLockWaitContext) Done() <-chan struct{} {
	select {
	case c.waiting <- struct{}{}:
	default:
	}
	return c.Context.Done()
}

func TestDiskMountAttachRevalidatesVolumeAfterWait(t *testing.T) {
	for _, change := range []string{"deleted", "cloud-id", "disk-path"} {
		t.Run(change, func(t *testing.T) {
			dir := t.TempDir()
			state, err := LoadState(dir)
			require.NoError(t, err)
			volume := &VolumeState{EntityId: "volume", CloudVolumeId: "cloud", DiskPath: filepath.Join(dir, "original"), Mode: storage_v1alpha.VM_ACCELERATOR}
			require.NoError(t, os.MkdirAll(volume.DiskPath, 0755))
			require.NoError(t, state.SetVolumeAndSave("volume", volume))
			controller := newTestDiskMountController(slog.Default(), dir, "node", nil, state, newMockDiskMountOps())
			controller.SetCloudClient(&mockCloudDiskClient{acquireLeaseNonce: "nonce", segmentsErr: syscall.EIO})
			unlock, err := state.LockCloudVolume(t.Context(), "cloud")
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			waiting := make(chan struct{}, 1)
			result := make(chan error, 1)
			go func() {
				result <- controller.Reconcile(cloudLockWaitContext{Context: ctx, waiting: waiting}, &storage_v1alpha.DiskMount{
					ID: "mount", VolumeId: "volume", DesiredState: storage_v1alpha.DM_WANT_MOUNTED, ActualState: storage_v1alpha.DM_PENDING,
				}, nil)
			}()
			select {
			case <-waiting:
			case <-ctx.Done():
				unlock()
				t.Fatal("attach never reached lock wait")
			}
			// Deletion/replacement finishes under the same lock while attach
			// is waiting with its original volume snapshot.
			if change == "deleted" {
				require.NoError(t, os.Rename(volume.DiskPath, filepath.Join(dir, "deleted")))
				state.DeleteVolume("volume")
			} else {
				if change == "cloud-id" {
					volume.CloudVolumeId = "replacement-cloud"
				} else {
					volume.DiskPath = filepath.Join(dir, "replacement")
				}
				state.SetVolume("volume", volume)
			}
			require.NoError(t, state.Save())
			unlock()
			var reconcileErr error
			select {
			case reconcileErr = <-result:
			case <-ctx.Done():
				t.Fatal("attach did not finish")
			}
			state, err = LoadState(dir)
			require.NoError(t, err)
			require.Nil(t, state.GetMount("mount"), "deleted/replaced volumes cannot leave a durable lease acquisition intent")
			if change == "deleted" {
				require.ErrorContains(t, reconcileErr, "not found in state")
			} else {
				require.ErrorContains(t, reconcileErr, "changed while waiting")
			}
		})
	}
}

// TestDiskMountControllerTeardownReleasesAfterVolumeGone is the robustness win
// from persisting the cloud id on the mount: orphan cleanup runs after the
// backing VolumeState has already been deleted, yet the lease is still released
// with the correct cloud id because the mount carries it (MIR-1634).
func TestDiskMountControllerTeardownReleasesAfterVolumeGone(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)
	state := NewState()
	ops := newMockDiskMountOps()

	// Deliberately no VolumeState: the volume entity/state is gone, which is
	// exactly the orphan-cleanup situation.
	mountState := &MountState{
		EntityId:      "disk_mount/mnt-acc",
		VolumeId:      "disk_volume/vol-acc",
		CloudVolumeId: "vol-cloud-acc",
		BackingPath:   filepath.Join(t.TempDir(), "disk.img"),
		DevicePath:    "/dev/loop3",
		Mode:          storage_v1alpha.VM_ACCELERATOR,
		LeaseNonce:    "nonce-acc",
	}
	state.SetMount("disk_mount/mnt-acc", mountState)

	cloud := &mockCloudDiskClient{}
	mc := newTestDiskMountController(log, t.TempDir(), "test-node", nil, state, ops)
	mc.SetCloudClient(cloud)

	mc.teardownLocalMount(ctx, mountState)

	require.Len(t, cloud.releaseLeaseCalls, 1)
	assert.Equal(t, "vol-cloud-acc", cloud.releaseLeaseCalls[0].volumeID)
	assert.Equal(t, "nonce-acc", cloud.releaseLeaseCalls[0].nonce)
}
