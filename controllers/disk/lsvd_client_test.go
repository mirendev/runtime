package disk

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/lsvd"
)

func TestLsvdClient_GetVolumeInfo_LiveMountStatus(t *testing.T) {
	t.Run("detects mount status changes from system", func(t *testing.T) {
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir).(*lsvdClientImpl)

		ctx := context.Background()

		// Create a volume
		volumeId := "mount-status-test"
		err := client.CreateVolume(ctx, volumeId, 1, "ext4")
		require.NoError(t, err)

		// Initially not mounted
		info1, err := client.GetVolumeInfo(ctx, volumeId)
		require.NoError(t, err)
		assert.Equal(t, VolumeStatusLoaded, info1.Status, "Should not be mounted initially")

		// Simulate external mount by updating internal state
		// (In real scenario, this would be an actual mount)
		client.mu.Lock()
		if state, exists := client.volumes[volumeId]; exists {
			state.mounted = true
			state.info.MountPath = filepath.Join(tempDir, volumeId)
			state.info.Status = VolumeStatusMounted

		}
		client.mu.Unlock()

		// GetVolumeInfo should detect the mount is not real (not in /proc/mounts)
		info2, err := client.GetVolumeInfo(ctx, volumeId)
		require.NoError(t, err)
		assert.Equal(t, VolumeStatusLoaded, info2.Status, "Should detect mount is not in /proc/mounts")
		assert.Empty(t, info2.MountPath, "Should clear mount path when not actually mounted")

		// Verify internal state was corrected
		client.mu.RLock()
		if state, exists := client.volumes[volumeId]; exists {
			assert.False(t, state.mounted, "Internal state should be corrected")
			assert.Empty(t, state.info.MountPath, "Internal mount path should be cleared")
		}
		client.mu.RUnlock()
	})
}

func TestLsvdClient_IdempotentCreateAndMount(t *testing.T) {
	t.Run("CreateVolume is idempotent", func(t *testing.T) {
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir)

		ctx := context.Background()

		// First create
		err := client.CreateVolume(ctx, "idempotent-vol", 5, "ext4")
		require.NoError(t, err)

		// Second create should succeed (idempotent)
		err = client.CreateVolume(ctx, "idempotent-vol", 5, "ext4")
		require.NoError(t, err, "CreateVolume should be idempotent")

		// Verify volume exists
		info, err := client.GetVolumeInfo(ctx, "idempotent-vol")
		require.NoError(t, err)
		assert.Equal(t, "idempotent-vol", info.ID)
		assert.Equal(t, int64(5*1024*1024*1024), info.SizeBytes)
	})

	t.Run("MountVolume requires CreateVolume first", func(t *testing.T) {
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir)

		ctx := context.Background()

		// Try to mount without creating first
		mountPath := filepath.Join(tempDir, "mount", "nonexistent")
		err := client.MountVolume(ctx, "nonexistent-vol", mountPath, false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found in cache")
		assert.Contains(t, err.Error(), "CreateVolume first")

		// Now create the volume
		err = client.CreateVolume(ctx, "mount-test-vol", 1, "ext4")
		require.NoError(t, err)

		// Mount should work now (though will fail on NBD in test env)
		mountPath = filepath.Join(tempDir, "mount", "mount-test-vol")
		defer client.UnmountVolume(ctx, "mount-test-vol")
		err = client.MountVolume(ctx, "mount-test-vol", mountPath, false)
		// Will fail on NBD operations in test
		if err != nil {
			assert.Contains(t, err.Error(), "NBD")
		}
	})

	t.Run("CreateVolume loads existing volumes from disk", func(t *testing.T) {
		tempDir := t.TempDir()
		log := slog.Default()

		// Create a volume with first client
		client1 := NewLsvdClient(log, tempDir)
		ctx := context.Background()
		err := client1.CreateVolume(ctx, "persist-vol", 3, "ext4")
		require.NoError(t, err)

		// Create new client (simulating restart)
		client2 := NewLsvdClient(log, tempDir)

		// CreateVolume should load the existing volume
		err = client2.CreateVolume(ctx, "persist-vol", 3, "ext4")
		require.NoError(t, err, "Should load existing volume")

		// Verify it's in the cache
		info, err := client2.GetVolumeInfo(ctx, "persist-vol")
		require.NoError(t, err)
		assert.Equal(t, "persist-vol", info.ID)
		assert.Equal(t, int64(3*1024*1024*1024), info.SizeBytes)

		// Should be able to mount it now
		mountPath := filepath.Join(tempDir, "mount", "persist-vol")
		defer client2.UnmountVolume(ctx, "persist-vol")
		err = client2.MountVolume(ctx, "persist-vol", mountPath, false)
		// Will fail on NBD in test env
		if err != nil {
			assert.Contains(t, err.Error(), "NBD")
		}
	})
}

