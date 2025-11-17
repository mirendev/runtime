package disk

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

func TestDiskLeaseController_DirectoryMode_Init(t *testing.T) {
	t.Run("enables directory mode when NBD unavailable", func(t *testing.T) {
		log := slog.Default()
		controller := NewDiskLeaseController(log, nil, nil)

		// Set environment to disable NBD
		t.Setenv("MIREN_DISABLE_NBD", "1")

		err := controller.Init(context.Background())
		require.NoError(t, err)

		assert.True(t, controller.directoryMode, "Directory mode should be enabled when NBD is unavailable")
	})

	t.Run("disables directory mode when NBD available", func(t *testing.T) {
		log := slog.Default()
		controller := NewDiskLeaseController(log, nil, nil)

		// Don't set MIREN_DISABLE_NBD - NBD availability depends on system
		err := controller.Init(context.Background())
		require.NoError(t, err)

		// We can't assert a specific value here since it depends on the system
		// Just verify Init doesn't error
	})
}

func TestDiskLeaseController_DirectoryMode_HandlePendingLease(t *testing.T) {
	t.Run("binds lease to directory without mounting", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()

		mockClient := NewMockLsvdClient()
		dlc := NewDiskLeaseControllerWithMountPath(log, nil, mockClient, tempDir)
		dlc.directoryMode = true

		// Create directory for the volume
		volumeId := "dir-vol-123"
		diskDataPath := filepath.Join(tempDir, "disk-data", volumeId)
		err := os.MkdirAll(diskDataPath, 0755)
		require.NoError(t, err)

		// Create a test disk
		disk := &storage_v1alpha.Disk{
			ID:           entity.Id("disk/dir-test-disk"),
			Status:       storage_v1alpha.PROVISIONED,
			LsvdVolumeId: volumeId,
			Filesystem:   storage_v1alpha.EXT4,
		}
		dlc.SetTestDisk(disk)

		// Create a pending lease
		lease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/dir-test-lease"),
			DiskId:    entity.Id("disk/dir-test-disk"),
			SandboxId: entity.Id("sandbox/dir-test-sandbox"),
			Status:    storage_v1alpha.PENDING,
			Mount: storage_v1alpha.Mount{
				Path:     "/data",
				ReadOnly: false,
			},
		}

		// Process the lease
		meta := &entity.Meta{}
		err = dlc.Create(ctx, lease, meta)
		require.NoError(t, err)

		// Should update status to BOUND
		assert.Equal(t, storage_v1alpha.BOUND, lease.Status)
		assert.Empty(t, lease.ErrorMessage)

		// Verify no volume was mounted in the mock client (directory mode)
		for _, vol := range mockClient.volumes {
			assert.NotEqual(t, VolumeStatusMounted, vol.Status, "No volumes should be mounted in directory mode")
		}

		// Verify lease is tracked
		currentLease, exists := dlc.activeLeases["disk/dir-test-disk"]
		assert.True(t, exists)
		assert.Equal(t, "disk-lease/dir-test-lease", currentLease)
	})

	t.Run("fails when directory does not exist", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()

		mockClient := NewMockLsvdClient()
		dlc := NewDiskLeaseControllerWithMountPath(log, nil, mockClient, tempDir)
		dlc.directoryMode = true

		volumeId := "missing-dir-vol"
		// Don't create the directory

		// Create a test disk
		disk := &storage_v1alpha.Disk{
			ID:           entity.Id("disk/missing-dir-disk"),
			Status:       storage_v1alpha.PROVISIONED,
			LsvdVolumeId: volumeId,
			Filesystem:   storage_v1alpha.EXT4,
		}
		dlc.SetTestDisk(disk)

		// Create a pending lease
		lease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/missing-dir-lease"),
			DiskId:    entity.Id("disk/missing-dir-disk"),
			SandboxId: entity.Id("sandbox/missing-dir-sandbox"),
			Status:    storage_v1alpha.PENDING,
			Mount: storage_v1alpha.Mount{
				Path: "/data",
			},
		}

		// Process the lease
		meta := &entity.Meta{}
		err := dlc.Create(ctx, lease, meta)
		require.NoError(t, err)

		// Should update status to FAILED
		assert.Equal(t, storage_v1alpha.FAILED, lease.Status)
		assert.Contains(t, lease.ErrorMessage, "Directory not found")

		// Verify lease is not tracked
		_, exists := dlc.activeLeases["disk/missing-dir-disk"]
		assert.False(t, exists, "Failed lease should not be tracked")
	})

	t.Run("handles disk provisioning in directory mode", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()

		mockClient := NewMockLsvdClient()
		dlc := NewDiskLeaseControllerWithMountPath(log, nil, mockClient, tempDir)
		dlc.directoryMode = true

		// Create a disk that is still provisioning
		disk := &storage_v1alpha.Disk{
			ID:           entity.Id("disk/provisioning-dir-disk"),
			Status:       storage_v1alpha.PROVISIONING,
			LsvdVolumeId: "",
			SizeGb:       10,
			Filesystem:   storage_v1alpha.EXT4,
		}
		dlc.SetTestDisk(disk)

		// Create a pending lease
		lease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/provisioning-dir-lease"),
			DiskId:    entity.Id("disk/provisioning-dir-disk"),
			SandboxId: entity.Id("sandbox/provisioning-dir-sandbox"),
			Status:    storage_v1alpha.PENDING,
			Mount: storage_v1alpha.Mount{
				Path: "/data",
			},
		}

		// First reconciliation - disk is still provisioning
		meta := &entity.Meta{}
		err := dlc.Create(ctx, lease, meta)
		require.NoError(t, err)

		// Lease should remain in PENDING state
		assert.Equal(t, storage_v1alpha.PENDING, lease.Status)

		// Now simulate disk becoming provisioned
		volumeId := "provisioned-dir-vol"
		disk.Status = storage_v1alpha.PROVISIONED
		disk.LsvdVolumeId = volumeId

		// Create the directory
		diskDataPath := filepath.Join(tempDir, "disk-data", volumeId)
		err = os.MkdirAll(diskDataPath, 0755)
		require.NoError(t, err)

		// Second reconciliation - disk is now provisioned
		meta2 := &entity.Meta{}
		err = dlc.Create(ctx, lease, meta2)
		require.NoError(t, err)

		// Lease should now be BOUND
		assert.Equal(t, storage_v1alpha.BOUND, lease.Status)
		assert.Empty(t, lease.ErrorMessage)
	})
}

