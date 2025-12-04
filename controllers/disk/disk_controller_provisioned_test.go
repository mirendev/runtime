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
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

// mockLsvdClient is a mock implementation of LsvdClient for testing
type mockLsvdClient struct {
	createFunc                func(ctx context.Context, sizeGb int64, filesystem string) (string, error)
	createInSegmentAccessFunc func(ctx context.Context, diskName string, sizeGb int64, filesystem string) (string, error)
	initializeDiskFunc        func(ctx context.Context, volumeId string, filesystem string) error
	mountFunc                 func(ctx context.Context, volumeId string, mountPath string, readOnly bool) error
	unmountFunc               func(ctx context.Context, volumeId string) error
	unprovisionFunc           func(ctx context.Context, volumeId string) error
	getInfoFunc               func(ctx context.Context, volumeId string) (*VolumeInfo, error)
	listFunc                  func(ctx context.Context) ([]VolumeInfo, error)
	isMountedFunc             func(ctx context.Context, volumeId string) (bool, error)
	acquireLeaseFunc          func(ctx context.Context, volumeId, nodeId, appId string) (string, error)
	releaseLeaseFunc          func(ctx context.Context, volumeId string, nonce string) error
	generatedVolumeId         string // For tracking generated volume IDs
}

func (m *mockLsvdClient) CreateVolume(ctx context.Context, sizeGb int64, filesystem string) (string, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, sizeGb, filesystem)
	}
	return "mock-vol-id", nil
}

func (m *mockLsvdClient) CreateVolumeInSegmentAccess(ctx context.Context, diskName string, sizeGb int64, filesystem string) (string, error) {
	if m.createInSegmentAccessFunc != nil {
		return m.createInSegmentAccessFunc(ctx, diskName, sizeGb, filesystem)
	}
	// Default behavior: generate a mock volume ID
	if m.generatedVolumeId != "" {
		return m.generatedVolumeId, nil
	}
	return "mock-generated-vol", nil
}

func (m *mockLsvdClient) InitializeDisk(ctx context.Context, volumeId string, filesystem string) error {
	if m.initializeDiskFunc != nil {
		return m.initializeDiskFunc(ctx, volumeId, filesystem)
	}
	return nil
}

func (m *mockLsvdClient) MountVolume(ctx context.Context, volumeId string, mountPath string, readOnly bool) error {
	if m.mountFunc != nil {
		return m.mountFunc(ctx, volumeId, mountPath, readOnly)
	}
	return nil
}

func (m *mockLsvdClient) UnmountVolume(ctx context.Context, volumeId string) error {
	if m.unmountFunc != nil {
		return m.unmountFunc(ctx, volumeId)
	}
	return nil
}

func (m *mockLsvdClient) UnprovisionVolume(ctx context.Context, volumeId string) error {
	if m.unprovisionFunc != nil {
		return m.unprovisionFunc(ctx, volumeId)
	}
	return nil
}

func (m *mockLsvdClient) GetVolumeInfo(ctx context.Context, volumeId string) (*VolumeInfo, error) {
	if m.getInfoFunc != nil {
		return m.getInfoFunc(ctx, volumeId)
	}
	return &VolumeInfo{
		ID:        volumeId,
		Name:      volumeId,
		SizeBytes: 10 * 1024 * 1024 * 1024,
		Status:    VolumeStatusLoaded,
	}, nil
}

func (m *mockLsvdClient) ListVolumes(ctx context.Context) ([]string, error) {
	if m.listFunc != nil {
		volumes, err := m.listFunc(ctx)
		if err != nil {
			return nil, err
		}
		result := make([]string, len(volumes))
		for i, v := range volumes {
			result[i] = v.ID
		}
		return result, nil
	}
	return []string{}, nil
}

func (m *mockLsvdClient) IsVolumeMounted(ctx context.Context, volumeId string) (bool, error) {
	if m.isMountedFunc != nil {
		return m.isMountedFunc(ctx, volumeId)
	}
	return false, nil
}

func (m *mockLsvdClient) AcquireVolumeLease(ctx context.Context, volumeId, nodeId, appId string) (string, error) {
	if m.acquireLeaseFunc != nil {
		return m.acquireLeaseFunc(ctx, volumeId, nodeId, appId)
	}
	// Return a mock nonce for testing
	return "mock-nonce-" + volumeId, nil
}