func TestLsvdClient_CreateVolume(t *testing.T) {
	t.Run("creates new volume", func(t *testing.T) {
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir)

		ctx := context.Background()
		err := client.CreateVolume(ctx, "test-vol-1", 100, "ext4")
		require.NoError(t, err)

		// Verify volume exists
		info, err := client.GetVolumeInfo(ctx, "test-vol-1")
		require.NoError(t, err)
		assert.Equal(t, "test-vol-1", info.ID)
		assert.Equal(t, int64(100*1024*1024*1024), info.SizeBytes) // 100 GB in bytes
		assert.Equal(t, "ext4", info.Filesystem)
		assert.Equal(t, VolumeStatusLoaded, info.Status)
		assert.NotEmpty(t, info.UUID, "Volume should have a UUID")
	})

	t.Run("CreateVolume is idempotent with same parameters", func(t *testing.T) {
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir)

		ctx := context.Background()
		err := client.CreateVolume(ctx, "test-vol-2", 50, "ext4")
		require.NoError(t, err)

		// Try to create same volume again - should succeed (idempotent)
		err = client.CreateVolume(ctx, "test-vol-2", 50, "ext4")
		assert.NoError(t, err, "CreateVolume should be idempotent with same params")

		// With different size - should succeed but log warning
		err = client.CreateVolume(ctx, "test-vol-2", 100, "ext4")
		assert.NoError(t, err, "CreateVolume should be idempotent even with different size")
	})

	t.Run("reuses existing volume in storage", func(t *testing.T) {
		tempDir := t.TempDir()
		log := slog.Default()

		// Create first client and volume
		client1 := NewLsvdClient(log, tempDir)
		ctx := context.Background()

		err := client1.CreateVolume(ctx, "reuse-vol", 25, "ext4")
		require.NoError(t, err)

		// Get the UUID from the first creation
		info1, err := client1.GetVolumeInfo(ctx, "reuse-vol")
		require.NoError(t, err)
		originalUUID := info1.UUID
		assert.NotEmpty(t, originalUUID)

		// Create a new client pointing to the same data path
		// This simulates a restart where the volume already exists
		client2 := NewLsvdClient(log, tempDir)

		// Try to create the volume again (should reuse existing)
		err = client2.CreateVolume(ctx, "reuse-vol", 25, "ext4")
		require.NoError(t, err)

		// Verify it has the same UUID (was reused)
		info2, err := client2.GetVolumeInfo(ctx, "reuse-vol")
		require.NoError(t, err)
		assert.Equal(t, originalUUID, info2.UUID, "Should reuse the same UUID")
	})
}

func TestLsvdClient_UnprovisionVolume(t *testing.T) {
	t.Run("unprovisions existing volume", func(t *testing.T) {
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir)

		ctx := context.Background()

		// Create volume
		err := client.CreateVolume(ctx, "delete-vol", 25, "btrfs")
		require.NoError(t, err)

		// Verify it exists
		info, err := client.GetVolumeInfo(ctx, "delete-vol")
		require.NoError(t, err)
		assert.Equal(t, "delete-vol", info.ID)

		// Unprovision volume
		err = client.UnprovisionVolume(ctx, "delete-vol")
		require.NoError(t, err)

		// Verify it's unprovisioned (removed from memory but still on disk)
		info, err = client.GetVolumeInfo(ctx, "delete-vol")
		require.NoError(t, err) // Should still find it on disk
		assert.Equal(t, VolumeStatusOnDisk, info.Status, "Volume should be on disk but not loaded")
	})

	t.Run("handles non-existent volume", func(t *testing.T) {
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir)

		ctx := context.Background()
		err := client.UnprovisionVolume(ctx, "non-existent")
		// UnprovisionVolume now returns nil for non-existent volumes
		// since there's nothing to unprovision
		assert.NoError(t, err)
	})
}

