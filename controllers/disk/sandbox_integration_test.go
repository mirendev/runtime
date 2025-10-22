package disk

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
)

func TestSandboxDiskIntegration(t *testing.T) {
	t.Run("complete sandbox disk provisioning workflow", func(t *testing.T) {
		ctx := context.Background()
		log := slog.Default()

		// Create controllers
		diskController := NewDiskController(log, nil, nil)
		leaseController := NewDiskLeaseController(log, nil, nil)

		// Step 1: Create and provision a disk
		disk := &storage_v1alpha.Disk{
			ID:         entity.Id("disk/app-data"),
			Name:       "app-data",
			SizeGb:     200,
			Filesystem: storage_v1alpha.EXT4,
			Status:     storage_v1alpha.PROVISIONING,
			CreatedBy:  entity.Id("app/web-service"),
		}

		// Process disk provisioning
		meta := &entity.Meta{}
		err := diskController.Create(ctx, disk, meta)
		require.NoError(t, err)

		// Verify disk was provisioned
		var volumeId string
		for _, attr := range meta.Attrs() {
			if attr.ID == storage_v1alpha.DiskLsvdVolumeIdId {
				volumeId = attr.Value.String()
			}
		}
		assert.NotEmpty(t, volumeId)

		// Update disk with provisioned status
		disk.Status = storage_v1alpha.PROVISIONED
		disk.LsvdVolumeId = volumeId

		// Set disk in the lease controller's test cache
		leaseController.SetTestDisk(disk)

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
			NodeId:     entity.Id("node/worker-1"),
		}

		// Process lease binding
		leaseMeta := &entity.Meta{}
		err = leaseController.Create(ctx, lease, leaseMeta)
		require.NoError(t, err)

		// Verify lease was bound
		hasStatus := false
		for _, attr := range leaseMeta.Attrs() {
			if attr.ID == storage_v1alpha.DiskLeaseStatusId {
				hasStatus = true
				assert.Equal(t, storage_v1alpha.DiskLeaseStatusBoundId, attr.Value.Id())
			}
		}
		assert.True(t, hasStatus)

		// Step 4: Simulate sandbox accessing the disk
		t.Log("Sandbox can now access disk at /data/app")
		assert.Equal(t, "/data/app", lease.Mount.Path)
		assert.Equal(t, volumeId, disk.LsvdVolumeId)

		// Step 5: Try to create another sandbox that wants the same disk (should fail)
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
			NodeId:     entity.Id("node/worker-2"),
		}

		conflictMeta := &entity.Meta{}
		err = leaseController.Create(ctx, conflictLease, conflictMeta)
		require.NoError(t, err)

		// Verify conflict was detected
		hasFailure := false
		for _, attr := range conflictMeta.Attrs() {
			if attr.ID == storage_v1alpha.DiskLeaseStatusId {
				hasFailure = true
				assert.Equal(t, storage_v1alpha.DiskLeaseStatusFailedId, attr.Value.Id())
			}
		}
		assert.True(t, hasFailure)

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
			NodeId:     entity.Id("node/worker-2"),
		}

		newMeta := &entity.Meta{}
		err = leaseController.Create(ctx, newLease, newMeta)
		require.NoError(t, err)

		// Verify new lease was successful
		hasNewStatus := false
		for _, attr := range newMeta.Attrs() {
			if attr.ID == storage_v1alpha.DiskLeaseStatusId {
				hasNewStatus = true
				assert.Equal(t, storage_v1alpha.DiskLeaseStatusBoundId, attr.Value.Id())
			}
		}
		assert.True(t, hasNewStatus)

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

		diskController := NewDiskController(log, nil, nil)
		leaseController := NewDiskLeaseController(log, nil, nil)

		// Create multiple disks
		disks := []storage_v1alpha.Disk{
			{
				ID:         entity.Id("disk/os-disk"),
				Name:       "os-disk",
				SizeGb:     50,
				Filesystem: storage_v1alpha.EXT4,
				Status:     storage_v1alpha.PROVISIONING,
			},
			{
				ID:         entity.Id("disk/data-disk"),
				Name:       "data-disk",
				SizeGb:     500,
				Filesystem: storage_v1alpha.XFS,
				Status:     storage_v1alpha.PROVISIONING,
			},
			{
				ID:         entity.Id("disk/cache-disk"),
				Name:       "cache-disk",
				SizeGb:     100,
				Filesystem: storage_v1alpha.BTRFS,
				Status:     storage_v1alpha.PROVISIONING,
			},
		}

		// Provision all disks
		for i := range disks {
			meta := &entity.Meta{}
			err := diskController.Create(ctx, &disks[i], meta)
			require.NoError(t, err)

			// Update disk with volume ID
			for _, attr := range meta.Attrs() {
				if attr.ID == storage_v1alpha.DiskLsvdVolumeIdId {
					disks[i].LsvdVolumeId = attr.Value.String()
					disks[i].Status = storage_v1alpha.PROVISIONED
				}
			}

			// Set disk in the lease controller's test cache
			leaseController.SetTestDisk(&disks[i])
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

		for i, disk := range disks {
			lease := &storage_v1alpha.DiskLease{
				ID:        entity.Id(filepath.Join("disk-lease", disk.Name)),
				DiskId:    disk.ID,
				SandboxId: sandbox.ID,
				Status:    storage_v1alpha.PENDING,
				Mount: storage_v1alpha.Mount{
					Path:     mountPaths[i],
					Options:  "rw,noatime",
					ReadOnly: false,
				},
				AcquiredAt: now,
				NodeId:     entity.Id("node/multi-disk-node"),
			}

			meta := &entity.Meta{}
			err := leaseController.Create(ctx, lease, meta)
			require.NoError(t, err)

			// Verify lease was bound
			for _, attr := range meta.Attrs() {
				if attr.ID == storage_v1alpha.DiskLeaseStatusId {
					assert.Equal(t, storage_v1alpha.DiskLeaseStatusBoundId, attr.Value.Id())
				}
			}
		}

		// Verify all disks are leased to the same sandbox
		leaseController.mu.RLock()
		assert.Len(t, leaseController.activeLeases, 3)
		for _, disk := range disks {
			leaseId, exists := leaseController.activeLeases[disk.ID.String()]
			assert.True(t, exists)
			assert.Contains(t, leaseId, disk.Name)
		}
		leaseController.mu.RUnlock()

		t.Log("Successfully mounted multiple disks to single sandbox")
	})
}