func TestDiskLeaseController_DirectoryMode_HandleBoundLease(t *testing.T) {
	t.Run("tracks bound lease without mounting in directory mode", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()

		mockClient := NewMockLsvdClient()
		dlc := NewDiskLeaseControllerWithMountPath(log, nil, mockClient, tempDir)
		dlc.directoryMode = true

		// Create directory for the volume
		volumeId := "bound-dir-vol"
		diskDataPath := filepath.Join(tempDir, "disk-data", volumeId)
		err := os.MkdirAll(diskDataPath, 0755)
		require.NoError(t, err)

		// Create a test disk
		disk := &storage_v1alpha.Disk{
			ID:           entity.Id("disk/bound-dir-disk"),
			Status:       storage_v1alpha.PROVISIONED,
			LsvdVolumeId: volumeId,
			Filesystem:   storage_v1alpha.EXT4,
		}
		dlc.SetTestDisk(disk)

		// Initially, no lease is tracked
		assert.Empty(t, dlc.activeLeases)

		// Create a bound lease (simulating what we get from EAS)
		boundLease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/bound-dir-lease"),
			DiskId:    entity.Id("disk/bound-dir-disk"),
			SandboxId: entity.Id("sandbox/bound-dir-sandbox"),
			Status:    storage_v1alpha.BOUND,
			Mount: storage_v1alpha.Mount{
				Path:     "/data",
				ReadOnly: false,
			},
		}

		// Process the bound lease
		meta := &entity.Meta{}
		err = dlc.Update(ctx, boundLease, meta)
		require.NoError(t, err)

		// Verify the lease is now tracked as active
		currentLease, hasLease := dlc.activeLeases["disk/bound-dir-disk"]
		assert.True(t, hasLease, "Bound lease should be tracked")
		assert.Equal(t, "disk-lease/bound-dir-lease", currentLease)

		// Verify no volumes were mounted
		for _, vol := range mockClient.volumes {
			assert.NotEqual(t, VolumeStatusMounted, vol.Status, "No volumes should be mounted in directory mode")
		}
	})

	t.Run("handles bound lease with missing directory gracefully", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()

		mockClient := NewMockLsvdClient()
		dlc := NewDiskLeaseControllerWithMountPath(log, nil, mockClient, tempDir)
		dlc.directoryMode = true

		volumeId := "missing-bound-dir-vol"
		// Don't create the directory

		// Create a test disk
		disk := &storage_v1alpha.Disk{
			ID:           entity.Id("disk/missing-bound-dir-disk"),
			Status:       storage_v1alpha.PROVISIONED,
			LsvdVolumeId: volumeId,
			Filesystem:   storage_v1alpha.EXT4,
		}
		dlc.SetTestDisk(disk)

		// Pre-track the lease (simulating it was bound earlier)
		dlc.activeLeases["disk/missing-bound-dir-disk"] = "disk-lease/missing-bound-dir-lease"
		dlc.leaseDetails["disk-lease/missing-bound-dir-lease"] = &leaseInfo{
			leaseId:   "disk-lease/missing-bound-dir-lease",
			diskId:    "disk/missing-bound-dir-disk",
			sandboxId: "sandbox/missing-bound-dir-sandbox",
			volumeId:  volumeId,
		}

		// Create a bound lease
		boundLease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/missing-bound-dir-lease"),
			DiskId:    entity.Id("disk/missing-bound-dir-disk"),
			SandboxId: entity.Id("sandbox/missing-bound-dir-sandbox"),
			Status:    storage_v1alpha.BOUND,
			Mount: storage_v1alpha.Mount{
				Path: "/data",
			},
		}

		// Process the bound lease - should not fail even if directory is missing
		meta := &entity.Meta{}
		err := dlc.Update(ctx, boundLease, meta)
		require.NoError(t, err)

		// In directory mode, handleBoundLease doesn't verify directory exists for already-tracked leases
		// It just returns early
		assert.Equal(t, storage_v1alpha.BOUND, boundLease.Status)
	})

	t.Run("bound lease is idempotent in directory mode", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()

		mockClient := NewMockLsvdClient()
		dlc := NewDiskLeaseControllerWithMountPath(log, nil, mockClient, tempDir)
		dlc.directoryMode = true

		// Create directory
		volumeId := "idempotent-dir-vol"
		diskDataPath := filepath.Join(tempDir, "disk-data", volumeId)
		err := os.MkdirAll(diskDataPath, 0755)
		require.NoError(t, err)

		// Create a test disk
		disk := &storage_v1alpha.Disk{
			ID:           entity.Id("disk/idempotent-dir-disk"),
			Status:       storage_v1alpha.PROVISIONED,
			LsvdVolumeId: volumeId,
			Filesystem:   storage_v1alpha.EXT4,
		}
		dlc.SetTestDisk(disk)

		// Pre-track the lease
		dlc.activeLeases["disk/idempotent-dir-disk"] = "disk-lease/idempotent-dir-lease"
		dlc.leaseDetails["disk-lease/idempotent-dir-lease"] = &leaseInfo{
			leaseId:   "disk-lease/idempotent-dir-lease",
			diskId:    "disk/idempotent-dir-disk",
			sandboxId: "sandbox/idempotent-dir-sandbox",
			volumeId:  volumeId,
		}

		// Create a bound lease
		boundLease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/idempotent-dir-lease"),
			DiskId:    entity.Id("disk/idempotent-dir-disk"),
			SandboxId: entity.Id("sandbox/idempotent-dir-sandbox"),
			Status:    storage_v1alpha.BOUND,
			Mount: storage_v1alpha.Mount{
				Path: "/data",
			},
		}

		// Process the bound lease multiple times
		for i := 0; i < 3; i++ {
			meta := &entity.Meta{}
			err := dlc.Update(ctx, boundLease, meta)
			require.NoError(t, err, "Iteration %d should succeed", i)
			assert.Equal(t, storage_v1alpha.BOUND, boundLease.Status)
		}

		// Verify lease is still tracked
		currentLease, exists := dlc.activeLeases["disk/idempotent-dir-disk"]
		assert.True(t, exists)
		assert.Equal(t, "disk-lease/idempotent-dir-lease", currentLease)

		// Verify no volumes were mounted
		assert.Empty(t, mockClient.volumes, "No volumes should be created in directory mode")
	})
}