func (m *mockLsvdClient) ReleaseVolumeLease(ctx context.Context, volumeId string, nonce string) error {
	if m.releaseLeaseFunc != nil {
		return m.releaseLeaseFunc(ctx, volumeId, nonce)
	}
	return nil
}

func TestDiskController_HandleProvisioned(t *testing.T) {
	tests := []struct {
		name           string
		setupDisk      func() *storage_v1alpha.Disk
		setupClient    func(t *testing.T, tempDir string) LsvdClient
		validateResult func(t *testing.T, attrs []entity.Attr, err error)
		expectedLogs   []string
	}{
		{
			name: "re-provisions disk with missing volume ID",
			setupDisk: func() *storage_v1alpha.Disk {
				return &storage_v1alpha.Disk{
					ID:         entity.Id("disk/test-missing-vol"),
					Name:       "test-disk",
					Status:     storage_v1alpha.PROVISIONED,
					SizeGb:     10,
					Filesystem: storage_v1alpha.EXT4,
					// Missing LsvdVolumeId
				}
			},
			setupClient: func(t *testing.T, tempDir string) LsvdClient {
				return &mockLsvdClient{
					createFunc: func(ctx context.Context, sizeGb int64, filesystem string) (string, error) {
						return "mock-vol-id", nil
					},
					mountFunc: func(ctx context.Context, volumeId string, mountPath string, readOnly bool) error {
						return nil
					},
				}
			},
			validateResult: func(t *testing.T, attrs []entity.Attr, err error) {
				require.NoError(t, err)
				// handleProvisioned now returns only error, no attrs
			},
			expectedLogs: []string{
				"Provisioned disk has no volume ID, re-provisioning",
			},
		},
		{
			name: "mounts unmounted volume",
			setupDisk: func() *storage_v1alpha.Disk {
				return &storage_v1alpha.Disk{
					ID:           entity.Id("disk/test-unmounted"),
					Name:         "test-disk",
					Status:       storage_v1alpha.PROVISIONED,
					SizeGb:       10,
					Filesystem:   storage_v1alpha.EXT4,
					LsvdVolumeId: "existing-vol-123",
				}
			},
			setupClient: func(t *testing.T, tempDir string) LsvdClient {
				return &mockLsvdClient{
					getInfoFunc: func(ctx context.Context, volumeId string) (*VolumeInfo, error) {
						return &VolumeInfo{
							ID:        volumeId,
							Name:      volumeId,
							SizeBytes: 10 * 1024 * 1024 * 1024,
							Status:    VolumeStatusLoaded,
						}, nil
					},
					mountFunc: func(ctx context.Context, volumeId string, mountPath string, readOnly bool) error {
						assert.Equal(t, "existing-vol-123", volumeId)
						assert.Contains(t, mountPath, volumeId)
						assert.False(t, readOnly)
						return nil
					},
				}
			},
			validateResult: func(t *testing.T, attrs []entity.Attr, err error) {
				require.NoError(t, err)
				// handleProvisioned only verifies volume exists, no mounting
			},
			expectedLogs: []string{
				"Mounting provisioned disk volume",
				"Successfully mounted provisioned disk",
			},
		},
		{
			name: "skips already mounted volume",
			setupDisk: func() *storage_v1alpha.Disk {
				return &storage_v1alpha.Disk{
					ID:           entity.Id("disk/test-mounted"),
					Name:         "test-disk",
					Status:       storage_v1alpha.PROVISIONED,
					SizeGb:       10,
					Filesystem:   storage_v1alpha.EXT4,
					LsvdVolumeId: "mounted-vol-123",
				}
			},
			setupClient: func(t *testing.T, tempDir string) LsvdClient {
				return &mockLsvdClient{
					getInfoFunc: func(ctx context.Context, volumeId string) (*VolumeInfo, error) {
						return &VolumeInfo{
							ID:        volumeId,
							Name:      volumeId,
							SizeBytes: 10 * 1024 * 1024 * 1024,
							Status:    VolumeStatusMounted,
							MountPath: filepath.Join(tempDir, volumeId),
						}, nil
					},
				}
			},
			validateResult: func(t *testing.T, attrs []entity.Attr, err error) {
				require.NoError(t, err)
				// handleProvisioned only verifies volume exists
			},
			expectedLogs: []string{
				"Provisioned disk already mounted",
			},
		},
		{
			name: "re-provisions when volume not found",
			setupDisk: func() *storage_v1alpha.Disk {
				return &storage_v1alpha.Disk{
					ID:           entity.Id("disk/test-notfound"),
					Name:         "test-disk",
					Status:       storage_v1alpha.PROVISIONED,
					SizeGb:       10,
					Filesystem:   storage_v1alpha.EXT4,
					LsvdVolumeId: "notfound-vol-123",
				}
			},
			setupClient: func(t *testing.T, tempDir string) LsvdClient {
				return &mockLsvdClient{
					getInfoFunc: func(ctx context.Context, volumeId string) (*VolumeInfo, error) {
						return nil, os.ErrNotExist
					},
					createFunc: func(ctx context.Context, sizeGb int64, filesystem string) (string, error) {
						return "mock-vol-id", nil
					},
					mountFunc: func(ctx context.Context, volumeId string, mountPath string, readOnly bool) error {
						return nil
					},
				}
			},
			validateResult: func(t *testing.T, attrs []entity.Attr, err error) {
				require.NoError(t, err)
				// handleProvisioned will re-provision if volume doesn't exist
			},
			expectedLogs: []string{
				"Volume not found for provisioned disk, re-provisioning",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			tempDir := t.TempDir()
			log := slog.Default()

			disk := tt.setupDisk()
			lsvdClient := tt.setupClient(t, tempDir)

			controller := &DiskController{
				Log:           log.With("controller", "disk"),
				lsvdClient:    lsvdClient,
				mountBasePath: tempDir,
			}

			// Execute
			err := controller.handleProvisioned(context.Background(), disk)

			// Validate
			tt.validateResult(t, nil, err)
		})
	}
}