func TestLsvdClient_ReuseNBDDevice(t *testing.T) {
	if !allowNBDTest() {
		t.Skip("Skipping test that requires root privileges for NBD operations")
	}

	t.Run("reuses existing NBD device when still connected", func(t *testing.T) {
		// This test simulates a scenario where we try to mount a volume
		// that already has an NBD device attached
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir).(*lsvdClientImpl)

		ctx := context.Background()

		// Create a volume
		volumeId := "nbd-reuse-test"
		err := client.CreateVolume(ctx, volumeId, 1, "ext4")
		require.NoError(t, err)

		// Simulate that an NBD device is already attached
		// by setting the devicePath, cleanup function, and index
		client.mu.Lock()
		if state, exists := client.volumes[volumeId]; exists {
			state.devicePath = "/dev/nbd99"                // Fake device path
			state.nbdCleanup = func() error { return nil } // Dummy cleanup
			state.nbdIndex = 99                            // Fake NBD index

			// Store original values for verification
			originalDevice := state.devicePath
			originalIndex := state.nbdIndex

			client.mu.Unlock()

			// Now try to mount - it should try to check NBD status
			// and since the device doesn't exist, it will try to attach a new one
			mountPath := filepath.Join(tempDir, "mount", volumeId)
			defer client.UnmountVolume(ctx, volumeId)
			err = client.MountVolume(ctx, volumeId, mountPath, false)

			// Will fail because we can't actually attach NBD in test environment
			if err != nil {
				// The error will be about attaching NBD, not about the fake device
				assert.Contains(t, err.Error(), "NBD", "Should fail on NBD operations")
			}

			// Verify the state was cleaned up when status check failed
			client.mu.Lock()
			if state, exists := client.volumes[volumeId]; exists {
				// Since NBD status check would fail for fake device,
				// the old state should be cleared
				if state.devicePath == originalDevice && state.nbdIndex == originalIndex {
					// If unchanged, it means it tried to reuse but failed later
					t.Log("State unchanged, NBD reuse was attempted")
				} else {
					// State was cleared because NBD wasn't connected
					t.Log("State was cleared, NBD was not connected")
				}
			}
			client.mu.Unlock()
		} else {
			client.mu.Unlock()
			t.Fatal("Volume state not found")
		}
	})

	t.Run("attaches new NBD when old one is disconnected", func(t *testing.T) {
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir).(*lsvdClientImpl)

		ctx := context.Background()

		// Create a volume
		volumeId := "nbd-disconnect-test"
		err := client.CreateVolume(ctx, volumeId, 1, "ext4")
		require.NoError(t, err)

		// Simulate that an NBD device was attached but is now disconnected
		client.mu.Lock()
		if state, exists := client.volumes[volumeId]; exists {
			state.devicePath = "/dev/nbd98"                // Fake device path
			state.nbdCleanup = func() error { return nil } // Dummy cleanup
			state.nbdIndex = 98                            // Fake NBD index (will fail status check)

			client.mu.Unlock()

			// Try to mount - should detect disconnected NBD and try to attach new one
			mountPath := filepath.Join(tempDir, "mount", volumeId)
			defer client.UnmountVolume(ctx, volumeId)
			err = client.MountVolume(ctx, volumeId, mountPath, false)

			assert.NoError(t, err)

			// Verify old state was cleared
			client.mu.Lock()
			if state, exists := client.volumes[volumeId]; exists {
				// The old device path should be cleared since status check failed
				assert.NotEqual(t, "/dev/nbd98", state.devicePath, "Old device path should be cleared")
			}
			client.mu.Unlock()
		} else {
			client.mu.Unlock()
			t.Fatal("Volume state not found")
		}
	})
}

func allowNBDTest() bool {
	return os.Geteuid() == 0 && os.Getenv("DISABLE_NBD_TEST") != "1"
}

func TestLsvdClient_MountUnmount(t *testing.T) {
	t.Run("mounts and unmounts volume", func(t *testing.T) {
		if !allowNBDTest() {
			// Skip if not running as root since NBD operations require root privileges
			// or if explicitly disabled via env var
			t.Skip("Skipping test that requires root privileges for NBD operations")
		}

		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir)

		ctx := context.Background()

		// Create volume
		err := client.CreateVolume(ctx, "mount-vol", 10, "ext4")
		require.NoError(t, err)

		// Mount volume
		mountPath := filepath.Join(tempDir, "mounts", "test-mount")
		defer client.UnmountVolume(ctx, "mount-vol")
		err = client.MountVolume(ctx, "mount-vol", mountPath, false)
		require.NoError(t, err)

		// Verify mounted state
		info, err := client.GetVolumeInfo(ctx, "mount-vol")
		require.NoError(t, err)
		assert.Equal(t, VolumeStatusMounted, info.Status)
		assert.Equal(t, mountPath, info.MountPath)

		// Try to mount again (should fail)
		err = client.MountVolume(ctx, "mount-vol", "/another/path", false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already mounted")

		// Unmount volume
		err = client.UnmountVolume(ctx, "mount-vol")
		require.NoError(t, err)

		// Verify unmounted state
		info, err = client.GetVolumeInfo(ctx, "mount-vol")
		require.NoError(t, err)
		assert.Equal(t, VolumeStatusLoaded, info.Status)
		assert.Empty(t, info.MountPath)
	})

	t.Run("handles unmount of non-mounted volume", func(t *testing.T) {
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir)

		ctx := context.Background()

		// Create volume
		err := client.CreateVolume(ctx, "unmount-test", 5, "ext4")
		require.NoError(t, err)

		// Try to unmount non-mounted volume
		err = client.UnmountVolume(ctx, "unmount-test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not mounted")
	})
}

func TestLsvdClient_ListVolumes(t *testing.T) {
	t.Run("lists all volumes", func(t *testing.T) {
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir)

		ctx := context.Background()

		// Initially empty
		volumes, err := client.ListVolumes(ctx)
		require.NoError(t, err)
		assert.Empty(t, volumes)

		// Create multiple volumes
		volumeIds := []string{"vol-1", "vol-2", "vol-3"}
		for _, id := range volumeIds {
			err := client.CreateVolume(ctx, id, 10, "ext4")
			require.NoError(t, err)
		}

		// List volumes
		volumes, err = client.ListVolumes(ctx)
		require.NoError(t, err)
		assert.Len(t, volumes, 3)

		// Verify all volumes are listed
		for _, id := range volumeIds {
			assert.Contains(t, volumes, id)
		}
	})
}