func TestDiskLeaseController_DirectoryMode_HandleReleasedLease(t *testing.T) {
	t.Run("releases lease without unmounting in directory mode", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()

		mockClient := NewMockLsvdClient()
		dlc := NewDiskLeaseControllerWithMountPath(log, nil, mockClient, tempDir)
		dlc.directoryMode = true

		volumeId := "released-dir-vol"

		// Setup active lease
		dlc.activeLeases["disk/released-dir-disk"] = "disk-lease/released-dir-lease"
		dlc.leaseDetails["disk-lease/released-dir-lease"] = &leaseInfo{
			leaseId:   "disk-lease/released-dir-lease",
			diskId:    "disk/released-dir-disk",
			sandboxId: "sandbox/released-dir-sandbox",
			volumeId:  volumeId,
		}

		// Create a released lease
		releasedLease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/released-dir-lease"),
			DiskId:    entity.Id("disk/released-dir-disk"),
			SandboxId: entity.Id("sandbox/released-dir-sandbox"),
			Status:    storage_v1alpha.RELEASED,
		}

		// Process the release
		meta := &entity.Meta{}
		err := dlc.Update(ctx, releasedLease, meta)
		require.NoError(t, err)

		// Should remove from active leases
		_, exists := dlc.activeLeases["disk/released-dir-disk"]
		assert.False(t, exists, "Should remove released lease from active leases")

		// Should remove from lease details
		_, detailsExist := dlc.leaseDetails["disk-lease/released-dir-lease"]
		assert.False(t, detailsExist, "Should remove from lease details")

		// Verify no unmount was attempted (no volumes in mock client)
		assert.Empty(t, mockClient.volumes, "No volumes should exist in directory mode")
	})

	t.Run("release is idempotent in directory mode", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()

		mockClient := NewMockLsvdClient()
		dlc := NewDiskLeaseControllerWithMountPath(log, nil, mockClient, tempDir)
		dlc.directoryMode = true

		// Setup: No active lease (already released)
		releasedLease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/already-released-dir"),
			DiskId:    entity.Id("disk/already-released-dir"),
			SandboxId: entity.Id("sandbox/released-dir-sandbox"),
			Status:    storage_v1alpha.RELEASED,
		}

		// Process the release multiple times - should be idempotent
		for i := 0; i < 3; i++ {
			meta := &entity.Meta{}
			err := dlc.Update(ctx, releasedLease, meta)
			require.NoError(t, err, "Iteration %d should succeed", i)
		}

		// Verify no lease is tracked
		_, exists := dlc.activeLeases["disk/already-released-dir"]
		assert.False(t, exists, "No lease should be tracked")
	})
}

