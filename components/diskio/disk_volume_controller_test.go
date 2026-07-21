package diskio

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func newTestDiskVolumeController(log *slog.Logger, dataPath, nodeId string, eac *entityserver_v1alpha.EntityAccessClient, state *State, ops DiskVolumeOps) *DiskVolumeController {
	mntOps := newMockDiskMountOps()
	vc := NewDiskVolumeController(log, dataPath, compute.NewNodeId(nodeId), state, ops, mntOps)
	vc.SetEAC(eac)
	return vc
}

func createDiskVolumeEntity(ctx context.Context, t *testing.T, es *testutils.InMemEntityServer, vol *storage_v1alpha.DiskVolume) {
	_, err := es.EAC.Create(ctx, entity.New(
		entity.DBId, vol.ID,
		vol.Encode,
	).Attrs())
	require.NoError(t, err)
}

func TestDiskVolumeControllerReconcileVolumePresent(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskVolumeOps()

	vc := newTestDiskVolumeController(log, dataPath, nodeId, es.EAC, state, ops)

	vol := &storage_v1alpha.DiskVolume{
		ID:           "disk_volume/vol-123",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		SizeGb:       10,
		Filesystem:   "ext4",
		DesiredState: storage_v1alpha.DV_PRESENT,
		ActualState:  storage_v1alpha.DV_PENDING,
	}
	createDiskVolumeEntity(ctx, t, es, vol)

	err := vc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// Verify volume directory was created
	assert.Len(t, ops.createdDirs, 1)
	expectedVolPath := filepath.Join(dataPath, "volumes", "vol-123")
	assert.Equal(t, expectedVolPath, ops.createdDirs[0])

	// Verify sparse disk image was created
	assert.Len(t, ops.createdImages, 1)
	assert.Equal(t, filepath.Join(expectedVolPath, "disk.img"), ops.createdImages[0].path)
	assert.Equal(t, int64(10*1024*1024*1024), ops.createdImages[0].sizeBytes)

	// Verify state was updated
	volState := state.GetVolume("disk_volume/vol-123")
	require.NotNil(t, volState)
	assert.Equal(t, "disk_volume/vol-123", volState.EntityId)
	assert.Equal(t, "vol-123", volState.VolumeId)
	assert.Equal(t, int64(10*1024*1024*1024), volState.SizeBytes)
	assert.Equal(t, "ext4", volState.Filesystem)

	// Verify entity was updated to READY
	resp, err := es.EAC.Get(ctx, "disk_volume/vol-123")
	require.NoError(t, err)
	var updated storage_v1alpha.DiskVolume
	updated.Decode(resp.Entity().Entity())
	assert.Equal(t, storage_v1alpha.DV_READY, updated.ActualState)
	assert.Equal(t, "vol-123", updated.VolumeId)
	assert.Equal(t, filepath.Join(expectedVolPath, "disk.img"), updated.ImagePath)
}

func TestDiskVolumeControllerReconcileVolumeAbsent(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskVolumeOps()

	// Pre-populate state with existing volume using a real temp directory
	// so that metadata can be written before the move.
	volDir := filepath.Join(dataPath, "volumes", "vol-456")
	require.NoError(t, os.MkdirAll(volDir, 0755))

	state.SetVolume("disk_volume/vol-456", &VolumeState{
		EntityId:   "disk_volume/vol-456",
		VolumeId:   "vol-456",
		DiskPath:   volDir,
		SizeBytes:  5 * 1024 * 1024 * 1024,
		Filesystem: "xfs",
	})
	ops.existingPaths[volDir] = true

	vc := newTestDiskVolumeController(log, dataPath, nodeId, es.EAC, state, ops)

	vol := &storage_v1alpha.DiskVolume{
		ID:           "disk_volume/vol-456",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		SizeGb:       5,
		Filesystem:   "xfs",
		DesiredState: storage_v1alpha.DV_ABSENT,
		ActualState:  storage_v1alpha.DV_READY,
	}
	createDiskVolumeEntity(ctx, t, es, vol)

	err := vc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// Verify volume directory was soft-deleted (moved, not removed)
	assert.Len(t, ops.movedDirs, 1)
	assert.Equal(t, volDir, ops.movedDirs[0].src)
	assert.Contains(t, ops.movedDirs[0].dst, "deleted-volumes")

	// Verify state was cleaned up
	assert.Nil(t, state.GetVolume("disk_volume/vol-456"))

	// Verify entity was updated to DELETED
	resp, err := es.EAC.Get(ctx, "disk_volume/vol-456")
	require.NoError(t, err)
	var updated storage_v1alpha.DiskVolume
	updated.Decode(resp.Entity().Entity())
	assert.Equal(t, storage_v1alpha.DV_DELETED, updated.ActualState)
}