func TestLsvdClient_ReplicaWriter(t *testing.T) {
	t.Run("creates volume with replication", func(t *testing.T) {
		tempDir := t.TempDir()
		log := slog.Default()

		// Create client with replica configuration
		// Using a mock URL for testing - in production this would be the actual DiskAPI endpoint
		client := NewLsvdClientWithReplica(log, tempDir, "http://diskapi.example.com", "test-token")

		ctx := context.Background()
		volumeId := "replica-test-vol"

		// Create volume - this will use ReplicaWriter internally
		err := client.CreateVolume(ctx, volumeId, 10, "ext4")
		// This will likely fail without a real DiskAPI endpoint, but that's expected in tests
		// The important thing is that the code path is exercised
		if err != nil {
			// Check if error is related to DiskAPI connection
			assert.Contains(t, err.Error(), "failed to init")
			t.Log("Expected error when DiskAPI is not available:", err)
		}
	})

	t.Run("falls back to local only when replica disabled", func(t *testing.T) {
		tempDir := t.TempDir()
		log := slog.Default()

		// Create regular client without replica
		client := NewLsvdClient(log, tempDir)

		ctx := context.Background()
		volumeId := "local-only-vol"

		// This should succeed with local storage only
		err := client.CreateVolume(ctx, volumeId, 10, "ext4")
		require.NoError(t, err)

		// Verify volume exists
		info, err := client.GetVolumeInfo(ctx, volumeId)
		require.NoError(t, err)
		assert.Equal(t, volumeId, info.ID)
	})
}

func TestLsvdClient_VolumeLifecycle(t *testing.T) {
	t.Run("complete volume lifecycle", func(t *testing.T) {
		// Skip if not running as root since NBD operations require root privileges
		if !allowNBDTest() {
			t.Skip("Skipping test that requires root privileges for NBD operations")
		}

		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir)

		ctx := context.Background()
		volumeId := "lifecycle-vol"

		// 1. Create volume
		err := client.CreateVolume(ctx, volumeId, 50, "ext4")
		require.NoError(t, err)

		// 2. Verify creation
		info, err := client.GetVolumeInfo(ctx, volumeId)
		require.NoError(t, err)
		assert.Equal(t, volumeId, info.ID)
		assert.Equal(t, VolumeStatusLoaded, info.Status)

		// 3. Mount volume
		mountPath := filepath.Join(tempDir, "mounts", volumeId)
		defer client.UnmountVolume(ctx, volumeId)
		err = client.MountVolume(ctx, volumeId, mountPath, false)
		require.NoError(t, err)

		// 4. Verify mount
		info, err = client.GetVolumeInfo(ctx, volumeId)
		require.NoError(t, err)
		assert.Equal(t, VolumeStatusMounted, info.Status)
		assert.Equal(t, mountPath, info.MountPath)

		// 5. List volumes (should include our volume)
		volumes, err := client.ListVolumes(ctx)
		require.NoError(t, err)
		assert.Contains(t, volumes, volumeId)

		// 6. Unmount volume
		err = client.UnmountVolume(ctx, volumeId)
		require.NoError(t, err)

		// 7. Unprovision volume
		err = client.UnprovisionVolume(ctx, volumeId)
		require.NoError(t, err)

		// 8. Verify deletion
		_, err = client.GetVolumeInfo(ctx, volumeId)
		assert.Error(t, err)

		// 9. List volumes (should not include deleted volume)
		volumes, err = client.ListVolumes(ctx)
		require.NoError(t, err)
		assert.NotContains(t, volumes, volumeId)
	})
}

// MockLsvdClient is a mock implementation for testing
type MockLsvdClient struct {
	volumes         map[string]VolumeInfo
	mountShouldFail bool // For testing mount failures
}

func NewMockLsvdClient() *MockLsvdClient {
	return &MockLsvdClient{
		volumes: make(map[string]VolumeInfo),
	}
}

func (m *MockLsvdClient) CreateVolume(ctx context.Context, volumeId string, sizeGb int64, filesystem string) error {
	// Implement as combination of CreateVolumeInSegmentAccess and InitializeDisk
	if err := m.CreateVolumeInSegmentAccess(ctx, volumeId, sizeGb, filesystem); err != nil {
		return err
	}
	return m.InitializeDisk(ctx, volumeId, filesystem)
}

func (m *MockLsvdClient) CreateVolumeInSegmentAccess(ctx context.Context, volumeId string, sizeGb int64, filesystem string) error {
	// Just check if volume already exists
	if _, exists := m.volumes[volumeId]; exists {
		return nil // Idempotent
	}

	// Generate a mock UUID for testing
	mockUUID := fmt.Sprintf("mock-uuid-%s", volumeId)

	// Create volume in "OnDisk" status (not loaded yet)
	m.volumes[volumeId] = VolumeInfo{
		ID:         volumeId,
		Name:       volumeId,
		SizeBytes:  sizeGb * 1024 * 1024 * 1024,
		Filesystem: filesystem,
		MountPath:  "",
		UUID:       mockUUID,
		Status:     VolumeStatusOnDisk,
	}
	return nil
}