func TestDiskLeaseController_DirectoryMode_Delete(t *testing.T) {
	t.Run("deletes lease without unmounting in directory mode", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()

		mockClient := NewMockLsvdClient()
		dlc := NewDiskLeaseControllerWithMountPath(log, nil, mockClient, tempDir)
		dlc.directoryMode = true

		volumeId := "delete-dir-vol"

		// Setup active lease and lease details
		dlc.activeLeases["disk/delete-dir-disk"] = "disk-lease/delete-dir-lease"
		dlc.leaseDetails["disk-lease/delete-dir-lease"] = &leaseInfo{
			leaseId:   "disk-lease/delete-dir-lease",
			diskId:    "disk/delete-dir-disk",
			sandboxId: "sandbox/delete-dir-sandbox",
			volumeId:  volumeId,
		}

		// Process the deletion
		err := dlc.Delete(ctx, entity.Id("disk-lease/delete-dir-lease"))
		require.NoError(t, err)

		// Should remove from active leases
		_, exists := dlc.activeLeases["disk/delete-dir-disk"]
		assert.False(t, exists, "Should remove lease from active leases")

		// Should remove from lease details
		_, detailsExist := dlc.leaseDetails["disk-lease/delete-dir-lease"]
		assert.False(t, detailsExist, "Should remove lease from lease details")

		// Verify no unmount was attempted
		assert.Empty(t, mockClient.volumes, "No volumes should exist in directory mode")
	})

	t.Run("delete is idempotent in directory mode", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()

		mockClient := NewMockLsvdClient()
		dlc := NewDiskLeaseControllerWithMountPath(log, nil, mockClient, tempDir)
		dlc.directoryMode = true

		leaseId := entity.Id("disk-lease/idempotent-delete-dir")

		// First deletion - lease doesn't exist
		err := dlc.Delete(ctx, leaseId)
		require.NoError(t, err)

		// Second deletion - should still work
		err = dlc.Delete(ctx, leaseId)
		require.NoError(t, err)
	})
}