func TestDiskVolumeControllerReconcileSkipsOtherNodes(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskVolumeOps()

	vc := newTestDiskVolumeController(log, dataPath, nodeId, es.EAC, state, ops)

	vol := &storage_v1alpha.DiskVolume{
		ID:           "disk_volume/vol-other",
		NodeId:       compute.NewNodeId("other-node").Id(),
		SizeGb:       10,
		DesiredState: storage_v1alpha.DV_PRESENT,
		ActualState:  storage_v1alpha.DV_PENDING,
	}
	createDiskVolumeEntity(ctx, t, es, vol)

	err := vc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	assert.Empty(t, ops.createdDirs)
	assert.Empty(t, ops.createdImages)
}

func TestDiskVolumeControllerReconcileVolumeAlreadyReady(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskVolumeOps()

	volPath := filepath.Join(dataPath, "volumes", "vol-ready")
	state.SetVolume("disk_volume/vol-ready", &VolumeState{
		EntityId:   "disk_volume/vol-ready",
		VolumeId:   "vol-ready",
		DiskPath:   volPath,
		SizeBytes:  10 * 1024 * 1024 * 1024,
		Filesystem: "ext4",
	})
	ops.existingPaths[volPath] = true

	vc := newTestDiskVolumeController(log, dataPath, nodeId, es.EAC, state, ops)

	vol := &storage_v1alpha.DiskVolume{
		ID:           "disk_volume/vol-ready",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		SizeGb:       10,
		Filesystem:   "ext4",
		DesiredState: storage_v1alpha.DV_PRESENT,
		ActualState:  storage_v1alpha.DV_READY,
		VolumeId:     "vol-ready",
	}
	createDiskVolumeEntity(ctx, t, es, vol)

	err := vc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	assert.Empty(t, ops.createdDirs)
	assert.Empty(t, ops.createdImages)
}

func TestDiskVolumeControllerReconcileVolumeReadyButMissing(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskVolumeOps()

	// State says volume exists, but path does NOT exist on disk
	state.SetVolume("disk_volume/vol-missing", &VolumeState{
		EntityId:   "disk_volume/vol-missing",
		VolumeId:   "vol-missing",
		DiskPath:   "/data/volumes/vol-missing",
		SizeBytes:  10 * 1024 * 1024 * 1024,
		Filesystem: "ext4",
	})
	// Do NOT mark path as existing

	vc := newTestDiskVolumeController(log, dataPath, nodeId, es.EAC, state, ops)

	vol := &storage_v1alpha.DiskVolume{
		ID:           "disk_volume/vol-missing",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		SizeGb:       10,
		Filesystem:   "ext4",
		DesiredState: storage_v1alpha.DV_PRESENT,
		ActualState:  storage_v1alpha.DV_READY,
		VolumeId:     "vol-missing",
	}
	createDiskVolumeEntity(ctx, t, es, vol)

	// Reconciliation should fail and set error state
	err := vc.ReconcileWithEntities(ctx)
	require.NoError(t, err) // ReconcileWithEntities logs errors but doesn't return them

	// Verify entity was set to error state
	resp, err := es.EAC.Get(ctx, "disk_volume/vol-missing")
	require.NoError(t, err)
	var updated storage_v1alpha.DiskVolume
	updated.Decode(resp.Entity().Entity())
	assert.Equal(t, storage_v1alpha.DV_ERROR, updated.ActualState)
	assert.Contains(t, updated.ErrorMessage, "volume directory missing")
}

func TestDiskVolumeControllerReconcileVolumeErrorRetry(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskVolumeOps()

	vc := newTestDiskVolumeController(log, dataPath, nodeId, es.EAC, state, ops)

	vol := &storage_v1alpha.DiskVolume{
		ID:           "disk_volume/vol-err",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		SizeGb:       10,
		Filesystem:   "ext4",
		DesiredState: storage_v1alpha.DV_PRESENT,
		ActualState:  storage_v1alpha.DV_ERROR,
	}
	createDiskVolumeEntity(ctx, t, es, vol)

	err := vc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// Should have recreated the volume
	assert.Len(t, ops.createdDirs, 1)
	assert.Len(t, ops.createdImages, 1)
}