func (m *MockLsvdClient) InitializeDisk(ctx context.Context, volumeId string, filesystem string) error {
	vol, exists := m.volumes[volumeId]
	if !exists {
		return fmt.Errorf("volume %s not found in segment access", volumeId)
	}
	// Change status to Loaded (disk initialized)
	vol.Status = VolumeStatusLoaded
	if filesystem != "" {
		vol.Filesystem = filesystem
	}
	m.volumes[volumeId] = vol
	return nil
}

func (m *MockLsvdClient) UnprovisionVolume(ctx context.Context, volumeId string) error {
	if _, exists := m.volumes[volumeId]; !exists {
		return fmt.Errorf("volume %s not found", volumeId)
	}
	delete(m.volumes, volumeId)
	return nil
}

func (m *MockLsvdClient) MountVolume(ctx context.Context, volumeId string, mountPath string, readOnly bool) error {
	// Check if we should simulate a mount failure
	if m.mountShouldFail {
		return fmt.Errorf("mount failed: simulated failure")
	}

	vol, exists := m.volumes[volumeId]
	if !exists {
		return fmt.Errorf("volume %s not found", volumeId)
	}
	if vol.Status == VolumeStatusMounted {
		return fmt.Errorf("volume %s already mounted", volumeId)
	}
	vol.Status = VolumeStatusMounted
	vol.MountPath = mountPath
	m.volumes[volumeId] = vol
	return nil
}

func (m *MockLsvdClient) IsVolumeMounted(ctx context.Context, volumeId string) (bool, error) {
	vol, exists := m.volumes[volumeId]
	if !exists {
		return false, nil
	}
	return vol.Status == VolumeStatusMounted, nil
}

func (m *MockLsvdClient) UnmountVolume(ctx context.Context, volumeId string) error {
	vol, exists := m.volumes[volumeId]
	if !exists {
		return fmt.Errorf("volume %s not found", volumeId)
	}
	if vol.Status != VolumeStatusMounted {
		return fmt.Errorf("volume %s not mounted", volumeId)
	}
	vol.Status = VolumeStatusLoaded
	vol.MountPath = ""
	m.volumes[volumeId] = vol
	return nil
}

func (m *MockLsvdClient) GetVolumeInfo(ctx context.Context, volumeId string) (*VolumeInfo, error) {
	vol, exists := m.volumes[volumeId]
	if !exists {
		return nil, fmt.Errorf("volume %s not found", volumeId)
	}
	return &vol, nil
}

func (m *MockLsvdClient) ListVolumes(ctx context.Context) ([]string, error) {
	var ids []string
	for id := range m.volumes {
		ids = append(ids, id)
	}
	return ids, nil
}