func TestDiskLeaseController_DirectoryMode_Integration(t *testing.T) {
	t.Run("full lifecycle in directory mode", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()

		mockClient := NewMockLsvdClient()
		dlc := NewDiskLeaseControllerWithMountPath(log, nil, mockClient, tempDir)
		dlc.directoryMode = true

		// Create directory for the volume
		volumeId := "lifecycle-dir-vol"
		diskDataPath := filepath.Join(tempDir, "disk-data", volumeId)
		err := os.MkdirAll(diskDataPath, 0755)
		require.NoError(t, err)

		// Create a test disk
		disk := &storage_v1alpha.Disk{
			ID:           entity.Id("disk/lifecycle-dir-disk"),
			Status:       storage_v1alpha.PROVISIONED,
			LsvdVolumeId: volumeId,
			Filesystem:   storage_v1alpha.EXT4,
		}
		dlc.SetTestDisk(disk)

		// Step 1: Create and bind a pending lease
		lease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/lifecycle-dir-lease"),
			DiskId:    entity.Id("disk/lifecycle-dir-disk"),
			SandboxId: entity.Id("sandbox/lifecycle-dir-sandbox"),
			Status:    storage_v1alpha.PENDING,
			Mount: storage_v1alpha.Mount{
				Path: "/data",
			},
		}

		meta := &entity.Meta{}
		err = dlc.Create(ctx, lease, meta)
		require.NoError(t, err)

		// Verify lease is bound
		assert.Equal(t, storage_v1alpha.BOUND, lease.Status)
		currentLease, exists := dlc.activeLeases["disk/lifecycle-dir-disk"]
		assert.True(t, exists)
		assert.Equal(t, "disk-lease/lifecycle-dir-lease", currentLease)

		// Step 2: Update bound lease (should be idempotent)
		lease.Status = storage_v1alpha.BOUND
		meta2 := &entity.Meta{}
		err = dlc.Update(ctx, lease, meta2)
		require.NoError(t, err)
		assert.Equal(t, storage_v1alpha.BOUND, lease.Status)

		// Step 3: Release the lease
		lease.Status = storage_v1alpha.RELEASED
		meta3 := &entity.Meta{}
		err = dlc.Update(ctx, lease, meta3)
		require.NoError(t, err)

		// Verify lease is released
		_, exists = dlc.activeLeases["disk/lifecycle-dir-disk"]
		assert.False(t, exists)

		// Step 4: Delete the lease
		err = dlc.Delete(ctx, lease.ID)
		require.NoError(t, err)

		// Verify complete cleanup
		assert.Empty(t, dlc.activeLeases)
		assert.Empty(t, dlc.leaseDetails)
		assert.Empty(t, mockClient.volumes, "No volumes should have been created in directory mode")
	})

	t.Run("multiple concurrent leases in directory mode", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()

		mockClient := NewMockLsvdClient()
		dlc := NewDiskLeaseControllerWithMountPath(log, nil, mockClient, tempDir)
		dlc.directoryMode = true

		// Create multiple disks and directories
		numDisks := 3
		for i := 0; i < numDisks; i++ {
			volumeId := "multi-dir-vol-" + string(rune('a'+i))
			diskDataPath := filepath.Join(tempDir, "disk-data", volumeId)
			err := os.MkdirAll(diskDataPath, 0755)
			require.NoError(t, err)

			diskId := "disk/multi-dir-disk-" + string(rune('a'+i))
			disk := &storage_v1alpha.Disk{
				ID:           entity.Id(diskId),
				Status:       storage_v1alpha.PROVISIONED,
				LsvdVolumeId: volumeId,
				Filesystem:   storage_v1alpha.EXT4,
			}
			dlc.SetTestDisk(disk)

			// Create and bind lease
			leaseId := "disk-lease/multi-dir-lease-" + string(rune('a'+i))
			lease := &storage_v1alpha.DiskLease{
				ID:        entity.Id(leaseId),
				DiskId:    entity.Id(diskId),
				SandboxId: entity.Id("sandbox/multi-dir-sandbox"),
				Status:    storage_v1alpha.PENDING,
				Mount: storage_v1alpha.Mount{
					Path: "/data" + string(rune('a'+i)),
				},
			}

			meta := &entity.Meta{}
			err = dlc.Create(ctx, lease, meta)
			require.NoError(t, err)
			assert.Equal(t, storage_v1alpha.BOUND, lease.Status)
		}

		// Verify all leases are tracked
		assert.Equal(t, numDisks, len(dlc.activeLeases))
		assert.Equal(t, numDisks, len(dlc.leaseDetails))

		// Release all leases
		for i := 0; i < numDisks; i++ {
			leaseId := "disk-lease/multi-dir-lease-" + string(rune('a'+i))
			diskId := "disk/multi-dir-disk-" + string(rune('a'+i))

			lease := &storage_v1alpha.DiskLease{
				ID:        entity.Id(leaseId),
				DiskId:    entity.Id(diskId),
				SandboxId: entity.Id("sandbox/multi-dir-sandbox"),
				Status:    storage_v1alpha.RELEASED,
			}

			meta := &entity.Meta{}
			err := dlc.Update(ctx, lease, meta)
			require.NoError(t, err)
		}

		// Verify all leases are released
		assert.Empty(t, dlc.activeLeases)
		assert.Empty(t, dlc.leaseDetails)
	})
}