func TestDiskVolumeControllerReconcileCleansUpOrphanedVolumes(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskVolumeOps()

	// Pre-populate local state with a volume that has no corresponding entity
	state.SetVolume("disk_volume/vol-orphan", &VolumeState{
		EntityId:   "disk_volume/vol-orphan",
		VolumeId:   "vol-orphan",
		DiskPath:   "/data/volumes/vol-orphan",
		SizeBytes:  10 * 1024 * 1024 * 1024,
		Filesystem: "ext4",
	})
	ops.existingPaths["/data/volumes/vol-orphan"] = true

	vc := newTestDiskVolumeController(log, dataPath, nodeId, es.EAC, state, ops)

	err := vc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// Verify volume directory was removed
	assert.Contains(t, ops.removedDirs, "/data/volumes/vol-orphan")

	// Verify state was cleaned up
	assert.Nil(t, state.GetVolume("disk_volume/vol-orphan"))
}

func TestDiskVolumeControllerReconcileKeepsNonOrphanedVolumes(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskVolumeOps()

	volPath := filepath.Join(dataPath, "volumes", "vol-keep")
	state.SetVolume("disk_volume/vol-keep", &VolumeState{
		EntityId:   "disk_volume/vol-keep",
		VolumeId:   "vol-keep",
		DiskPath:   volPath,
		SizeBytes:  10 * 1024 * 1024 * 1024,
		Filesystem: "ext4",
	})
	ops.existingPaths[volPath] = true

	// Also add an orphan
	state.SetVolume("disk_volume/vol-orphan2", &VolumeState{
		EntityId:   "disk_volume/vol-orphan2",
		VolumeId:   "vol-orphan2",
		DiskPath:   "/data/volumes/vol-orphan2",
		SizeBytes:  5 * 1024 * 1024 * 1024,
		Filesystem: "ext4",
	})
	ops.existingPaths["/data/volumes/vol-orphan2"] = true

	vc := newTestDiskVolumeController(log, dataPath, nodeId, es.EAC, state, ops)

	vol := &storage_v1alpha.DiskVolume{
		ID:           "disk_volume/vol-keep",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		SizeGb:       10,
		Filesystem:   "ext4",
		DesiredState: storage_v1alpha.DV_PRESENT,
		ActualState:  storage_v1alpha.DV_READY,
		VolumeId:     "vol-keep",
	}
	createDiskVolumeEntity(ctx, t, es, vol)

	err := vc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	assert.NotNil(t, state.GetVolume("disk_volume/vol-keep"))
	assert.Nil(t, state.GetVolume("disk_volume/vol-orphan2"))
	assert.Contains(t, ops.removedDirs, "/data/volumes/vol-orphan2")
	assert.NotContains(t, ops.removedDirs, volPath)
}

func TestDiskVolumeControllerMultipleVolumes(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskVolumeOps()

	vc := newTestDiskVolumeController(log, dataPath, nodeId, es.EAC, state, ops)

	for i := 1; i <= 3; i++ {
		vol := &storage_v1alpha.DiskVolume{
			ID:           entity.Id("disk_volume/vol-" + string(rune('0'+i))),
			NodeId:       compute.NewNodeId(nodeId).Id(),
			SizeGb:       int64(i * 10),
			Filesystem:   "ext4",
			DesiredState: storage_v1alpha.DV_PRESENT,
			ActualState:  storage_v1alpha.DV_PENDING,
		}
		createDiskVolumeEntity(ctx, t, es, vol)
	}

	err := vc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	assert.Len(t, ops.createdDirs, 3)
	assert.Len(t, ops.createdImages, 3)
	assert.Len(t, state.Volumes, 3)
}