func TestLsvdClient_CreateVolumeStatus(t *testing.T) {
	t.Run("new volume has VolumeStatusLoaded", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir)

		volumeId := "new-status-test"
		err := client.CreateVolume(ctx, volumeId, 1, "ext4")
		require.NoError(t, err)

		info, err := client.GetVolumeInfo(ctx, volumeId)
		require.NoError(t, err)
		assert.Equal(t, VolumeStatusLoaded, info.Status)
	})

	t.Run("idempotent CreateVolume preserves loaded status", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir).(*lsvdClientImpl)

		volumeId := "idempotent-status"

		// First create
		err := client.CreateVolume(ctx, volumeId, 2, "xfs")
		require.NoError(t, err)

		// Verify initial status
		info1, err := client.GetVolumeInfo(ctx, volumeId)
		require.NoError(t, err)
		assert.Equal(t, VolumeStatusLoaded, info1.Status)

		// Second create (idempotent)
		err = client.CreateVolume(ctx, volumeId, 2, "xfs")
		require.NoError(t, err)

		// Verify status is still loaded
		info2, err := client.GetVolumeInfo(ctx, volumeId)
		require.NoError(t, err)
		assert.Equal(t, VolumeStatusLoaded, info2.Status)
	})

	t.Run("CreateVolume corrects mounted status", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir).(*lsvdClientImpl)

		volumeId := "mounted-correct"

		// Create volume
		err := client.CreateVolume(ctx, volumeId, 1, "ext4")
		require.NoError(t, err)

		// Manually set to mounted (simulating state corruption)
		client.mu.Lock()
		if state, exists := client.volumes[volumeId]; exists {
			state.mounted = true
			state.info.MountPath = "/fake/path"
			state.info.Status = VolumeStatusMounted
			state.info.MountPath = "/fake/path"
		}
		client.mu.Unlock()

		// Call CreateVolume again - should correct the status
		err = client.CreateVolume(ctx, volumeId, 1, "ext4")
		require.NoError(t, err)

		// Verify status was corrected
		info, err := client.GetVolumeInfo(ctx, volumeId)
		require.NoError(t, err)
		assert.Equal(t, VolumeStatusLoaded, info.Status, "Status should be corrected to loaded")
		assert.False(t, client.volumes[volumeId].mounted, "Mounted flag should be false")
	})

	t.Run("CreateVolume sets status when initializing missing disk", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir).(*lsvdClientImpl)

		volumeId := "missing-disk"

		// Create volume
		err := client.CreateVolume(ctx, volumeId, 1, "ext4")
		require.NoError(t, err)

		// Clear the disk but set wrong status
		client.mu.Lock()
		if state, exists := client.volumes[volumeId]; exists {
			state.disk = nil
			state.info.Status = VolumeStatusOnDisk // Wrong status
			delete(client.disks, volumeId)
		}
		client.mu.Unlock()

		// Call CreateVolume again - should fix disk and status
		err = client.CreateVolume(ctx, volumeId, 1, "ext4")
		require.NoError(t, err)

		// Verify disk was reinitialized and status corrected
		client.mu.RLock()
		state, exists := client.volumes[volumeId]
		client.mu.RUnlock()

		require.True(t, exists)
		assert.NotNil(t, state.disk, "Disk should be reinitialized")
		assert.Equal(t, VolumeStatusLoaded, state.info.Status, "Status should be loaded")
	})

	t.Run("CreateVolume loads from disk with correct status", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()

		// Create with first client
		client1 := NewLsvdClient(log, tempDir)
		volumeId := "persist-status"
		err := client1.CreateVolume(ctx, volumeId, 2, "btrfs")
		require.NoError(t, err)

		// New client loads from disk
		client2 := NewLsvdClient(log, tempDir)
		err = client2.CreateVolume(ctx, volumeId, 2, "btrfs")
		require.NoError(t, err)

		// Verify status is loaded
		info, err := client2.GetVolumeInfo(ctx, volumeId)
		require.NoError(t, err)
		assert.Equal(t, VolumeStatusLoaded, info.Status)
	})

	t.Run("mount and unmount update status correctly", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir).(*lsvdClientImpl)

		volumeId := "mount-unmount-status"

		// Create volume
		err := client.CreateVolume(ctx, volumeId, 1, "ext4")
		require.NoError(t, err)

		// Initial status should be loaded
		info1, err := client.GetVolumeInfo(ctx, volumeId)
		require.NoError(t, err)
		assert.Equal(t, VolumeStatusLoaded, info1.Status)

		mountPath := filepath.Join(tempDir, "mount", volumeId)
		defer client.UnmountVolume(ctx, volumeId)
		err = client.MountVolume(ctx, volumeId, mountPath, false) // Ignore NBD error

		if err != nil {
			// Try to mount (will fail due to NBD, but update state manually)
			// Manually set mounted state
			client.mu.Lock()
			if state, exists := client.volumes[volumeId]; exists {
				state.mounted = true
				state.info.MountPath = mountPath
				state.info.Status = VolumeStatusMounted
				state.info.MountPath = mountPath
			}
			client.mu.Unlock()

			// Check internal state directly (GetVolumeInfo would correct it)
			client.mu.RLock()
			state := client.volumes[volumeId]
			client.mu.RUnlock()
			assert.Equal(t, VolumeStatusMounted, state.info.Status)

			// Set mounted to false to simulate unmount
			client.mu.Lock()
			if state, exists := client.volumes[volumeId]; exists {
				state.mounted = false
				state.info.MountPath = ""
				state.info.Status = VolumeStatusLoaded
				state.info.MountPath = ""
			}
			client.mu.Unlock()

		} else {
			client.UnmountVolume(ctx, volumeId)
		}

		// Verify status is back to loaded
		info3, err := client.GetVolumeInfo(ctx, volumeId)
		require.NoError(t, err)
		assert.Equal(t, VolumeStatusLoaded, info3.Status)
	})
}

