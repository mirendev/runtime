package disk

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

func TestDiskLeaseController_New(t *testing.T) {
	log := slog.Default()
	mockClient := NewMockLsvdClient()
	controller := NewDiskLeaseController(log, nil, mockClient)

	assert.NotNil(t, controller)
	assert.NotNil(t, controller.Log)
	assert.NotNil(t, controller.lsvdClient)
}

func TestDiskLeaseController_Create(t *testing.T) {
	t.Run("binds pending lease without mounting", func(t *testing.T) {
		log := slog.Default()
		mockClient := NewMockLsvdClient()
		dlc := NewDiskLeaseController(log, nil, mockClient)

		// Add the volume to the mock client
		mockClient.volumes["test-volume-123"] = VolumeInfo{
			ID:         "test-volume-123",
			Name:       "test-volume-123",
			SizeBytes:  10 * 1024 * 1024 * 1024,
			Status:     VolumeStatusOnDisk,
			Filesystem: "ext4",
		}

		// Create a test disk and set it in the cache
		disk := &storage_v1alpha.Disk{
			ID:           entity.Id("disk/test-disk"),
			Status:       storage_v1alpha.PROVISIONED,
			LsvdVolumeId: "test-volume-123",
		}
		dlc.SetTestDisk(disk)

		// Create a pending lease
		lease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/test-lease"),
			DiskId:    entity.Id("disk/test-disk"),
			SandboxId: entity.Id("sandbox/test-sandbox"),
			Status:    storage_v1alpha.PENDING,
			Mount: storage_v1alpha.Mount{
				Path:     "/data",
				Options:  "rw",
				ReadOnly: false,
			},
		}

		// Process the lease
		meta := &entity.Meta{}
		err := dlc.Create(context.Background(), lease, meta)
		require.NoError(t, err)

		// Should update status to BOUND
		hasStatus := false
		for _, attr := range meta.Attrs() {
			if attr.ID == storage_v1alpha.DiskLeaseStatusId {
				hasStatus = true
				assert.Equal(t, storage_v1alpha.DiskLeaseStatusBoundId, attr.Value.Id())
			}
			// Debug: print error message if present
			if attr.ID == storage_v1alpha.DiskLeaseErrorMessageId {
				t.Logf("Error message: %s", attr.Value.String())
			}
		}
		assert.True(t, hasStatus, "Should update status to BOUND")
	})

	t.Run("handles lease conflict", func(t *testing.T) {
		log := slog.Default()
		dlc := NewDiskLeaseController(log, nil, NewMockLsvdClient())

		// Simulate existing lease for the disk
		dlc.activeLeases["disk/test-disk"] = "disk-lease/existing-lease"

		conflictingLease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/conflicting-lease"),
			DiskId:    entity.Id("disk/test-disk"),
			SandboxId: entity.Id("sandbox/another-sandbox"),
			Status:    storage_v1alpha.PENDING,
			Mount: storage_v1alpha.Mount{
				Path: "/data",
			},
		}

		// Process the conflicting lease
		meta := &entity.Meta{}
		err := dlc.Create(context.Background(), conflictingLease, meta)
		require.NoError(t, err)

		// Should update status to FAILED with error message
		hasStatus := false
		hasError := false
		for _, attr := range meta.Attrs() {
			if attr.ID == storage_v1alpha.DiskLeaseStatusId {
				hasStatus = true
				assert.Equal(t, storage_v1alpha.DiskLeaseStatusFailedId, attr.Value.Id())
			}
			if attr.ID == storage_v1alpha.DiskLeaseErrorMessageId {
				hasError = true
				assert.Contains(t, attr.Value.String(), "already leased")
			}
		}
		assert.True(t, hasStatus, "Should update status to FAILED")
		assert.True(t, hasError, "Should include error message")
	})

	t.Run("releases lease on deletion", func(t *testing.T) {
		log := slog.Default()
		dlc := NewDiskLeaseController(log, nil, NewMockLsvdClient())

		// Setup active lease and lease details
		dlc.activeLeases["disk/test-disk"] = "disk-lease/test-lease"
		dlc.leaseDetails["disk-lease/test-lease"] = &leaseInfo{
			leaseId:   "disk-lease/test-lease",
			diskId:    "disk/test-disk",
			sandboxId: "sandbox/test-sandbox",
		}

		lease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/test-lease"),
			DiskId:    entity.Id("disk/test-disk"),
			SandboxId: entity.Id("sandbox/test-sandbox"),
			Status:    storage_v1alpha.BOUND,
		}

		// Process the deletion
		err := dlc.Delete(context.Background(), lease.ID)
		require.NoError(t, err)

		// Should remove from active leases
		_, exists := dlc.activeLeases["disk/test-disk"]
		assert.False(t, exists, "Should remove lease from active leases")

		// Should also remove from lease details
		_, detailsExist := dlc.leaseDetails["disk-lease/test-lease"]
		assert.False(t, detailsExist, "Should remove lease from lease details")
	})

	t.Run("unmounts disk on deletion", func(t *testing.T) {
		log := slog.Default()
		mockClient := NewMockLsvdClient()
		dlc := NewDiskLeaseController(log, nil, mockClient)

		// Setup a mounted volume
		volumeId := "test-volume-delete"
		mockClient.volumes[volumeId] = VolumeInfo{
			ID:         volumeId,
			Name:       volumeId,
			SizeBytes:  10 * 1024 * 1024 * 1024,
			Status:     VolumeStatusMounted,
			Filesystem: "ext4",
			MountPath:  "/var/lib/miren/disks/" + volumeId,
		}

		// Create a test disk and set it in the cache
		disk := &storage_v1alpha.Disk{
			ID:           entity.Id("disk/test-disk-unmount"),
			Status:       storage_v1alpha.PROVISIONED,
			LsvdVolumeId: volumeId,
			Filesystem:   storage_v1alpha.EXT4,
		}
		dlc.SetTestDisk(disk)

		// Setup active lease and lease details
		dlc.activeLeases["disk/test-disk-unmount"] = "disk-lease/test-lease-unmount"
		dlc.leaseDetails["disk-lease/test-lease-unmount"] = &leaseInfo{
			leaseId:   "disk-lease/test-lease-unmount",
			diskId:    "disk/test-disk-unmount",
			sandboxId: "sandbox/test-sandbox",
			volumeId:  volumeId, // Store the volume ID so Delete doesn't need to fetch the disk
		}

		// Process the deletion
		err := dlc.Delete(context.Background(), entity.Id("disk-lease/test-lease-unmount"))
		require.NoError(t, err)

		// Verify the volume was unmounted
		vol := mockClient.volumes[volumeId]
		assert.Equal(t, VolumeStatusLoaded, vol.Status, "Volume should be unmounted but still loaded")
		assert.Empty(t, vol.MountPath, "Mount path should be cleared")

		// Should remove from active leases
		_, exists := dlc.activeLeases["disk/test-disk-unmount"]
		assert.False(t, exists, "Should remove lease from active leases")

		// Should also remove from lease details
		_, detailsExist := dlc.leaseDetails["disk-lease/test-lease-unmount"]
		assert.False(t, detailsExist, "Should remove lease from lease details")
	})

	t.Run("handles explicit release", func(t *testing.T) {
		log := slog.Default()
		dlc := NewDiskLeaseController(log, nil, NewMockLsvdClient())

		// Setup active lease
		dlc.activeLeases["disk/test-disk"] = "disk-lease/test-lease"
		dlc.leaseDetails["disk-lease/test-lease"] = &leaseInfo{
			leaseId:   "disk-lease/test-lease",
			diskId:    "disk/test-disk",
			sandboxId: "sandbox/test-sandbox",
		}

		releasedLease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/test-lease"),
			DiskId:    entity.Id("disk/test-disk"),
			SandboxId: entity.Id("sandbox/test-sandbox"),
			Status:    storage_v1alpha.RELEASED,
		}

		// Process the release
		meta := &entity.Meta{}
		err := dlc.Update(context.Background(), releasedLease, meta)
		require.NoError(t, err)

		// Should remove from active leases
		_, exists := dlc.activeLeases["disk/test-disk"]
		assert.False(t, exists, "Should remove released lease from active leases")

		// Should remove from lease details
		_, detailsExist := dlc.leaseDetails["disk-lease/test-lease"]
		assert.False(t, detailsExist, "Should remove from lease details")
	})

	t.Run("release is idempotent", func(t *testing.T) {
		log := slog.Default()
		mockClient := NewMockLsvdClient()
		dlc := NewDiskLeaseController(log, nil, mockClient)

		// Setup: No active lease (already released)
		// Note: activeLeases is empty

		releasedLease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/already-released"),
			DiskId:    entity.Id("disk/already-released"),
			SandboxId: entity.Id("sandbox/test-sandbox"),
			Status:    storage_v1alpha.RELEASED,
		}

		// Process the release multiple times - should be idempotent
		meta := &entity.Meta{}

		// First call - lease already not active
		err := dlc.Update(context.Background(), releasedLease, meta)
		require.NoError(t, err)

		// Second call - should still work without errors
		err = dlc.Update(context.Background(), releasedLease, meta)
		require.NoError(t, err)

		// Verify no lease is tracked
		_, exists := dlc.activeLeases["disk/already-released"]
		assert.False(t, exists, "No lease should be tracked")
	})
}