func TestDiskVolumeControllerReconcilePersistedVolumeOnDisk(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskVolumeOps()

	volPath := filepath.Join(dataPath, "volumes", "vol-persist")
	// State has disk path and it exists on disk
	state.SetVolume("disk_volume/vol-persist", &VolumeState{
		EntityId:   "disk_volume/vol-persist",
		VolumeId:   "vol-persist",
		DiskPath:   volPath,
		SizeBytes:  10 * 1024 * 1024 * 1024,
		Filesystem: "ext4",
	})
	ops.existingPaths[volPath] = true

	vc := newTestDiskVolumeController(log, dataPath, nodeId, es.EAC, state, ops)

	// Entity is in PENDING state but local state has the volume on disk
	vol := &storage_v1alpha.DiskVolume{
		ID:           "disk_volume/vol-persist",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		SizeGb:       10,
		Filesystem:   "ext4",
		DesiredState: storage_v1alpha.DV_PRESENT,
		ActualState:  storage_v1alpha.DV_PENDING,
	}
	createDiskVolumeEntity(ctx, t, es, vol)

	err := vc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// Should NOT recreate (state found on disk), just update entity
	assert.Empty(t, ops.createdDirs)
	assert.Empty(t, ops.createdImages)

	// Entity should now be READY
	resp, err := es.EAC.Get(ctx, "disk_volume/vol-persist")
	require.NoError(t, err)
	var updated storage_v1alpha.DiskVolume
	updated.Decode(resp.Entity().Entity())
	assert.Equal(t, storage_v1alpha.DV_READY, updated.ActualState)
}

func TestDiskVolumeControllerDeleteNotInState(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	ops := newMockDiskVolumeOps()

	vc := newTestDiskVolumeController(log, dataPath, nodeId, es.EAC, state, ops)

	// Volume not in local state but entity requests deletion
	vol := &storage_v1alpha.DiskVolume{
		ID:           "disk_volume/vol-gone",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		SizeGb:       10,
		DesiredState: storage_v1alpha.DV_ABSENT,
		ActualState:  storage_v1alpha.DV_READY,
	}
	createDiskVolumeEntity(ctx, t, es, vol)

	err := vc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// Should still transition to DELETED even without local state
	resp, err := es.EAC.Get(ctx, "disk_volume/vol-gone")
	require.NoError(t, err)
	var updated storage_v1alpha.DiskVolume
	updated.Decode(resp.Entity().Entity())
	assert.Equal(t, storage_v1alpha.DV_DELETED, updated.ActualState)
}

func TestDiskVolumeControllerUniversalMountAtCreation(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	volOps := newMockDiskVolumeOps()
	mntOps := newMockDiskMountOps()

	vc := NewDiskVolumeController(log, dataPath, compute.NewNodeId(nodeId), state, volOps, mntOps)
	vc.SetEAC(es.EAC)

	vol := &storage_v1alpha.DiskVolume{
		ID:           "disk_volume/vol-uni",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		SizeGb:       10,
		Filesystem:   "ext4",
		VolumeMode:   storage_v1alpha.VM_UNIVERSAL,
		DesiredState: storage_v1alpha.DV_PRESENT,
		ActualState:  storage_v1alpha.DV_PENDING,
	}
	createDiskVolumeEntity(ctx, t, es, vol)

	err := vc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// Verify volume was created
	assert.Len(t, volOps.createdDirs, 1)
	assert.Len(t, volOps.createdImages, 1)

	// Verify loop attach was called (alwaysMount)
	assert.Len(t, mntOps.attachedLoops, 1)

	// Verify format was called
	assert.Len(t, mntOps.formatCalls, 1)
	assert.Equal(t, "ext4", mntOps.formatCalls[0].filesystem)

	// Verify mount was called
	assert.Len(t, mntOps.mounts, 1)

	// Verify state reflects mount
	volState := state.GetVolume("disk_volume/vol-uni")
	require.NotNil(t, volState)
	assert.True(t, volState.Mounted)
	assert.Equal(t, "/dev/loop0", volState.DevicePath)
	assert.NotEmpty(t, volState.MountPath)
	assert.Equal(t, storage_v1alpha.VM_UNIVERSAL, volState.Mode)
}