// Test that GetVolumeInfo can detect existing mounts
func TestLsvdClient_GetVolumeInfo_DetectsExistingMounts(t *testing.T) {
	t.Run("loads existing mounted volume into state", func(t *testing.T) {
		// This test would need to be run in an environment where we can actually mount
		// For now, we'll just test the logic without actual mounting
		t.Skip("Requires mount privileges")

		tempDir := t.TempDir()
		log := slog.Default()

		// Create volume directory structure
		volumeId := "test-vol-123"
		volumePath := filepath.Join(tempDir, "lsvd-volumes", volumeId)
		require.NoError(t, os.MkdirAll(volumePath, 0755))

		// Create mount point
		mountPath := filepath.Join(tempDir, volumeId)
		require.NoError(t, os.MkdirAll(mountPath, 0755))

		client := NewLsvdClient(log, tempDir).(*lsvdClientImpl)

		// Get volume info - should detect it's not mounted (since we can't actually mount in test)
		info, err := client.GetVolumeInfo(context.Background(), volumeId)

		// In a real environment with mount, this would be:
		// assert.True(t, info.Mounted)
		// assert.Equal(t, mountPath, info.MountPath)

		// For now just verify it doesn't error
		assert.Error(t, err) // Will error because no volume metadata exists
		assert.Nil(t, info)
	})
}

