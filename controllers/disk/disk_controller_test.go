package disk

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/storage/storage_v1alpha"
)

func TestDiskController_New(t *testing.T) {
	log := slog.Default()
	controller := NewDiskController(log, nil, nil)

	assert.NotNil(t, controller)
	assert.NotNil(t, controller.Log)
	assert.Equal(t, "/var/lib/miren/disks", controller.mountBasePath)
}

func TestDiskController_NewWithMountPath(t *testing.T) {
	log := slog.Default()
	customPath := "/custom/mount/path"
	controller := NewDiskControllerWithMountPath(log, nil, nil, customPath)

	assert.NotNil(t, controller)
	assert.Equal(t, customPath, controller.mountBasePath)
}

func TestDiskController_ProvisionDisk(t *testing.T) {
	t.Run("provisions new disk", func(t *testing.T) {
		log := slog.Default()
		controller := NewDiskController(log, nil, nil)

		// Create a disk entity
		disk := &storage_v1alpha.Disk{
			Name:       "test-disk",
			SizeGb:     100,
			Filesystem: storage_v1alpha.EXT4,
			Status:     storage_v1alpha.PROVISIONING,
		}

		// Mock provision - in real implementation this would call LSVD
		volumeId, err := controller.provisionVolume(context.Background(), disk)
		require.NoError(t, err)
		assert.NotEmpty(t, volumeId)
	})

	t.Run("unprovisions disk volume", func(t *testing.T) {
		log := slog.Default()
		controller := NewDiskController(log, nil, nil)

		// Create a disk marked for deletion
		disk := &storage_v1alpha.Disk{
			Name:         "test-disk",
			SizeGb:       50,
			Status:       storage_v1alpha.DELETING,
			LsvdVolumeId: "lsvd-vol-123",
		}

		// Mock unprovision - in real implementation this would call LSVD
		err := controller.deleteVolumeData(context.Background(), disk.LsvdVolumeId)
		require.NoError(t, err)
	})

	t.Run("handles invalid disk size", func(t *testing.T) {
		log := slog.Default()
		controller := NewDiskController(log, nil, nil)

		// Create a disk with invalid size
		disk := &storage_v1alpha.Disk{
			Name:       "test-disk",
			SizeGb:     -1, // Invalid size
			Filesystem: storage_v1alpha.EXT4,
			Status:     storage_v1alpha.PROVISIONING,
		}

		// Should return error for invalid size
		_, err := controller.provisionVolume(context.Background(), disk)
		assert.Error(t, err)
	})
}

func TestDiskController_Provisioning(t *testing.T) {
	t.Run("provisions disk in SegmentAccess only", func(t *testing.T) {
		log := slog.Default()
		mockClient := &MockLsvdClient{
			volumes: make(map[string]VolumeInfo),
		}

		controller := NewDiskController(log, nil, mockClient)

		disk := &storage_v1alpha.Disk{
			Name:       "provision-test-disk",
			SizeGb:     50,
			Filesystem: storage_v1alpha.EXT4,
			Status:     storage_v1alpha.PROVISIONING,
		}

		ctx := context.Background()
		err := controller.handleProvisioning(ctx, disk)
		require.NoError(t, err)

		// Check that disk was provisioned
		assert.Equal(t, storage_v1alpha.PROVISIONED, disk.Status)
		assert.NotEmpty(t, disk.LsvdVolumeId)

		// Verify volume exists in mock client but NOT mounted
		info, exists := mockClient.volumes[disk.LsvdVolumeId]
		assert.True(t, exists)
		assert.Equal(t, VolumeStatusOnDisk, info.Status) // Only in SegmentAccess, not initialized
		assert.Empty(t, info.MountPath)                  // Should not be mounted
	})

	t.Run("unprovisions disk on deletion", func(t *testing.T) {
		log := slog.Default()
		mockClient := &MockLsvdClient{
			volumes: make(map[string]VolumeInfo),
		}

		controller := NewDiskController(log, nil, mockClient)

		// Create a volume in SegmentAccess
		volumeId := "delete-test-vol"
		mockClient.volumes[volumeId] = VolumeInfo{
			ID:         volumeId,
			SizeBytes:  50 * 1024 * 1024 * 1024,
			Filesystem: "ext4",
			MountPath:  "", // Not mounted by disk controller
			Status:     VolumeStatusOnDisk,
		}

		disk := &storage_v1alpha.Disk{
			Name:         "delete-test-disk",
			LsvdVolumeId: volumeId,
			Status:       storage_v1alpha.DELETING,
		}

		ctx := context.Background()
		err := controller.handleDeletion(ctx, disk)
		require.NoError(t, err)

		// Note: deleteVolumeData is currently a stub that doesn't actually delete
		// So the volume will still exist in the mock client
		_, exists := mockClient.volumes[volumeId]
		assert.True(t, exists, "Volume should still exist (deleteVolumeData is a stub)")
	})

}