func TestDiskVolumeControllerUniversalRemountOnReconcile(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	volOps := newMockDiskVolumeOps()
	mntOps := newMockDiskMountOps()

	volPath := filepath.Join(dataPath, "volumes", "vol-remount")
	mountPath := filepath.Join(dataPath, "vol-remount")

	// Pre-populate state: volume is ready but NOT mounted (simulating restart)
	state.SetVolume("disk_volume/vol-remount", &VolumeState{
		EntityId:   "disk_volume/vol-remount",
		VolumeId:   "vol-remount",
		DiskPath:   volPath,
		SizeBytes:  10 * 1024 * 1024 * 1024,
		Filesystem: "ext4",
		Mode:       storage_v1alpha.VM_UNIVERSAL,
		Mounted:    false,
		MountPath:  mountPath,
	})
	volOps.existingPaths[volPath] = true

	vc := NewDiskVolumeController(log, dataPath, compute.NewNodeId(nodeId), state, volOps, mntOps)
	vc.SetEAC(es.EAC)

	vol := &storage_v1alpha.DiskVolume{
		ID:           "disk_volume/vol-remount",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		SizeGb:       10,
		Filesystem:   "ext4",
		VolumeMode:   storage_v1alpha.VM_UNIVERSAL,
		DesiredState: storage_v1alpha.DV_PRESENT,
		ActualState:  storage_v1alpha.DV_READY,
	}
	createDiskVolumeEntity(ctx, t, es, vol)

	err := vc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// Verify loop attach was called for remount
	assert.Len(t, mntOps.attachedLoops, 1)
	assert.Len(t, mntOps.mounts, 1)

	// Verify state reflects mount
	volState := state.GetVolume("disk_volume/vol-remount")
	require.NotNil(t, volState)
	assert.True(t, volState.Mounted)
}

// TestDiskVolumeControllerUniversalAdoptsExistingLoopDevice verifies that
// when the backing disk image is already attached to a loop device in the
// kernel (e.g. left over from a SIGKILL'd miren whose container kept
// holding the old loop open), the universal-mode remount path adopts that
// existing device rather than allocating a second loop for the same file.
// Double-attach would produce two incoherent page caches and corrupt the
// filesystem.
func TestDiskVolumeControllerUniversalAdoptsExistingLoopDevice(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	volOps := newMockDiskVolumeOps()
	mntOps := newMockDiskMountOps()

	volPath := filepath.Join(dataPath, "volumes", "vol-972")
	mountPath := filepath.Join(dataPath, "vol-972")
	imagePath := filepath.Join(volPath, "disk.img")

	// Pre-populate state: volume is ready but NOT mounted (simulating
	// restart after SIGKILL). The local state may have a stale DevicePath
	// or none at all — either way, we should adopt the kernel's loop.
	state.SetVolume("disk_volume/vol-972", &VolumeState{
		EntityId:   "disk_volume/vol-972",
		VolumeId:   "vol-972",
		DiskPath:   volPath,
		SizeBytes:  10 * 1024 * 1024 * 1024,
		Filesystem: "ext4",
		Mode:       storage_v1alpha.VM_UNIVERSAL,
		Mounted:    false,
		MountPath:  mountPath,
	})
	volOps.existingPaths[volPath] = true

	// Simulate the kernel still holding a loop device for this image,
	// pre-formatted so the adoption path doesn't try to mkfs over it.
	const staleLoopDev = "/dev/loop7"
	mntOps.loopBacking = map[string]string{imagePath: staleLoopDev}
	mntOps.formattedDevs[staleLoopDev] = "ext4"

	vc := NewDiskVolumeController(log, dataPath, compute.NewNodeId(nodeId), state, volOps, mntOps)
	vc.SetEAC(es.EAC)

	vol := &storage_v1alpha.DiskVolume{
		ID:           "disk_volume/vol-972",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		SizeGb:       10,
		Filesystem:   "ext4",
		VolumeMode:   storage_v1alpha.VM_UNIVERSAL,
		DesiredState: storage_v1alpha.DV_PRESENT,
		ActualState:  storage_v1alpha.DV_READY,
	}
	createDiskVolumeEntity(ctx, t, es, vol)

	err := vc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// Critically: LoopAttach must NOT have been called. A second loop
	// device backing the same file would be a double-attach.
	assert.Empty(t, mntOps.attachedLoops, "LoopAttach must not be called when backing file is already attached")

	// The existing device should have been adopted and mounted.
	require.Len(t, mntOps.mounts, 1)
	assert.Equal(t, staleLoopDev, mntOps.mounts[0].device)

	// Pre-formatted, so FormatDevice must not be called.
	assert.Empty(t, mntOps.formatCalls, "adopted pre-formatted device must not be re-formatted")

	volState := state.GetVolume("disk_volume/vol-972")
	require.NotNil(t, volState)
	assert.True(t, volState.Mounted)
	assert.Equal(t, staleLoopDev, volState.DevicePath)
}