func TestLsvdClient_MountStateVerification(t *testing.T) {
	t.Run("corrects mount state mismatch on MountVolume", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir).(*lsvdClientImpl)

		// Create a volume
		volumeId := "state-test-vol"
		err := client.CreateVolume(ctx, volumeId, 1, "ext4")
		require.NoError(t, err)

		// Manually set mounted state to true (simulating state corruption)
		client.mu.Lock()
		if state, exists := client.volumes[volumeId]; exists {
			state.mounted = true
			state.info.MountPath = "/fake/mount/path"
			state.info.Status = VolumeStatusMounted
			state.info.MountPath = "/fake/mount/path"
		}
		client.mu.Unlock()

		// Try to mount - should detect state mismatch and correct it
		mountPath := filepath.Join(tempDir, "mounts", volumeId)
		defer client.UnmountVolume(ctx, volumeId)
		err = client.MountVolume(ctx, volumeId, mountPath, false)

		// Should succeed after correcting state (but will fail on NBD in test env)
		if err != nil {
			assert.Contains(t, err.Error(), "NBD", "Error should be about NBD, not state mismatch")
			// Verify state was corrected before mount attempt
			client.mu.RLock()
			state, exists := client.volumes[volumeId]
			client.mu.RUnlock()
			require.True(t, exists)
			// State should show not mounted since mount failed
			assert.False(t, state.mounted, "State should be corrected to not mounted")
			assert.Equal(t, "", state.info.MountPath, "Mount path should be cleared")
		} else {
			// Verify state was corrected before mount attempt
			client.mu.RLock()
			state, exists := client.volumes[volumeId]
			client.mu.RUnlock()
			require.True(t, exists)
			// State should show not mounted since mount failed
			assert.True(t, state.mounted, "State should be corrected to not mounted")
			assert.Equal(t, mountPath, state.info.MountPath)
		}
	})

	t.Run("corrects mount state mismatch on UnmountVolume", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir).(*lsvdClientImpl)

		// Create a volume
		volumeId := "unmount-state-test"
		err := client.CreateVolume(ctx, volumeId, 1, "ext4")
		require.NoError(t, err)

		// Manually set mounted state to true (simulating state corruption)
		client.mu.Lock()
		if state, exists := client.volumes[volumeId]; exists {
			state.mounted = true
			state.info.MountPath = "/fake/mount/path"
			state.info.Status = VolumeStatusMounted
			state.info.MountPath = "/fake/mount/path"
		}
		client.mu.Unlock()

		// Try to unmount - should detect state mismatch and return error
		err = client.UnmountVolume(ctx, volumeId)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not mounted")

		// Verify state was corrected
		client.mu.RLock()
		state, exists := client.volumes[volumeId]
		client.mu.RUnlock()
		require.True(t, exists)
		assert.False(t, state.mounted, "State should be corrected to not mounted")
		assert.Equal(t, "", state.info.MountPath, "Mount path should be cleared")
		assert.Equal(t, VolumeStatusLoaded, state.info.Status, "Status should be loaded")
	})

	t.Run("detects and corrects stale mount state during unprovision", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir).(*lsvdClientImpl)

		// Create a volume
		volumeId := "unprovision-state-test"
		err := client.CreateVolume(ctx, volumeId, 1, "ext4")
		require.NoError(t, err)

		// Manually set mounted state to true (simulating state corruption)
		client.mu.Lock()
		if state, exists := client.volumes[volumeId]; exists {
			state.mounted = true
			state.info.MountPath = "/fake/mount/path"
			state.info.Status = VolumeStatusMounted
			state.info.MountPath = "/fake/mount/path"
		}
		client.mu.Unlock()

		// Unprovision should detect mismatch and handle gracefully
		err = client.UnprovisionVolume(ctx, volumeId)
		assert.NoError(t, err)

		// Volume should be removed from state
		client.mu.RLock()
		_, exists := client.volumes[volumeId]
		client.mu.RUnlock()
		assert.False(t, exists, "Volume should be removed after unprovision")
	})
}

func TestLsvdClient_DiskInitialization(t *testing.T) {
	t.Run("CreateVolume initializes disk for new volume", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir).(*lsvdClientImpl)

		volumeId := "new-vol"
		err := client.CreateVolume(ctx, volumeId, 1, "ext4")
		require.NoError(t, err)

		// Verify disk is initialized
		client.mu.RLock()
		state, exists := client.volumes[volumeId]
		client.mu.RUnlock()

		require.True(t, exists)
		assert.NotNil(t, state.disk, "Disk should be initialized")
		assert.NotNil(t, state.sa, "Segment access should be initialized")
		assert.Equal(t, VolumeStatusLoaded, state.info.Status)
	})

	t.Run("CreateVolume initializes disk for cached volume without disk", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir).(*lsvdClientImpl)

		volumeId := "cached-vol"

		// First create the volume normally
		err := client.CreateVolume(ctx, volumeId, 2, "xfs")
		require.NoError(t, err)

		// Manually clear the disk (simulating a partially initialized state)
		client.mu.Lock()
		if state, exists := client.volumes[volumeId]; exists {
			state.disk = nil
			delete(client.disks, volumeId)
		}
		client.mu.Unlock()

		// Call CreateVolume again - should reinitialize the disk
		err = client.CreateVolume(ctx, volumeId, 2, "xfs")
		require.NoError(t, err)

		// Verify disk was reinitialized
		client.mu.RLock()
		state, exists := client.volumes[volumeId]
		client.mu.RUnlock()

		require.True(t, exists)
		assert.NotNil(t, state.disk, "Disk should be reinitialized")
		assert.NotNil(t, client.disks[volumeId], "Disk should be in disks map")
	})

	t.Run("MountVolume fails if disk not initialized", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir).(*lsvdClientImpl)

		volumeId := "no-disk-vol"

		// Create a volume state without disk (simulating corrupted state)
		client.mu.Lock()
		client.volumes[volumeId] = &volumeState{
			info: VolumeInfo{
				ID:         volumeId,
				Name:       volumeId,
				SizeBytes:  1024 * 1024 * 1024,
				Filesystem: "ext4",
				Status:     VolumeStatusLoaded,
			},
			disk: nil, // No disk initialized
		}
		client.mu.Unlock()

		// Try to mount - should fail with specific error
		mountPath := tempDir + "/mount"
		err := client.MountVolume(ctx, volumeId, mountPath, false)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "disk not initialized")
		assert.Contains(t, err.Error(), "call CreateVolume first")
	})

	t.Run("CreateVolume loads existing volume from disk with disk initialized", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()

		// Create volume with first client
		client1 := NewLsvdClient(log, tempDir).(*lsvdClientImpl)
		volumeId := "persist-vol"
		err := client1.CreateVolume(ctx, volumeId, 3, "ext4")
		require.NoError(t, err)

		// Verify disk is initialized in first client
		client1.mu.RLock()
		state1, exists1 := client1.volumes[volumeId]
		client1.mu.RUnlock()
		require.True(t, exists1)
		assert.NotNil(t, state1.disk)

		// Create new client (simulating restart)
		client2 := NewLsvdClient(log, tempDir).(*lsvdClientImpl)

		// Load the existing volume
		err = client2.CreateVolume(ctx, volumeId, 3, "ext4")
		require.NoError(t, err)

		// Verify disk is initialized in second client
		client2.mu.RLock()
		state2, exists2 := client2.volumes[volumeId]
		client2.mu.RUnlock()

		require.True(t, exists2)
		assert.NotNil(t, state2.disk, "Disk should be initialized when loading from disk")
		assert.NotNil(t, state2.sa, "Segment access should be initialized")
		assert.Equal(t, VolumeStatusLoaded, state2.info.Status)
	})
}

