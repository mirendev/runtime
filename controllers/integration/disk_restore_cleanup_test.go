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

// buildRestoreResidue stands up the exact state a partial Finalize (or the disk
// controller's self-heal) leaves behind when a disk_volume is committed for a
// PROVISIONED disk on the coordinator: a disk_volume entity with
// DesiredState=DV_PRESENT / ActualState=DV_READY owned by this node. It then
// reconciles the disk-volume controller once so the coordinator materializes
// the local residue the bug report describes — a recovered VolumeState, a
// volume directory, and (universal mode) a held loop-device mount.
func buildRestoreResidue(t *testing.T, h *TestHarness, diskID, volEntityID, volID string) {
	t.Helper()
	ctx := context.Background()

	// The disk is PROVISIONED and already carries the volume id, the state
	// Finalize's Patch leaves it in before the disk_volume Create (or that the
	// controller's self-heal recovers it to).
	disk := &storage.Disk{
		ID:         entity.Id(diskID),
		Name:       "restore-disk",
		SizeGb:     2,
		Filesystem: storage.EXT4,
		Status:     storage.PROVISIONED,
		VolumeId:   volID,
	}
	_, err := h.EAC.Create(ctx, entity.New(
		entity.DBId, entity.Id(diskID),
		disk.Encode,
	).Attrs())
	require.NoError(t, err)

	// The disk_volume committed on this node, in the "recovered-ready" shape.
	vol := &storage.DiskVolume{
		ID:           entity.Id(volEntityID),
		Name:         "restore-disk",
		DiskId:       entity.Id(diskID),
		VolumeId:     volID,
		SizeGb:       2,
		Filesystem:   "ext4",
		VolumeMode:   storage.VM_UNIVERSAL,
		DesiredState: storage.DV_PRESENT,
		ActualState:  storage.DV_READY,
		NodeId:       compute.NewNodeId(testNodeId).Id(),
	}
	_, err = h.EAC.Create(ctx, entity.New(
		entity.DBId, entity.Id(volEntityID),
		vol.Encode,
	).Attrs())
	require.NoError(t, err)

	// Reconcile the disk-volume controller so it "recovers" the orphaned
	// DV_READY volume: this materializes the local residue (VolumeState,
	// volume directory, and a held loop-device mount in universal mode).
	nodeId := compute.NewNodeId(testNodeId).Id()
	h.reconcileByIndex(ctx, entity.Ref(storage.DiskVolumeNodeIdId, nodeId), h.DiskVolRC)

	// Assert the residue exists before we attempt any cleanup.
	require.NotNil(t, h.DiskioState.GetVolume(volEntityID),
		"coordinator should have materialized a VolumeState for the recovered disk_volume")
	require.NotEmpty(t, h.MockMountOps.existingMounts,
		"universal volume should hold a mount the coordinator will not reclaim on its own")
	vols := listDiskVolumes(t, ctx, h)
	require.Len(t, vols, 1)
	assert.Equal(t, storage.DV_READY, vols[0].ActualState)
	assert.Equal(t, storage.DV_PRESENT, vols[0].DesiredState)
}

// TestDiskRestoreCleanup_DELETINGTearsDownResidue is the end-to-end guarantee
// the fix relies on: after a failed restore, keeping the disk alive and
// transitioning it to DELETING (the new Cleanup) drives the disk controller's
// handleDeletion -> disk_volume DV_ABSENT -> DiskVolumeController
// reconcileVolumeAbsent tear-down path, removing every piece of the residue —
// disk entity, disk_volume entity, local VolumeState, and held mount — instead
// of orphaning them.
func TestDiskRestoreCleanup_DELETINGTearsDownResidue(t *testing.T) {
	ctx := context.Background()
	h := NewTestHarness(t)

	const (
		diskID      = "disk/restore-fix"
		volEntityID = "disk_volume/restore-vol-fix"
		volID       = "restore-vol-fix"
	)
	buildRestoreResidue(t, h, diskID, volEntityID, volID)

	// This is exactly what the CLI's new Cleanup does: transition the disk
	// to DELETING, keeping it alive to drive cleanup.
	patchDiskStatus(t, ctx, h, diskID, storage.DELETING)

	// Let the controller chain converge: disk -> handleDeletion writes
	// disk_volume.desired_state=DV_ABSENT; the DiskVolumeController
	// unmounts, soft-deletes, and clears VolumeState; a subsequent disk
	// reconcile deletes the disk_volume and disk entities.
	h.ReconcileAll(ctx, 20)

	assert.Empty(t, listDisks(t, ctx, h), "disk entity should be deleted by the DELETING path")
	assert.Empty(t, listDiskVolumes(t, ctx, h), "disk_volume entity should be deleted, not orphaned")
	assert.Nil(t, h.DiskioState.GetVolume(volEntityID),
		"local VolumeState must be cleared so no residue is held on the coordinator")
	assert.Empty(t, h.MockMountOps.existingMounts,
		"the held loop-device mount must be released by the DELETING tear-down")
}
