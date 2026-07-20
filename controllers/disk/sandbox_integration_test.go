package disk

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
)

// simulateDiskVolumeReady finds the disk_volume for a disk and sets it to DV_READY
// with a volume ID, simulating what the disk mount controller would do.
func simulateDiskVolumeReady(t *testing.T, ctx context.Context, es *testutils.InMemEntityServer, diskId entity.Id) string {
	t.Helper()

	// Find the disk_volume by disk_id index
	resp, err := es.EAC.List(ctx, entity.Ref(storage_v1alpha.DiskVolumeDiskIdId, diskId))
	require.NoError(t, err)

	values := resp.Values()
	require.Len(t, values, 1, "expected exactly 1 disk_volume for disk %s", diskId)

	var vol storage_v1alpha.DiskVolume
	vol.Decode(values[0].Entity())

	// Set volume to ready state with a volume ID
	volumeId := "test-vol-" + string(diskId)
	updateAttrs := []entity.Attr{
		entity.Ref(entity.DBId, vol.ID),
		entity.Ref(storage_v1alpha.DiskVolumeActualStateId, storage_v1alpha.DiskVolumeActualStateDvReadyId),
		entity.String(storage_v1alpha.DiskVolumeVolumeIdId, volumeId),
	}
	_, err = es.EAC.Patch(ctx, updateAttrs, 0)
	require.NoError(t, err)

	return volumeId
}

// simulateDiskMountMounted finds the disk_mount for a lease and sets it to DM_MOUNTED,
// simulating what the disk mount controller would do.
func simulateDiskMountMounted(t *testing.T, ctx context.Context, es *testutils.InMemEntityServer, leaseId entity.Id) {
	t.Helper()

	resp, err := es.EAC.List(ctx, entity.Ref(storage_v1alpha.DiskMountDiskLeaseIdId, leaseId))
	require.NoError(t, err)

	values := resp.Values()
	require.Len(t, values, 1, "expected exactly 1 disk_mount for lease %s", leaseId)

	var mnt storage_v1alpha.DiskMount
	mnt.Decode(values[0].Entity())

	updateAttrs := []entity.Attr{
		entity.Ref(entity.DBId, mnt.ID),
		entity.Ref(storage_v1alpha.DiskMountActualStateId, storage_v1alpha.DiskMountActualStateDmMountedId),
	}
	_, err = es.EAC.Patch(ctx, updateAttrs, 0)
	require.NoError(t, err)
}

// createDiskInEAC creates a disk entity in the entity store and returns it.
func createDiskInEAC(t *testing.T, ctx context.Context, es *testutils.InMemEntityServer, id entity.Id, disk *storage_v1alpha.Disk) {
	t.Helper()

	disk.ID = id
	_, err := es.EAC.Create(ctx, entity.New(
		entity.DBId, id,
		disk.Encode,
	).Attrs())
	require.NoError(t, err)
}

// updateDiskInEAC patches a disk entity in the entity store.
func updateDiskInEAC(t *testing.T, ctx context.Context, es *testutils.InMemEntityServer, disk *storage_v1alpha.Disk) {
	t.Helper()

	attrs := []entity.Attr{entity.Ref(entity.DBId, disk.ID)}
	attrs = append(attrs, entity.New(disk.Encode).Attrs()...)
	_, err := es.EAC.Patch(ctx, attrs, 0)
	require.NoError(t, err)
}