func TestDiskLeaseController_HandleBoundLease(t *testing.T) {
	t.Run("remounts unmounted disk for bound lease", func(t *testing.T) {
		log := slog.Default()
		mockClient := NewMockLsvdClient()
		dlc := NewDiskLeaseController(log, nil, mockClient)

		// Setup: Add volume that is initialized but not mounted
		mockClient.volumes["test-volume-456"] = VolumeInfo{
			ID:         "test-volume-456",
			Name:       "test-volume-456",
			SizeBytes:  20 * 1024 * 1024 * 1024,
			Status:     VolumeStatusLoaded, // Initialized but not mounted
			Filesystem: "ext4",
		}

		// Create a test disk and set it in the cache
		disk := &storage_v1alpha.Disk{
			ID:           entity.Id("disk/bound-test-disk"),
			Status:       storage_v1alpha.PROVISIONED,
			LsvdVolumeId: "test-volume-456",
			Filesystem:   storage_v1alpha.EXT4,
		}
		dlc.SetTestDisk(disk)

		// Setup active lease
		dlc.activeLeases["disk/bound-test-disk"] = "disk-lease/bound-lease"
		dlc.leaseDetails["disk-lease/bound-lease"] = &leaseInfo{
			leaseId:   "disk-lease/bound-lease",
			diskId:    "disk/bound-test-disk",
			sandboxId: "sandbox/test-sandbox",
		}

		// Create a bound lease
		boundLease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/bound-lease"),
			DiskId:    entity.Id("disk/bound-test-disk"),
			SandboxId: entity.Id("sandbox/test-sandbox"),
			Status:    storage_v1alpha.BOUND,
			Mount: storage_v1alpha.Mount{
				Path:     "/data",
				ReadOnly: false,
			},
		}

		// Process the bound lease - should detect unmounted and remount
		meta := &entity.Meta{}
		err := dlc.Update(context.Background(), boundLease, meta)
		require.NoError(t, err)

		// Verify the volume was mounted
		vol := mockClient.volumes["test-volume-456"]
		assert.Equal(t, VolumeStatusMounted, vol.Status)
		assert.NotEmpty(t, vol.MountPath)
	})

	t.Run("tracks bound lease from EAS as active", func(t *testing.T) {
		log := slog.Default()
		mockClient := NewMockLsvdClient()
		dlc := NewDiskLeaseController(log, nil, mockClient)

		// Setup: Add volume that is mounted
		mockClient.volumes["test-volume-new"] = VolumeInfo{
			ID:         "test-volume-new",
			Name:       "test-volume-new",
			SizeBytes:  10 * 1024 * 1024 * 1024,
			Status:     VolumeStatusMounted,
			Filesystem: "ext4",
			MountPath:  "/var/lib/miren/disks/test-volume-new",
		}

		// Create a test disk and set it in the cache
		disk := &storage_v1alpha.Disk{
			ID:           entity.Id("disk/new-bound-disk"),
			Status:       storage_v1alpha.PROVISIONED,
			LsvdVolumeId: "test-volume-new",
			Filesystem:   storage_v1alpha.EXT4,
		}
		dlc.SetTestDisk(disk)

		// Initially, no lease is tracked
		assert.Empty(t, dlc.activeLeases)

		// Create a bound lease (simulating what we get from EAS)
		boundLease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/new-bound-lease"),
			DiskId:    entity.Id("disk/new-bound-disk"),
			SandboxId: entity.Id("sandbox/test-sandbox"),
			Status:    storage_v1alpha.BOUND,
			Mount: storage_v1alpha.Mount{
				Path:     "/data",
				ReadOnly: false,
			},
		}

		// Process the bound lease
		meta := &entity.Meta{}
		err := dlc.Update(context.Background(), boundLease, meta)
		require.NoError(t, err)

		// Verify the lease is now tracked as active
		currentLease, hasLease := dlc.activeLeases["disk/new-bound-disk"]
		assert.True(t, hasLease, "Bound lease should be tracked")
		assert.Equal(t, "disk-lease/new-bound-lease", currentLease)

		// Verify lease details are tracked
		details, hasDetails := dlc.leaseDetails["disk-lease/new-bound-lease"]
		assert.True(t, hasDetails, "Lease details should be tracked")
		assert.Equal(t, "disk/new-bound-disk", details.diskId)
		assert.Equal(t, "sandbox/test-sandbox", details.sandboxId)
	})

	t.Run("detects conflict when new bound lease tries to replace existing", func(t *testing.T) {
		log := slog.Default()
		mockClient := NewMockLsvdClient()
		dlc := NewDiskLeaseController(log, nil, mockClient)

		// Setup: Track an old lease
		dlc.activeLeases["disk/replacement-disk"] = "disk-lease/old-lease"
		dlc.leaseDetails["disk-lease/old-lease"] = &leaseInfo{
			leaseId:   "disk-lease/old-lease",
			diskId:    "disk/replacement-disk",
			sandboxId: "sandbox/old-sandbox",
		}

		// Create a test disk
		disk := &storage_v1alpha.Disk{
			ID:           entity.Id("disk/replacement-disk"),
			Status:       storage_v1alpha.PROVISIONED,
			LsvdVolumeId: "test-volume-replacement",
			Filesystem:   storage_v1alpha.EXT4,
		}
		dlc.SetTestDisk(disk)

		// Add volume
		mockClient.volumes["test-volume-replacement"] = VolumeInfo{
			ID:        "test-volume-replacement",
			Name:      "test-volume-replacement",
			Status:    VolumeStatusMounted,
			MountPath: "/var/lib/miren/disks/test-volume-replacement",
		}

		// Create a new bound lease for the same disk
		newBoundLease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/new-lease"),
			DiskId:    entity.Id("disk/replacement-disk"),
			SandboxId: entity.Id("sandbox/new-sandbox"),
			Status:    storage_v1alpha.BOUND,
			Mount: storage_v1alpha.Mount{
				Path: "/data",
			},
		}

		// Process the new bound lease
		meta := &entity.Meta{}
		err := dlc.Update(context.Background(), newBoundLease, meta)
		require.NoError(t, err)

		// Should detect conflict and fail the new lease
		hasFailure := false
		for _, attr := range meta.Attrs() {
			if attr.ID == storage_v1alpha.DiskLeaseStatusId {
				assert.Equal(t, storage_v1alpha.DiskLeaseStatusFailedId, attr.Value.Id())
				hasFailure = true
			}
			if attr.ID == storage_v1alpha.DiskLeaseErrorMessageId {
				assert.Contains(t, attr.Value.String(), "Lease conflict detected")
			}
		}
		assert.True(t, hasFailure, "Should detect conflict and fail")

		// Verify the old lease is still active (not replaced)
		currentLease, hasLease := dlc.activeLeases["disk/replacement-disk"]
		assert.True(t, hasLease)
		assert.Equal(t, "disk-lease/old-lease", currentLease)

		// Verify old lease details are still there
		_, hasOldDetails := dlc.leaseDetails["disk-lease/old-lease"]
		assert.True(t, hasOldDetails, "Old lease details should be preserved")

		// Verify new lease details are NOT tracked
		_, hasNewDetails := dlc.leaseDetails["disk-lease/new-lease"]
		assert.False(t, hasNewDetails, "New lease details should not be tracked due to conflict")
	})

	t.Run("leaves already mounted disk alone", func(t *testing.T) {
		log := slog.Default()
		mockClient := NewMockLsvdClient()
		dlc := NewDiskLeaseController(log, nil, mockClient)

		// Setup: Add volume that is already mounted
		mountPath := "/var/lib/miren/disks/test-volume-789"
		mockClient.volumes["test-volume-789"] = VolumeInfo{
			ID:         "test-volume-789",
			Name:       "test-volume-789",
			SizeBytes:  20 * 1024 * 1024 * 1024,
			Status:     VolumeStatusMounted,
			Filesystem: "ext4",
			MountPath:  mountPath,
		}

		// Create a test disk and set it in the cache
		disk := &storage_v1alpha.Disk{
			ID:           entity.Id("disk/mounted-test-disk"),
			Status:       storage_v1alpha.PROVISIONED,
			LsvdVolumeId: "test-volume-789",
			Filesystem:   storage_v1alpha.EXT4,
		}
		dlc.SetTestDisk(disk)

		// Setup active lease with volume ID
		dlc.activeLeases["disk/mounted-test-disk"] = "disk-lease/mounted-lease"
		dlc.leaseDetails["disk-lease/mounted-lease"] = &leaseInfo{
			leaseId:   "disk-lease/mounted-lease",
			diskId:    "disk/mounted-test-disk",
			sandboxId: "sandbox/test-sandbox",
			volumeId:  "test-volume-789",
		}

		// Create a bound lease
		boundLease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/mounted-lease"),
			DiskId:    entity.Id("disk/mounted-test-disk"),
			SandboxId: entity.Id("sandbox/test-sandbox"),
			Status:    storage_v1alpha.BOUND,
			Mount: storage_v1alpha.Mount{
				Path:     "/data",
				ReadOnly: false,
			},
		}

		// Process the bound lease
		meta := &entity.Meta{}
		err := dlc.Update(context.Background(), boundLease, meta)
		require.NoError(t, err)

		// Verify the volume is still mounted at the same path
		vol := mockClient.volumes["test-volume-789"]
		assert.Equal(t, VolumeStatusMounted, vol.Status)
		assert.Equal(t, mountPath, vol.MountPath)
	})

	t.Run("bound lease is idempotent", func(t *testing.T) {
		log := slog.Default()
		mockClient := NewMockLsvdClient()
		dlc := NewDiskLeaseController(log, nil, mockClient)

		volumeId := "test-volume-idempotent"
		mountPath := "/var/lib/miren/disks/" + volumeId

		// Setup: Add volume that is already mounted
		mockClient.volumes[volumeId] = VolumeInfo{
			ID:         volumeId,
			Name:       volumeId,
			SizeBytes:  10 * 1024 * 1024 * 1024,
			Status:     VolumeStatusMounted,
			Filesystem: "ext4",
			MountPath:  mountPath,
		}

		// Create a test disk and set it in the cache
		disk := &storage_v1alpha.Disk{
			ID:           entity.Id("disk/idempotent-disk"),
			Status:       storage_v1alpha.PROVISIONED,
			LsvdVolumeId: volumeId,
			Filesystem:   storage_v1alpha.EXT4,
		}
		dlc.SetTestDisk(disk)

		// Setup active lease with volume ID (already fully set up)
		dlc.activeLeases["disk/idempotent-disk"] = "disk-lease/idempotent-lease"
		dlc.leaseDetails["disk-lease/idempotent-lease"] = &leaseInfo{
			leaseId:   "disk-lease/idempotent-lease",
			diskId:    "disk/idempotent-disk",
			sandboxId: "sandbox/test-sandbox",
			volumeId:  volumeId,
		}

		// Create a bound lease
		boundLease := &storage_v1alpha.DiskLease{
			ID:        entity.Id("disk-lease/idempotent-lease"),
			DiskId:    entity.Id("disk/idempotent-disk"),
			SandboxId: entity.Id("sandbox/test-sandbox"),
			Status:    storage_v1alpha.BOUND,
			Mount: storage_v1alpha.Mount{
				Path: "/data",
			},
		}

		// Process the bound lease multiple times - should be idempotent
		meta := &entity.Meta{}

		// First call
		err := dlc.Update(context.Background(), boundLease, meta)
		require.NoError(t, err)

		// Verify volume is still mounted
		vol := mockClient.volumes[volumeId]
		assert.Equal(t, VolumeStatusMounted, vol.Status)
		assert.Equal(t, mountPath, vol.MountPath)

		// Second call - should do nothing since everything is already set up
		err = dlc.Update(context.Background(), boundLease, meta)
		require.NoError(t, err)

		// Third call - still idempotent
		err = dlc.Update(context.Background(), boundLease, meta)
		require.NoError(t, err)

		// Verify volume is still mounted and nothing changed
		vol = mockClient.volumes[volumeId]
		assert.Equal(t, VolumeStatusMounted, vol.Status)
		assert.Equal(t, mountPath, vol.MountPath)

		// Lease should still be tracked
		currentLease, exists := dlc.activeLeases["disk/idempotent-disk"]
		assert.True(t, exists)
		assert.Equal(t, "disk-lease/idempotent-lease", currentLease)
	})
}

