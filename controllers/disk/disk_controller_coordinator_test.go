package disk

import (
	"testing"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

// TestDiskController_NonCoordinator_NoVolumeCreated is the MIR-1030 repro:
// a DiskController running on a non-coordinator (distributed runner) node
// must not create a disk_volume entity when it observes a provisioning
// disk. Disks are coordinator-only today.
func TestDiskController_NonCoordinator_NoVolumeCreated(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dc := NewDiskController(log, es.EAC, compute.NewNodeId("runner-uuid"), "", false)
	dc.ForceUniversalMode()

	disk := &storage_v1alpha.Disk{
		ID:         "disk/provisioning-disk",
		Name:       "data",
		SizeGb:     10,
		Filesystem: storage_v1alpha.EXT4,
		Status:     storage_v1alpha.PROVISIONING,
	}

	_, err := es.EAC.Create(ctx, entity.New(
		entity.DBId, disk.ID,
		disk.Encode,
	).Attrs())
	require.NoError(t, err)

	meta := &entity.Meta{}
	err = dc.Create(ctx, disk, meta)
	require.NoError(t, err)

	volume, err := dc.getDiskVolumeForDisk(ctx, disk.ID)
	require.NoError(t, err)
	assert.Nil(t, volume, "non-coordinator runner must not create a disk_volume")

	assert.Equal(t, storage_v1alpha.PROVISIONING, disk.Status, "non-coordinator runner must not mutate disk status")
}

// TestDiskController_PrefersOwnVolume is the secondary MIR-1030 fix: when
// an orphaned foreign disk_volume exists alongside the controller's own
// disk_volume, getDiskVolumeForDisk must return the native one so
// handleProvisioning can finalize the disk regardless of the orphan.
func TestDiskController_PrefersOwnVolume(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dc := NewDiskController(log, es.EAC, compute.NewNodeId("miren"), "", true)
	dc.ForceUniversalMode()

	disk := &storage_v1alpha.Disk{
		ID:         "disk/with-orphan",
		Name:       "data",
		SizeGb:     10,
		Filesystem: storage_v1alpha.EXT4,
		Status:     storage_v1alpha.PROVISIONING,
	}
	_, err := es.EAC.Create(ctx, entity.New(
		entity.DBId, disk.ID,
		disk.Encode,
	).Attrs())
	require.NoError(t, err)

	// Orphan volume from a misbehaving runner (the MIR-1030 bug state).
	foreign := &storage_v1alpha.DiskVolume{
		Name:         "data",
		DiskId:       disk.ID,
		SizeGb:       10,
		Filesystem:   "ext4",
		VolumeMode:   storage_v1alpha.VM_UNIVERSAL,
		DesiredState: storage_v1alpha.DV_PRESENT,
		ActualState:  storage_v1alpha.DV_PENDING,
		NodeId:       compute.NewNodeId("runner-uuid").Id(),
	}
	_, err = es.EAC.Create(ctx, entity.New(
		entity.DBId, entity.Id("disk_volume/foreign"),
		foreign.Encode,
	).Attrs())
	require.NoError(t, err)

	// Native volume, already ready — should win on selection.
	native := &storage_v1alpha.DiskVolume{
		Name:         "data",
		DiskId:       disk.ID,
		SizeGb:       10,
		Filesystem:   "ext4",
		VolumeMode:   storage_v1alpha.VM_UNIVERSAL,
		DesiredState: storage_v1alpha.DV_PRESENT,
		ActualState:  storage_v1alpha.DV_READY,
		VolumeId:     "native-vol-id",
		NodeId:       compute.NewNodeId("miren").Id(),
	}
	_, err = es.EAC.Create(ctx, entity.New(
		entity.DBId, entity.Id("disk_volume/native"),
		native.Encode,
	).Attrs())
	require.NoError(t, err)

	volume, err := dc.getDiskVolumeForDisk(ctx, disk.ID)
	require.NoError(t, err)
	require.NotNil(t, volume)
	assert.Equal(t, entity.Id("disk_volume/native"), volume.ID, "should prefer the native volume over the orphan")

	meta := &entity.Meta{}
	err = dc.Create(ctx, disk, meta)
	require.NoError(t, err)

	assert.Equal(t, storage_v1alpha.PROVISIONED, disk.Status, "disk should reach PROVISIONED via the native volume despite the orphan")
	assert.Equal(t, "native-vol-id", disk.VolumeId)
}

// TestDiskController_OnlyOrphan_StillCreatesNative guards against the
// deadlock described in MIR-1030: if an orphan foreign disk_volume
// exists without a native one, the coordinator must still create its
// own native volume rather than skip.
func TestDiskController_OnlyOrphan_StillCreatesNative(t *testing.T) {
	ctx := t.Context()
	log := testutils.TestLogger(t)

	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	dc := NewDiskController(log, es.EAC, compute.NewNodeId("miren"), "", true)
	dc.ForceUniversalMode()

	disk := &storage_v1alpha.Disk{
		ID:         "disk/orphan-only",
		Name:       "data",
		SizeGb:     10,
		Filesystem: storage_v1alpha.EXT4,
		Status:     storage_v1alpha.PROVISIONING,
	}
	_, err := es.EAC.Create(ctx, entity.New(
		entity.DBId, disk.ID,
		disk.Encode,
	).Attrs())
	require.NoError(t, err)

	foreign := &storage_v1alpha.DiskVolume{
		Name:         "data",
		DiskId:       disk.ID,
		SizeGb:       10,
		Filesystem:   "ext4",
		VolumeMode:   storage_v1alpha.VM_UNIVERSAL,
		DesiredState: storage_v1alpha.DV_PRESENT,
		ActualState:  storage_v1alpha.DV_READY,
		NodeId:       compute.NewNodeId("runner-uuid").Id(),
	}
	_, err = es.EAC.Create(ctx, entity.New(
		entity.DBId, entity.Id("disk_volume/foreign-only"),
		foreign.Encode,
	).Attrs())
	require.NoError(t, err)

	meta := &entity.Meta{}
	err = dc.Create(ctx, disk, meta)
	require.NoError(t, err)

	// Native volume should now exist, owned by the coordinator.
	indexAttr := entity.Ref(storage_v1alpha.DiskVolumeDiskIdId, disk.ID)
	resp, err := es.EAC.List(ctx, indexAttr)
	require.NoError(t, err)
	values := resp.Values()
	require.Len(t, values, 2, "both the orphan and a new native volume should exist")

	var sawNative bool
	for _, v := range values {
		var vol storage_v1alpha.DiskVolume
		vol.Decode(v.Entity())
		if vol.NodeId == compute.NewNodeId("miren").Id() {
			sawNative = true
		}
	}
	assert.True(t, sawNative, "coordinator must have created a native disk_volume alongside the orphan")
}