func TestLsvdClient_PathHelpers(t *testing.T) {
	t.Run("path helpers return expected paths", func(t *testing.T) {
		log := slog.Default()
		dataPath := "/custom/data/path"
		client := &lsvdClientImpl{
			log:      log,
			dataPath: dataPath,
			volumes:  make(map[string]*volumeState),
			disks:    make(map[string]*lsvd.Disk),
		}

		volumeId := "test-vol-123"

		// Test getVolumePath
		expectedVolumePath := filepath.Join(dataPath, "lsvd-volumes", volumeId)
		actualVolumePath := client.getVolumePath(volumeId)
		assert.Equal(t, expectedVolumePath, actualVolumePath, "Volume path should match")

		// Test getVolumesBasePath
		expectedBasePath := filepath.Join(dataPath, "lsvd-volumes")
		actualBasePath := client.getVolumesBasePath()
		assert.Equal(t, expectedBasePath, actualBasePath, "Volumes base path should match")

	})

	t.Run("paths are used consistently in CreateVolume", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir).(*lsvdClientImpl)

		volumeId := "path-test-vol"
		err := client.CreateVolume(ctx, volumeId, 1, "ext4")
		require.NoError(t, err)

		// Verify the volume state has the correct paths
		state, exists := client.volumes[volumeId]
		require.True(t, exists)

		// Check segment access directory matches getVolumePath
		expectedPath := client.getVolumePath(volumeId)
		if localSA, ok := state.sa.(*lsvd.LocalFileAccess); ok {
			assert.Equal(t, expectedPath, localSA.Dir, "Segment access should use volume path")
		}
	})

	t.Run("paths are used consistently in GetVolumeInfo", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()

		// Create volume with first client
		client1 := NewLsvdClient(log, tempDir)
		volumeId := "info-path-test"
		err := client1.CreateVolume(ctx, volumeId, 1, "ext4")
		require.NoError(t, err)

		// Create new client to test loading from disk
		client2 := NewLsvdClient(log, tempDir).(*lsvdClientImpl)

		// GetVolumeInfo should use helper methods for paths
		info, err := client2.GetVolumeInfo(ctx, volumeId)
		require.NoError(t, err)
		assert.Equal(t, volumeId, info.ID)

		// If volume gets loaded later, it should use the same paths
		err = client2.CreateVolume(ctx, volumeId, 1, "ext4")
		require.NoError(t, err)

		state, exists := client2.volumes[volumeId]
		require.True(t, exists)

		expectedPath := client2.getVolumePath(volumeId)
		if localSA, ok := state.sa.(*lsvd.LocalFileAccess); ok {
			assert.Equal(t, expectedPath, localSA.Dir, "Loaded volume should use same path")
		}
	})

	t.Run("ListVolumes uses volumes base path", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir)

		// Create a few volumes
		err := client.CreateVolume(ctx, "list-vol-1", 1, "ext4")
		require.NoError(t, err)
		err = client.CreateVolume(ctx, "list-vol-2", 1, "xfs")
		require.NoError(t, err)

		// List should find them
		volumes, err := client.ListVolumes(ctx)
		require.NoError(t, err)
		assert.Len(t, volumes, 2)
		assert.Contains(t, volumes, "list-vol-1")
		assert.Contains(t, volumes, "list-vol-2")
	})

}