// TestDiskVolumeControllerUniversalFailsClosedWhenFindLoopErrors verifies
// that if the kernel's loop state cannot be read, ensureVolumeMount
// refuses to allocate a fresh loop device. Without this guard, a sysfs
// read failure would bypass the adoption check and could produce a
// double-attach.
func TestDiskVolumeControllerUniversalFailsClosedWhenFindLoopErrors(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	volOps := newMockDiskVolumeOps()
	mntOps := newMockDiskMountOps()

	volPath := filepath.Join(dataPath, "volumes", "vol-fcl")
	mountPath := filepath.Join(dataPath, "vol-fcl")

	state.SetVolume("disk_volume/vol-fcl", &VolumeState{
		EntityId:   "disk_volume/vol-fcl",
		VolumeId:   "vol-fcl",
		DiskPath:   volPath,
		SizeBytes:  10 * 1024 * 1024 * 1024,
		Filesystem: "ext4",
		Mode:       storage_v1alpha.VM_UNIVERSAL,
		Mounted:    false,
		MountPath:  mountPath,
	})
	volOps.existingPaths[volPath] = true

	// Simulate sysfs being unreadable.
	mntOps.findLoopErr = errors.New("sysfs read error")

	vc := NewDiskVolumeController(log, dataPath, compute.NewNodeId(nodeId), state, volOps, mntOps)
	vc.SetEAC(es.EAC)

	vol := &storage_v1alpha.DiskVolume{
		ID:           "disk_volume/vol-fcl",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		SizeGb:       10,
		Filesystem:   "ext4",
		VolumeMode:   storage_v1alpha.VM_UNIVERSAL,
		DesiredState: storage_v1alpha.DV_PRESENT,
		ActualState:  storage_v1alpha.DV_READY,
	}
	createDiskVolumeEntity(ctx, t, es, vol)

	// Reconcile records per-volume errors internally and returns nil,
	// but we verify the behavior via side effects.
	err := vc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// Critically: LoopAttach must NOT have been called. We failed
	// closed rather than risking a double-attach.
	assert.Empty(t, mntOps.attachedLoops, "LoopAttach must not be called when FindLoopByBacking fails")
	assert.Empty(t, mntOps.mounts, "Mount must not happen when FindLoopByBacking fails")

	volState := state.GetVolume("disk_volume/vol-fcl")
	require.NotNil(t, volState)
	assert.False(t, volState.Mounted)
}

// TestDiskVolumeControllerOrphanLoopSweep verifies that at boot, a loop
// device backing a file inside miren's volumes dir that has no
// corresponding known volume gets torn down. This catches stale kernel
// state from uncleanly-shut-down volumes that would otherwise leak
// forever.
func TestDiskVolumeControllerOrphanLoopSweep(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	volOps := newMockDiskVolumeOps()
	mntOps := newMockDiskMountOps()

	// A stale loop device backing a file inside our volumes dir, with
	// no corresponding volume entity in state.
	orphanImage := filepath.Join(dataPath, "volumes", "vol-dead", "disk.img")
	const orphanLoopDev = "/dev/loop9"
	mntOps.loopBacking = map[string]string{orphanImage: orphanLoopDev}

	// Also a loop backing a file OUTSIDE our volumes dir — the sweep
	// must leave this alone.
	const foreignLoopDev = "/dev/loop10"
	foreignImage := "/some/other/place/disk.img"
	mntOps.loopBacking[foreignImage] = foreignLoopDev

	vc := NewDiskVolumeController(log, dataPath, compute.NewNodeId(nodeId), state, volOps, mntOps)
	vc.SetEAC(es.EAC)

	err := vc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// Orphan must be detached.
	assert.Contains(t, mntOps.detachedLoops, orphanLoopDev, "orphan loop backing a miren volume file should be detached")
	// Foreign loop must be left alone.
	assert.NotContains(t, mntOps.detachedLoops, foreignLoopDev, "loop backing a file outside miren's volumes dir must not be touched")

	// Running reconcile again should not re-sweep (once per lifetime).
	before := len(mntOps.detachedLoops)
	err = vc.ReconcileWithEntities(ctx)
	require.NoError(t, err)
	assert.Equal(t, before, len(mntOps.detachedLoops), "orphan sweep must run at most once per controller lifetime")
}