func TestDiskLeaseController_DirectoryMode_WithNormalMode(t *testing.T) {
	t.Run("directory mode does not affect LSVD mounting when disabled", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()

		mockClient := NewMockLsvdClient()
		dlc := NewDiskLeaseControllerWithMountPath(log, nil, mockClient, tempDir)
		dlc.directoryMode = false // Normal mode

		volumeId := "normal-mode-vol"

		// Add volume to mock client
		mockClient.volumes[volumeId] = VolumeInfo{
			ID:         volumeId,
			Name:       volumeId,
			SizeBytes:  10 * 1024 * 1024 * 1024,
			Status:     VolumeStatusOnDisk,
			Filesystem: "ext4",
		}

		// Create a test disk
		disk := &storage_v1alpha.Disk{
			ID:           entity.Id("disk/normal-mode-disk"),
			Status:       storage_v1alpha.PROVISIONED,
			LsvdVolumeId: volumeId,
			Filesystem:   storage_v1alpha.EXT4,
		}
		dlc.SetTestDisk(disk)

		// Create a pending lease
		lease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/normal-mode-lease"),
			DiskId:    entity.Id("disk/normal-mode-disk"),
			SandboxId: entity.Id("sandbox/normal-mode-sandbox"),
			Status:    storage_v1alpha.PENDING,
			Mount: storage_v1alpha.Mount{
				Path: "/data",
			},
		}

		// Process the lease
		meta := &entity.Meta{}
		err := dlc.Create(ctx, lease, meta)
		require.NoError(t, err)

		// In normal mode, should mount the volume
		assert.Equal(t, storage_v1alpha.BOUND, lease.Status)

		// Verify volume was mounted
		vol := mockClient.volumes[volumeId]
		assert.Equal(t, VolumeStatusMounted, vol.Status, "Volume should be mounted in normal mode")
		assert.NotEmpty(t, vol.MountPath, "Mount path should be set in normal mode")
	})
}
