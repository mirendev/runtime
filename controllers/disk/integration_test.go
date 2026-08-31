package disk

import (
	"context"
	"log/slog"
	"testing"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestDiskAndLeaseIntegration(t *testing.T) {
	t.Run("lease conflict detection", func(t *testing.T) {
		log := slog.Default()
		leaseController := NewDiskLeaseController(log, nil, compute.NewNodeId("test-node"), "")

		// Manually track an active lease
		leaseController.activeLeases["disk/integration-disk"] = "disk-lease/existing-lease"
		leaseController.leaseDetails["disk-lease/existing-lease"] = &leaseInfo{
			leaseId:   "disk-lease/existing-lease",
			diskId:    "disk/integration-disk",
			sandboxId: "sandbox/existing-sandbox",
			volumeId:  "test-volume-123",
		}

		// Try to bind another lease for the same disk (should stay PENDING for retry)
		conflictLease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/conflict-lease"),
			DiskId:    entity.Id("disk/integration-disk"),
			SandboxId: entity.Id("sandbox/another-sandbox"),
			Status:    storage_v1alpha.PENDING,
			Mount: storage_v1alpha.Mount{
				Path: "/data/conflict",
			},
		}

		conflictMeta := &entity.Meta{}
		err := leaseController.Create(context.Background(), conflictLease, conflictMeta)
		require.NoError(t, err)

		assert.Equal(t, storage_v1alpha.PENDING, conflictLease.Status, "Conflicting lease should stay PENDING for retry")
	})

	t.Run("lease release flow", func(t *testing.T) {
		log := slog.Default()
		leaseController := NewDiskLeaseController(log, nil, compute.NewNodeId("test-node"), "")

		// Setup active lease
		diskId := "disk/release-test-disk"
		leaseId := "disk-lease/release-test-lease"
		leaseController.activeLeases[diskId] = leaseId
		leaseController.leaseDetails[leaseId] = &leaseInfo{
			leaseId:   leaseId,
			diskId:    diskId,
			sandboxId: "sandbox/test-sandbox",
			volumeId:  "test-volume-456",
		}

		// Release the lease
		lease := &storage_v1alpha.DiskLease{
			ID:        entity.Id(leaseId),
			DiskId:    entity.Id(diskId),
			SandboxId: entity.Id("sandbox/test-sandbox"),
			Status:    storage_v1alpha.RELEASED,
		}

		releaseMeta := &entity.Meta{}
		err := leaseController.Update(context.Background(), lease, releaseMeta)
		require.NoError(t, err)

		// Verify lease is no longer tracked
		leaseController.mu.RLock()
		_, exists := leaseController.activeLeases[diskId]
		leaseController.mu.RUnlock()
		assert.False(t, exists, "Released lease should not be tracked")
	})

	t.Run("cleanup old released leases", func(t *testing.T) {
		log := slog.Default()
		leaseController := NewDiskLeaseController(log, nil, compute.NewNodeId("test-node"), "")

		// Test that cleanup doesn't fail in test mode (no EAC)
		err := leaseController.CleanupOldReleasedLeases(context.Background())
		assert.NoError(t, err, "Cleanup should handle nil EAC gracefully")
	})
}