// TestDiskVolumeControllerOrphanSweepUnmountsBeforeDetach verifies that
// when an orphan loop device is also backing an orphan mount, the sweep
// unmounts the filesystem before detaching the loop. Detaching a loop
// that still has a mounted filesystem returns EBUSY and leaves both
// the mount and the device behind.
func TestDiskVolumeControllerOrphanSweepUnmountsBeforeDetach(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	volOps := newMockDiskVolumeOps()
	mntOps := newMockDiskMountOps()

	orphanImage := filepath.Join(dataPath, "volumes", "vol-wedged", "disk.img")
	const orphanLoopDev = "/dev/loop9"
	orphanMountPath := "/var/lib/miren/disks/vol-wedged"

	// The kernel state: an orphan loop device backs a file under
	// miren's volumes dir, and an orphan mount under diskMountBasePath
	// is using that same device.
	mntOps.loopBacking = map[string]string{orphanImage: orphanLoopDev}
	mntOps.mountedPaths[orphanMountPath] = true
	mntOps.mountDevices[orphanMountPath] = orphanLoopDev

	vc := NewDiskVolumeController(log, dataPath, compute.NewNodeId(nodeId), state, volOps, mntOps)
	vc.SetEAC(es.EAC)

	err := vc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// Both cleanups happened.
	assert.Contains(t, mntOps.unmounts, orphanMountPath, "orphan mount should have been unmounted")
	assert.Contains(t, mntOps.detachedLoops, orphanLoopDev, "orphan loop should have been detached")

	// Critically: the unmount happened BEFORE the detach. If the
	// order flips, a real kernel would reject the detach with EBUSY.
	unmountIdx := -1
	detachIdx := -1
	for i, op := range mntOps.opsLog {
		if op == "Unmount:"+orphanMountPath {
			unmountIdx = i
		}
		if op == "LoopDetach:"+orphanLoopDev {
			detachIdx = i
		}
	}
	require.NotEqual(t, -1, unmountIdx, "Unmount missing from opsLog")
	require.NotEqual(t, -1, detachIdx, "LoopDetach missing from opsLog")
	assert.Less(t, unmountIdx, detachIdx,
		"orphan sweep must unmount the filesystem before detaching its loop device")
}

func TestDiskVolumeControllerUniversalUnmountOnDelete(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	volOps := newMockDiskVolumeOps()
	mntOps := newMockDiskMountOps()

	volDir := filepath.Join(dataPath, "volumes", "vol-del")
	require.NoError(t, os.MkdirAll(volDir, 0755))

	mountPath := filepath.Join(dataPath, "vol-del")
	state.SetVolume("disk_volume/vol-del", &VolumeState{
		EntityId:   "disk_volume/vol-del",
		VolumeId:   "vol-del",
		DiskPath:   volDir,
		SizeBytes:  10 * 1024 * 1024 * 1024,
		Filesystem: "ext4",
		Mode:       storage_v1alpha.VM_UNIVERSAL,
		DevicePath: "/dev/loop3",
		MountPath:  mountPath,
		Mounted:    true,
	})
	volOps.existingPaths[volDir] = true
	mntOps.mountedPaths[mountPath] = true

	vc := NewDiskVolumeController(log, dataPath, compute.NewNodeId(nodeId), state, volOps, mntOps)
	vc.SetEAC(es.EAC)

	vol := &storage_v1alpha.DiskVolume{
		ID:           "disk_volume/vol-del",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		SizeGb:       10,
		Filesystem:   "ext4",
		VolumeMode:   storage_v1alpha.VM_UNIVERSAL,
		DesiredState: storage_v1alpha.DV_ABSENT,
		ActualState:  storage_v1alpha.DV_READY,
	}
	createDiskVolumeEntity(ctx, t, es, vol)

	err := vc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// Verify unmount was called
	assert.Contains(t, mntOps.unmounts, mountPath)
	// Verify loop detach was called
	assert.Contains(t, mntOps.detachedLoops, "/dev/loop3")
	// Verify volume directory was soft-deleted (moved, not removed)
	require.Len(t, volOps.movedDirs, 1)
	assert.Equal(t, volDir, volOps.movedDirs[0].src)
	assert.Contains(t, volOps.movedDirs[0].dst, "deleted-volumes")
}