func TestSandboxDiskIntegration(t *testing.T) {
	t.Run("complete sandbox disk provisioning workflow", func(t *testing.T) {
		ctx := context.Background()
		log := slog.Default()

		es, cleanup := testutils.NewInMemEntityServer(t)
		t.Cleanup(cleanup)

		// Create controllers with real EAC and universal mode
		diskController := NewDiskController(log, es.EAC, compute.NewNodeId("test-node"), "", true)
		diskController.ForceUniversalMode()
		leaseController := NewDiskLeaseController(log, es.EAC, compute.NewNodeId("test-node"), "")
		leaseController.ForceUniversalMode()

		// Step 1: Create and provision a disk
		disk := &storage_v1alpha.Disk{
			Name:       "app-data",
			SizeGb:     200,
			Filesystem: storage_v1alpha.EXT4,
			Status:     storage_v1alpha.PROVISIONING,
			CreatedBy:  entity.Id("app/web-service"),
		}
		diskId := entity.Id("disk/app-data")
		createDiskInEAC(t, ctx, es, diskId, disk)

		// Process disk provisioning - creates disk_volume entity, stays PROVISIONING
		meta := &entity.Meta{}
		err := diskController.Create(ctx, disk, meta)
		require.NoError(t, err)
		assert.Equal(t, storage_v1alpha.PROVISIONING, disk.Status)

		// Simulate disk_volume becoming ready (normally done by disk mount controller)
		volumeId := simulateDiskVolumeReady(t, ctx, es, diskId)

		// Re-reconcile disk - sees DV_READY, sets PROVISIONED
		meta = &entity.Meta{}
		err = diskController.Update(ctx, disk, meta)
		require.NoError(t, err)
		assert.Equal(t, storage_v1alpha.PROVISIONED, disk.Status)
		assert.Equal(t, storage_v1alpha.UNIVERSAL, disk.Mode)
		assert.NotEmpty(t, disk.VolumeId)

		// Update the disk entity in EAC so lease controller can find it
		updateDiskInEAC(t, ctx, es, disk)

		// Step 2: Create a sandbox that requests the disk
		sandbox := &compute.Sandbox{
			ID:     entity.Id("sandbox/web-app"),
			Status: compute.RUNNING,
			Labels: []string{
				"app=web-service",
				"env=production",
			},
			Volume: []compute.Volume{
				{
					Name:     "app-data",
					Provider: "disk",
					Labels: types.Labels{
						types.Label{Key: "disk_id", Value: "disk/app-data"},
						types.Label{Key: "mount_path", Value: "/data/app"},
						types.Label{Key: "read_only", Value: "false"},
					},
				},
			},
		}

		// Step 3: Create a disk lease for the sandbox
		now := time.Now()
		lease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/web-app-lease"),
			DiskId:    disk.ID,
			SandboxId: sandbox.ID,
			AppId:     entity.Id("app/web-service"),
			Status:    storage_v1alpha.PENDING,
			Mount: storage_v1alpha.Mount{
				Path:     "/data/app",
				Options:  "rw,noatime",
				ReadOnly: false,
			},
			AcquiredAt: now,
			NodeId:     compute.NewNodeId("test-node").Id(),
		}

		// Process lease binding - creates disk_mount entity, stays PENDING
		leaseMeta := &entity.Meta{}
		err = leaseController.Create(ctx, lease, leaseMeta)
		require.NoError(t, err)
		assert.Equal(t, storage_v1alpha.PENDING, lease.Status)

		// Simulate disk_mount becoming mounted
		simulateDiskMountMounted(t, ctx, es, lease.ID)

		// Re-reconcile lease - sees DM_MOUNTED, sets BOUND
		leaseMeta = &entity.Meta{}
		err = leaseController.Update(ctx, lease, leaseMeta)
		require.NoError(t, err)
		assert.Equal(t, storage_v1alpha.BOUND, lease.Status)

		// Step 4: Verify sandbox can access disk
		t.Log("Sandbox can now access disk at /data/app")
		assert.Equal(t, "/data/app", lease.Mount.Path)
		assert.NotEmpty(t, volumeId)

		// Step 5: Try to create another sandbox that wants the same disk (should conflict)
		conflictSandbox := &compute.Sandbox{
			ID:     entity.Id("sandbox/conflicting-app"),
			Status: compute.RUNNING,
		}

		conflictLease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/conflict"),
			DiskId:    disk.ID,
			SandboxId: conflictSandbox.ID,
			Status:    storage_v1alpha.PENDING,
			Mount: storage_v1alpha.Mount{
				Path: "/mnt/data",
			},
			AcquiredAt: now,
			NodeId:     compute.NewNodeId("test-node").Id(),
		}

		conflictMeta := &entity.Meta{}
		err = leaseController.Create(ctx, conflictLease, conflictMeta)
		require.NoError(t, err)

		// Verify conflict was detected — lease stays PENDING for retry
		assert.Equal(t, storage_v1alpha.PENDING, conflictLease.Status, "Conflicting lease should stay PENDING for retry")

		// Step 6: Sandbox releases the disk
		lease.Status = storage_v1alpha.RELEASED

		releaseMeta := &entity.Meta{}
		err = leaseController.Update(ctx, lease, releaseMeta)
		require.NoError(t, err)

		// Step 7: Now the conflicting sandbox can acquire the disk
		newLease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/new-lease"),
			DiskId:    disk.ID,
			SandboxId: conflictSandbox.ID,
			Status:    storage_v1alpha.PENDING,
			Mount: storage_v1alpha.Mount{
				Path: "/mnt/data",
			},
			AcquiredAt: time.Now(),
			NodeId:     compute.NewNodeId("test-node").Id(),
		}

		newMeta := &entity.Meta{}
		err = leaseController.Create(ctx, newLease, newMeta)
		require.NoError(t, err)

		// New lease creates a disk_mount, stays PENDING until mounted
		assert.Equal(t, storage_v1alpha.PENDING, newLease.Status)

		// Simulate the new disk_mount becoming mounted
		simulateDiskMountMounted(t, ctx, es, newLease.ID)

		// Re-reconcile - now BOUND
		newMeta = &entity.Meta{}
		err = leaseController.Update(ctx, newLease, newMeta)
		require.NoError(t, err)
		assert.Equal(t, storage_v1alpha.BOUND, newLease.Status)

		// Step 8: Clean up - delete disk
		disk.Status = storage_v1alpha.DELETING
		deleteMeta := &entity.Meta{}
		err = diskController.Update(ctx, disk, deleteMeta)
		require.NoError(t, err)

		t.Log("Successfully completed full sandbox disk lifecycle")
	})

	t.Run("sandbox with multiple disks", func(t *testing.T) {
		ctx := context.Background()
		log := slog.Default()

		es, cleanup := testutils.NewInMemEntityServer(t)
		t.Cleanup(cleanup)

		diskController := NewDiskController(log, es.EAC, compute.NewNodeId("test-node"), "", true)
		diskController.ForceUniversalMode()
		leaseController := NewDiskLeaseController(log, es.EAC, compute.NewNodeId("test-node"), "")
		leaseController.ForceUniversalMode()

		// Create multiple disks
		type diskInfo struct {
			disk *storage_v1alpha.Disk
			id   entity.Id
		}

		diskSpecs := []struct {
			id   entity.Id
			name string
			size int64
			fs   storage_v1alpha.DiskFilesystem
		}{
			{entity.Id("disk/os-disk"), "os-disk", 50, storage_v1alpha.EXT4},
			{entity.Id("disk/data-disk"), "data-disk", 500, storage_v1alpha.XFS},
			{entity.Id("disk/cache-disk"), "cache-disk", 100, storage_v1alpha.BTRFS},
		}

		var disks []diskInfo
		for _, spec := range diskSpecs {
			disk := &storage_v1alpha.Disk{
				Name:       spec.name,
				SizeGb:     spec.size,
				Filesystem: spec.fs,
				Status:     storage_v1alpha.PROVISIONING,
			}
			createDiskInEAC(t, ctx, es, spec.id, disk)

			// Provision: creates disk_volume
			meta := &entity.Meta{}
			err := diskController.Create(ctx, disk, meta)
			require.NoError(t, err)

			// Simulate disk_volume becoming ready
			simulateDiskVolumeReady(t, ctx, es, spec.id)

			// Re-reconcile to pick up DV_READY
			meta = &entity.Meta{}
			err = diskController.Update(ctx, disk, meta)
			require.NoError(t, err)
			require.Equal(t, storage_v1alpha.PROVISIONED, disk.Status)

			// Update in EAC
			updateDiskInEAC(t, ctx, es, disk)

			disks = append(disks, diskInfo{disk: disk, id: spec.id})
		}

		// Create sandbox with multiple disk volumes
		sandbox := &compute.Sandbox{
			ID:     entity.Id("sandbox/multi-disk-app"),
			Status: compute.RUNNING,
			Volume: []compute.Volume{
				{
					Name:     "os",
					Provider: "disk",
					Labels: types.Labels{
						types.Label{Key: "disk_id", Value: "disk/os-disk"},
						types.Label{Key: "mount_path", Value: "/"},
						types.Label{Key: "read_only", Value: "false"},
					},
				},
				{
					Name:     "data",
					Provider: "disk",
					Labels: types.Labels{
						types.Label{Key: "disk_id", Value: "disk/data-disk"},
						types.Label{Key: "mount_path", Value: "/data"},
						types.Label{Key: "read_only", Value: "false"},
					},
				},
				{
					Name:     "cache",
					Provider: "disk",
					Labels: types.Labels{
						types.Label{Key: "disk_id", Value: "disk/cache-disk"},
						types.Label{Key: "mount_path", Value: "/cache"},
						types.Label{Key: "read_only", Value: "false"},
					},
				},
			},
		}

		// Create leases for all disks
		now := time.Now()
		mountPaths := []string{"/", "/data", "/cache"}

		for i, di := range disks {
			lease := &storage_v1alpha.DiskLease{
				ID:        entity.Id("disk-lease/" + di.disk.Name),
				DiskId:    di.id,
				SandboxId: sandbox.ID,
				Status:    storage_v1alpha.PENDING,
				Mount: storage_v1alpha.Mount{
					Path:     mountPaths[i],
					Options:  "rw,noatime",
					ReadOnly: false,
				},
				AcquiredAt: now,
				NodeId:     compute.NewNodeId("test-node").Id(),
			}

			meta := &entity.Meta{}
			err := leaseController.Create(ctx, lease, meta)
			require.NoError(t, err)
			assert.Equal(t, storage_v1alpha.PENDING, lease.Status)

			// Simulate disk_mount becoming mounted
			simulateDiskMountMounted(t, ctx, es, lease.ID)

			// Re-reconcile to bind
			meta = &entity.Meta{}
			err = leaseController.Update(ctx, lease, meta)
			require.NoError(t, err)
			assert.Equal(t, storage_v1alpha.BOUND, lease.Status)
		}

		// Verify all disks are leased to the same sandbox
		leaseController.mu.RLock()
		assert.Len(t, leaseController.activeLeases, 3)
		for _, di := range disks {
			leaseId, exists := leaseController.activeLeases[di.id.String()]
			assert.True(t, exists)
			assert.Contains(t, leaseId, di.disk.Name)
		}
		leaseController.mu.RUnlock()

		t.Log("Successfully mounted multiple disks to single sandbox")
	})
}
