package disk

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

func TestDiskAndLeaseIntegration(t *testing.T) {
	t.Run("complete disk lifecycle with lease", func(t *testing.T) {
		log := slog.Default()
		diskController := NewDiskController(log, nil, nil)
		leaseController := NewDiskLeaseController(log, nil, nil)
		ctx := context.Background()

		// Create a disk
		disk := &storage_v1alpha.Disk{
			ID:         entity.Id("disk/integration-disk"),
			Name:       "integration-disk",
			SizeGb:     50,
			Filesystem: storage_v1alpha.EXT4,
			Status:     storage_v1alpha.PROVISIONING,
		}

		// Handle provisioning
		diskMeta := &entity.Meta{}
		err := diskController.Create(ctx, disk, diskMeta)
		require.NoError(t, err)

		// Should set status to PROVISIONED
		hasVolumeId := false
		for _, attr := range diskMeta.Attrs() {
			if attr.ID == storage_v1alpha.DiskLsvdVolumeIdId {
				hasVolumeId = true
				assert.NotEmpty(t, attr.Value.String())
			}
			if attr.ID == storage_v1alpha.DiskStatusId {
				assert.Equal(t, storage_v1alpha.DiskStatusProvisionedId, attr.Value.Id())
			}
		}
		assert.True(t, hasVolumeId, "Should have volume ID after provisioning")

		// Update disk with the volume ID from attrs
		for _, attr := range diskMeta.Attrs() {
			if attr.ID == storage_v1alpha.DiskLsvdVolumeIdId {
				disk.LsvdVolumeId = attr.Value.String()
			}
			if attr.ID == storage_v1alpha.DiskStatusId && attr.Value.Id() == storage_v1alpha.DiskStatusProvisionedId {
				disk.Status = storage_v1alpha.PROVISIONED
			}
		}

		// Set disk in lease controller's test cache
		leaseController.SetTestDisk(disk)

		// Create a lease for the disk
		now := time.Now()
		lease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/integration-lease"),
			DiskId:    disk.ID,
			SandboxId: entity.Id("sandbox/integration-sandbox"),
			Status:    storage_v1alpha.PENDING,
			Mount: storage_v1alpha.Mount{
				Path:     "/data/integration",
				Options:  "rw,noatime",
				ReadOnly: false,
			},
			AcquiredAt: now,
			NodeId:     entity.Id("node/worker-1"),
		}

		// Bind the lease
		leaseMeta := &entity.Meta{}
		err = leaseController.Create(ctx, lease, leaseMeta)
		require.NoError(t, err)

		// Should update status to BOUND
		hasStatus := false
		for _, attr := range leaseMeta.Attrs() {
			if attr.ID == storage_v1alpha.DiskLeaseStatusId {
				hasStatus = true
				assert.Equal(t, storage_v1alpha.DiskLeaseStatusBoundId, attr.Value.Id())
			}
		}
		assert.True(t, hasStatus, "Should update status to BOUND")

		// Verify lease is tracked
		leaseController.mu.RLock()
		activeLease, exists := leaseController.activeLeases[disk.ID.String()]
		leaseController.mu.RUnlock()
		assert.True(t, exists, "Should track active lease")
		assert.Equal(t, lease.ID.String(), activeLease)

		// Try to bind another lease for the same disk (should fail)
		conflictLease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/conflict-lease"),
			DiskId:    disk.ID,
			SandboxId: entity.Id("sandbox/another-sandbox"),
			Status:    storage_v1alpha.PENDING,
			Mount: storage_v1alpha.Mount{
				Path: "/data/conflict",
			},
			AcquiredAt: now,
		}

		conflictMeta := &entity.Meta{}
		err = leaseController.Create(ctx, conflictLease, conflictMeta)
		require.NoError(t, err)

		// Should fail with conflict
		hasFailure := false
		for _, attr := range conflictMeta.Attrs() {
			if attr.ID == storage_v1alpha.DiskLeaseStatusId {
				assert.Equal(t, storage_v1alpha.DiskLeaseStatusFailedId, attr.Value.Id())
				hasFailure = true
			}
			if attr.ID == storage_v1alpha.DiskLeaseErrorMessageId {
				assert.Contains(t, attr.Value.String(), "already leased")
			}
		}
		assert.True(t, hasFailure, "Conflicting lease should fail")

		// Release the original lease
		lease.Status = storage_v1alpha.RELEASED
		releaseMeta := &entity.Meta{}
		err = leaseController.Update(ctx, lease, releaseMeta)
		require.NoError(t, err)

		// Verify lease is no longer tracked
		leaseController.mu.RLock()
		_, exists = leaseController.activeLeases[disk.ID.String()]
		leaseController.mu.RUnlock()
		assert.False(t, exists, "Released lease should not be tracked")

		// Delete the disk
		disk.Status = storage_v1alpha.DELETING
		deleteMeta := &entity.Meta{}
		err = diskController.Update(ctx, disk, deleteMeta)
		require.NoError(t, err)

		// Note: In a real implementation, the disk controller would handle cleanup
		// For testing, we're just verifying the flow completes without errors
	})

	t.Run("cleanup old released leases", func(t *testing.T) {
		log := slog.Default()
		leaseController := NewDiskLeaseController(log, nil, nil)
		ctx := context.Background()

		// Create a mock disk
		disk := &storage_v1alpha.Disk{
			ID:           entity.Id("disk/cleanup-disk"),
			Status:       storage_v1alpha.PROVISIONED,
			LsvdVolumeId: "cleanup-volume-123",
		}
		leaseController.SetTestDisk(disk)

		// Test that cleanup doesn't fail in test mode (no EAC)
		err := leaseController.CleanupOldReleasedLeases(ctx)
		assert.NoError(t, err, "Cleanup should handle nil EAC gracefully")

		// Note: In production, this would:
		// 1. List all disk leases from EAS
		// 2. Check each lease's UpdatedAt timestamp
		// 3. Delete RELEASED leases older than 1 hour
		// 4. Preserve recent leases and non-RELEASED leases
	})
}