func TestDiskControllerUpgradeProvisionedToUniversal(t *testing.T) {
	t.Run("provisioned disk with no disk_volume creates one in single cycle", func(t *testing.T) {
		ctx := t.Context()
		log := testutils.TestLogger(t)

		es, cleanup := testutils.NewInMemEntityServer(t)
		defer cleanup()

		dc := NewDiskController(log, es.EAC, compute.NewNodeId("test-node-1"), "", true)
		dc.ForceUniversalMode()

		// Create a disk entity that was provisioned under an older system:
		// Status=PROVISIONED but VolumeId empty and no disk_volume entity.
		disk := &storage_v1alpha.Disk{
			ID:         "disk/old-provisioned-disk",
			Name:       "my-data",
			SizeGb:     10,
			Filesystem: storage_v1alpha.EXT4,
			Status:     storage_v1alpha.PROVISIONED,
		}

		_, err := es.EAC.Create(ctx, entity.New(
			entity.DBId, disk.ID,
			disk.Encode,
		).Attrs())
		require.NoError(t, err)

		// Reconcile — VolumeId is empty so handleProvisioned calls handleProvisioning
		meta := &entity.Meta{}
		err = dc.Create(ctx, disk, meta)
		require.NoError(t, err)

		// Disk should now be PROVISIONING with mode set
		assert.Equal(t, storage_v1alpha.PROVISIONING, disk.Status)
		assert.Equal(t, storage_v1alpha.UNIVERSAL, disk.Mode)

		// A disk_volume entity should have been created
		volume, err := dc.getDiskVolumeForDisk(ctx, disk.ID)
		require.NoError(t, err)
		require.NotNil(t, volume, "disk_volume entity should have been created")

		assert.Equal(t, "my-data", volume.Name)
		assert.Equal(t, disk.ID, volume.DiskId)
		assert.Equal(t, int64(10), volume.SizeGb)
		assert.Equal(t, storage_v1alpha.DiskFilesystemExt4, volume.Filesystem)
		assert.Equal(t, storage_v1alpha.DV_PRESENT, volume.DesiredState)
		assert.Equal(t, storage_v1alpha.DV_PENDING, volume.ActualState)
	})

	t.Run("provisioned disk with stale volume_id and no disk_volume creates one", func(t *testing.T) {
		ctx := t.Context()
		log := testutils.TestLogger(t)

		es, cleanup := testutils.NewInMemEntityServer(t)
		defer cleanup()

		dc := NewDiskController(log, es.EAC, compute.NewNodeId("test-node-1"), "", true)
		dc.ForceUniversalMode()

		// Disk has a VolumeId (from old system) but no corresponding disk_volume entity
		disk := &storage_v1alpha.Disk{
			ID:         "disk/stale-vol-disk",
			Name:       "stale-data",
			SizeGb:     5,
			Filesystem: storage_v1alpha.EXT4,
			Status:     storage_v1alpha.PROVISIONED,
			VolumeId:   "old-volume-id-that-no-longer-exists",
		}

		_, err := es.EAC.Create(ctx, entity.New(
			entity.DBId, disk.ID,
			disk.Encode,
		).Attrs())
		require.NoError(t, err)

		meta := &entity.Meta{}
		err = dc.Create(ctx, disk, meta)
		require.NoError(t, err)

		// Should have created disk_volume in same cycle
		assert.Equal(t, storage_v1alpha.PROVISIONING, disk.Status)
		assert.Equal(t, "", disk.VolumeId, "stale volume ID should be cleared")

		volume, err := dc.getDiskVolumeForDisk(ctx, disk.ID)
		require.NoError(t, err)
		require.NotNil(t, volume, "disk_volume entity should have been created")
		assert.Equal(t, "stale-data", volume.Name)
	})

	t.Run("provisioned disk with existing ready disk_volume stays provisioned", func(t *testing.T) {
		ctx := t.Context()
		log := testutils.TestLogger(t)

		es, cleanup := testutils.NewInMemEntityServer(t)
		defer cleanup()

		dc := NewDiskController(log, es.EAC, compute.NewNodeId("test-node-1"), "", true)
		dc.ForceUniversalMode()

		disk := &storage_v1alpha.Disk{
			ID:         "disk/good-disk",
			Name:       "good-data",
			SizeGb:     10,
			Filesystem: storage_v1alpha.EXT4,
			Status:     storage_v1alpha.PROVISIONED,
			VolumeId:   "vol-ready-123",
			Mode:       storage_v1alpha.UNIVERSAL,
		}

		_, err := es.EAC.Create(ctx, entity.New(
			entity.DBId, disk.ID,
			disk.Encode,
		).Attrs())
		require.NoError(t, err)

		// Create a matching disk_volume entity that is READY
		dv := &storage_v1alpha.DiskVolume{
			ID:           "disk_volume/vol-ready-123",
			Name:         "good-data",
			DiskId:       disk.ID,
			SizeGb:       10,
			Filesystem:   "ext4",
			VolumeId:     "vol-ready-123",
			DesiredState: storage_v1alpha.DV_PRESENT,
			ActualState:  storage_v1alpha.DV_READY,
			NodeId:       compute.NewNodeId("test-node-1").Id(),
		}

		_, err = es.EAC.Create(ctx, entity.New(
			entity.DBId, dv.ID,
			dv.Encode,
		).Attrs())
		require.NoError(t, err)

		meta := &entity.Meta{}
		err = dc.Create(ctx, disk, meta)
		require.NoError(t, err)

		// Should stay PROVISIONED — no re-provisioning needed
		assert.Equal(t, storage_v1alpha.PROVISIONED, disk.Status)
		assert.Equal(t, "vol-ready-123", disk.VolumeId)
	})
}