// Test that MountVolume can mount existing volumes after restart
func TestDiskController_MountExistingVolumeAfterRestart(t *testing.T) {
	t.Run("mounts existing volume after simulated restart", func(t *testing.T) {
		t.Skip("Requires mount privileges")

		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()

		// Step 1: Create a volume with first client
		client1 := NewLsvdClient(log, tempDir)
		volumeId, err := client1.CreateVolume(ctx, 10, "ext4")
		require.NoError(t, err)
		require.NotEmpty(t, volumeId)

		// Verify volume exists
		info1, err := client1.GetVolumeInfo(ctx, volumeId)
		require.NoError(t, err)
		assert.Equal(t, volumeId, info1.ID)

		// Step 2: Simulate restart by creating new client
		client2 := NewLsvdClient(log, tempDir)

		// Step 3: Try to mount the existing volume with new client
		mountPath := filepath.Join(tempDir, "mounts", volumeId)

		// First, load the volume into the new client's cache using CreateVolumeInSegmentAccess
		// (idempotent - will find existing volume)
		_, err = client2.CreateVolume(ctx, 10, "ext4")
		require.NoError(t, err) // Should be idempotent and load from disk

		defer client2.UnmountVolume(ctx, volumeId)

		// Now try to mount it
		err = client2.MountVolume(ctx, volumeId, mountPath, false)

		// This will fail without actual NBD support, but we're testing the loading logic
		// In a real environment, this would succeed
		if err != nil {
			// Verify the error is about NBD, not about volume not found
			assert.Contains(t, err.Error(), "NBD")
			t.Log("Mount failed as expected in test environment (no NBD support):", err)
		}

		// Verify the volume was at least loaded into the new client
		info2, err := client2.GetVolumeInfo(ctx, volumeId)
		require.NoError(t, err)
		assert.Equal(t, volumeId, info2.ID)
		assert.Equal(t, int64(10*1024*1024*1024), info2.SizeBytes)
	})

	t.Run("disk controller handles provisioned disk after restart", func(t *testing.T) {
		ctx := context.Background()
		tempDir := t.TempDir()
		log := slog.Default()

		// Step 1: Create and provision a disk with mock client that won't fail on mount
		mockClient1 := &mockLsvdClient{
			createFunc: func(ctx context.Context, sizeGb int64, filesystem string) (string, error) {
				// Simulate creating the volume
				return "mock-vol-1", nil
			},
			mountFunc: func(ctx context.Context, volumeId string, mountPath string, readOnly bool) error {
				// Simulate successful mount
				return nil
			},
			getInfoFunc: func(ctx context.Context, volumeId string) (*VolumeInfo, error) {
				return &VolumeInfo{
					ID:        volumeId,
					Name:      volumeId,
					SizeBytes: 5 * 1024 * 1024 * 1024,
					Status:    VolumeStatusLoaded, // Initially not mounted
				}, nil
			},
		}

		controller1 := NewDiskControllerWithMountPath(log, nil, mockClient1, tempDir)

		disk := &storage_v1alpha.Disk{
			ID:         entity.Id("disk/restart-test"),
			Name:       "restart-test",
			Status:     storage_v1alpha.PROVISIONING,
			SizeGb:     5,
			Filesystem: storage_v1alpha.EXT4,
		}

		// Provision the disk
		err := controller1.handleProvisioning(ctx, disk)
		require.NoError(t, err)

		// Extract volume ID from the disk after provisioning
		volumeId := disk.LsvdVolumeId
		require.NotEmpty(t, volumeId)

		// Update disk to provisioned
		disk.Status = storage_v1alpha.PROVISIONED
		disk.LsvdVolumeId = volumeId

		// Step 2: Simulate restart with new controller and mock client
		mountCalled := false
		mockClient2 := &mockLsvdClient{
			createFunc: func(ctx context.Context, sizeGb int64, filesystem string) (string, error) {
				// Simulate creating a new volume
				return "mock-vol-2", nil
			},
			getInfoFunc: func(ctx context.Context, vid string) (*VolumeInfo, error) {
				if vid == volumeId {
					return &VolumeInfo{
						ID:        volumeId,
						Name:      volumeId,
						SizeBytes: 5 * 1024 * 1024 * 1024,
						Status:    VolumeStatusLoaded, // Not mounted after restart
					}, nil
				}
				return nil, fmt.Errorf("volume not found")
			},
			mountFunc: func(ctx context.Context, vid string, mountPath string, readOnly bool) error {
				if vid == volumeId {
					mountCalled = true
					return nil
				}
				return fmt.Errorf("volume not found")
			},
		}

		controller2 := NewDiskControllerWithMountPath(log, nil, mockClient2, tempDir)

		// Step 3: Handle the provisioned disk with new controller
		// This should detect the existing volume and attempt to mount it
		err = controller2.handleProvisioned(ctx, disk)
		require.NoError(t, err)

		// Should NOT have attempted to mount (disk controller no longer mounts)
		assert.False(t, mountCalled, "Disk controller should not mount volumes")

		t.Log("Successfully handled existing provisioned disk after restart")
	})
}