func TestDiskVolumeControllerShutdown(t *testing.T) {
	log := testutils.TestLogger(t)

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	volOps := newMockDiskVolumeOps()
	mntOps := newMockDiskMountOps()

	// Mount paths under diskMountBasePath (what the real system uses)
	mountPath1 := diskMountBasePath + "/vol-a"
	mountPath2 := diskMountBasePath + "/vol-b"
	accMountPath := diskMountBasePath + "/vol-acc"

	state.SetVolume("disk_volume/vol-a", &VolumeState{
		EntityId:   "disk_volume/vol-a",
		VolumeId:   "vol-a",
		DiskPath:   "/data/volumes/vol-a",
		Mode:       storage_v1alpha.VM_UNIVERSAL,
		DevicePath: "/dev/loop1",
		MountPath:  mountPath1,
		Mounted:    true,
	})
	state.SetVolume("disk_volume/vol-b", &VolumeState{
		EntityId:   "disk_volume/vol-b",
		VolumeId:   "vol-b",
		DiskPath:   "/data/volumes/vol-b",
		Mode:       storage_v1alpha.VM_UNIVERSAL,
		DevicePath: "/dev/loop2",
		MountPath:  mountPath2,
		Mounted:    true,
	})

	// Simulate kernel mount table: these are the actual mounts the kernel reports
	mntOps.mountedPaths[mountPath1] = true
	mntOps.mountDevices[mountPath1] = "/dev/loop1"
	mntOps.mountedPaths[mountPath2] = true
	mntOps.mountDevices[mountPath2] = "/dev/loop2"
	mntOps.mountedPaths[accMountPath] = true
	mntOps.mountDevices[accMountPath] = "/dev/lbd0"

	vc := NewDiskVolumeController(log, dataPath, compute.NewNodeId(nodeId), state, volOps, mntOps)

	vc.Shutdown()

	// All three mounts should be unmounted (found via kernel mount table scan)
	assert.Len(t, mntOps.unmounts, 3)
	assert.Contains(t, mntOps.unmounts, mountPath1)
	assert.Contains(t, mntOps.unmounts, mountPath2)
	assert.Contains(t, mntOps.unmounts, accMountPath)

	// Loop devices detached for universal volumes
	assert.Contains(t, mntOps.detachedLoops, "/dev/loop1")
	assert.Contains(t, mntOps.detachedLoops, "/dev/loop2")

	// LBD device detached for accelerator volume
	assert.Contains(t, mntOps.detachedLbds, "/dev/lbd0")

	// Verify volume state reflects unmounted
	volA := state.GetVolume("disk_volume/vol-a")
	require.NotNil(t, volA)
	assert.False(t, volA.Mounted)
}

func TestDiskVolumeControllerAcceleratorNoMountAtCreation(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dataPath := t.TempDir()
	nodeId := "test-node-1"
	state := NewState()
	volOps := newMockDiskVolumeOps()
	mntOps := newMockDiskMountOps()

	vc := NewDiskVolumeController(log, dataPath, compute.NewNodeId(nodeId), state, volOps, mntOps)
	vc.SetEAC(es.EAC)

	vol := &storage_v1alpha.DiskVolume{
		ID:           "disk_volume/vol-acc",
		NodeId:       compute.NewNodeId(nodeId).Id(),
		SizeGb:       10,
		Filesystem:   "ext4",
		VolumeMode:   storage_v1alpha.VM_ACCELERATOR,
		DesiredState: storage_v1alpha.DV_PRESENT,
		ActualState:  storage_v1alpha.DV_PENDING,
	}
	createDiskVolumeEntity(ctx, t, es, vol)

	err := vc.ReconcileWithEntities(ctx)
	require.NoError(t, err)

	// Volume should be created but NOT mounted
	assert.Len(t, volOps.createdDirs, 2) // volume dir + log dir
	assert.Len(t, volOps.createdImages, 1)
	assert.Empty(t, mntOps.attachedLoops, "accelerator mode should not mount at creation")
	assert.Empty(t, mntOps.mounts, "accelerator mode should not mount at creation")

	volState := state.GetVolume("disk_volume/vol-acc")
	require.NotNil(t, volState)
	assert.False(t, volState.Mounted)
}