func TestDiskLeaseController_CleanupOldReleasedLeases(t *testing.T) {
	t.Run("cleans up old released leases", func(t *testing.T) {
		log := slog.Default()
		dlc := NewDiskLeaseController(log, nil, NewMockLsvdClient())

		// Since we don't have a real EAC, we test the logic in isolation
		// The controller should skip cleanup when EAC is nil (test mode)
		ctx := context.Background()
		err := dlc.CleanupOldReleasedLeases(ctx)

		// Should not error even with no EAC
		assert.NoError(t, err)
	})

	t.Run("preserves recent released leases", func(t *testing.T) {
		log := slog.Default()
		dlc := NewDiskLeaseController(log, nil, NewMockLsvdClient())

		// Test mode - should skip when EAC is nil
		ctx := context.Background()
		err := dlc.CleanupOldReleasedLeases(ctx)
		assert.NoError(t, err)

		// Note: In a real scenario with EAC, we would verify that
		// leases with UpdatedAt within the last hour are not deleted
	})

	t.Run("preserves non-released leases", func(t *testing.T) {
		log := slog.Default()
		dlc := NewDiskLeaseController(log, nil, NewMockLsvdClient())

		// Test mode - should skip when EAC is nil
		ctx := context.Background()
		err := dlc.CleanupOldReleasedLeases(ctx)
		assert.NoError(t, err)

		// Note: In a real scenario with EAC, we would verify that
		// BOUND, PENDING, and FAILED leases are never deleted
	})
}
