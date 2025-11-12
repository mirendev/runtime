//go:build linux && integration

package disk

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// TestLsvdClientMountIntegration tests actual NBD mounting
// This test requires:
// - Root privileges (for NBD and mount operations)
// - Linux environment with NBD module available
// - mkfs.ext4, mkfs.xfs, mkfs.btrfs utilities installed
//
// Run with: sudo go test -tags=integration ./controllers/disk -run TestLsvdClientMountIntegration -v
func TestLsvdClientMountIntegration(t *testing.T) {
	// Skip if not running as root
	if os.Geteuid() != 0 {
		t.Skip("Test requires root privileges")
	}

	// Check if we're on Linux
	var uname unix.Utsname
	if err := unix.Uname(&uname); err != nil {
		t.Skip("Cannot determine OS, skipping")
	}

	t.Run("mount ext4 volume", func(t *testing.T) {
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir)

		ctx := context.Background()

		// Create a small volume for testing (1GB)
		volumeId := "test-mount-ext4"
		err := client.CreateVolume(ctx, volumeId, 1, "ext4")
		require.NoError(t, err)

		// Mount the volume
		mountPath := filepath.Join(tempDir, "mounts", volumeId)
		err = client.MountVolume(ctx, volumeId, mountPath, false)
		require.NoError(t, err)

		// Verify mount is active
		info, err := client.GetVolumeInfo(ctx, volumeId)
		require.NoError(t, err)
		assert.True(t, info.Mounted)
		assert.Equal(t, mountPath, info.MountPath)

		// Write a test file
		testFile := filepath.Join(mountPath, "test.txt")
		err = os.WriteFile(testFile, []byte("Hello LSVD!"), 0644)
		require.NoError(t, err)

		// Read the test file
		data, err := os.ReadFile(testFile)
		require.NoError(t, err)
		assert.Equal(t, "Hello LSVD!", string(data))

		// Unmount the volume
		err = client.UnmountVolume(ctx, volumeId)
		require.NoError(t, err)

		// Verify unmounted
		info, err = client.GetVolumeInfo(ctx, volumeId)
		require.NoError(t, err)
		assert.False(t, info.Mounted)

		// Mount again to verify persistence
		err = client.MountVolume(ctx, volumeId, mountPath, false)
		require.NoError(t, err)

		// Check if file still exists
		data, err = os.ReadFile(testFile)
		require.NoError(t, err)
		assert.Equal(t, "Hello LSVD!", string(data))

		// Clean up
		err = client.UnmountVolume(ctx, volumeId)
		require.NoError(t, err)

		err = client.UnprovisionVolume(ctx, volumeId)
		require.NoError(t, err)
	})

	t.Run("mount xfs volume", func(t *testing.T) {
		// Check if mkfs.xfs is available
		if _, err := os.Stat("/sbin/mkfs.xfs"); os.IsNotExist(err) {
			t.Skip("mkfs.xfs not available")
		}

		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir)

		ctx := context.Background()

		// Create XFS volume
		volumeId := "test-mount-xfs"
		err := client.CreateVolume(ctx, volumeId, 1, "xfs")
		require.NoError(t, err)

		// Mount the volume
		mountPath := filepath.Join(tempDir, "mounts", volumeId)
		err = client.MountVolume(ctx, volumeId, mountPath, false)
		require.NoError(t, err)

		// Write and read test
		testFile := filepath.Join(mountPath, "xfs-test.txt")
		testData := []byte("XFS filesystem test")
		err = os.WriteFile(testFile, testData, 0644)
		require.NoError(t, err)

		data, err := os.ReadFile(testFile)
		require.NoError(t, err)
		assert.Equal(t, testData, data)

		// Clean up
		err = client.UnmountVolume(ctx, volumeId)
		require.NoError(t, err)

		err = client.UnprovisionVolume(ctx, volumeId)
		require.NoError(t, err)
	})

	t.Run("mount read-only volume", func(t *testing.T) {
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir)

		ctx := context.Background()

		// Create volume
		volumeId := "test-mount-ro"
		err := client.CreateVolume(ctx, volumeId, 1, "ext4")
		require.NoError(t, err)

		// Mount as read-write first
		mountPath := filepath.Join(tempDir, "mounts", volumeId)
		err = client.MountVolume(ctx, volumeId, mountPath, false)
		require.NoError(t, err)

		// Write a file
		testFile := filepath.Join(mountPath, "readonly.txt")
		err = os.WriteFile(testFile, []byte("Read-only test"), 0644)
		require.NoError(t, err)

		// Unmount
		err = client.UnmountVolume(ctx, volumeId)
		require.NoError(t, err)

		// Remount as read-only
		err = client.MountVolume(ctx, volumeId, mountPath, true)
		require.NoError(t, err)

		// Try to write (should fail)
		err = os.WriteFile(testFile, []byte("Should fail"), 0644)
		assert.Error(t, err)

		// But reading should work
		data, err := os.ReadFile(testFile)
		require.NoError(t, err)
		assert.Equal(t, "Read-only test", string(data))

		// Clean up
		err = client.UnmountVolume(ctx, volumeId)
		require.NoError(t, err)

		err = client.UnprovisionVolume(ctx, volumeId)
		require.NoError(t, err)
	})

	t.Run("concurrent mount attempts", func(t *testing.T) {
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir)

		ctx := context.Background()

		// Create volume
		volumeId := "test-concurrent"
		err := client.CreateVolume(ctx, volumeId, 1, "ext4")
		require.NoError(t, err)

		// First mount should succeed
		mountPath1 := filepath.Join(tempDir, "mounts", "mount1")
		err = client.MountVolume(ctx, volumeId, mountPath1, false)
		require.NoError(t, err)

		// Second mount should fail (already mounted)
		mountPath2 := filepath.Join(tempDir, "mounts", "mount2")
		err = client.MountVolume(ctx, volumeId, mountPath2, false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already mounted")

		// Clean up
		err = client.UnmountVolume(ctx, volumeId)
		require.NoError(t, err)

		err = client.UnprovisionVolume(ctx, volumeId)
		require.NoError(t, err)
	})
}

// TestNBDDeviceManagement tests NBD device creation and cleanup
func TestNBDDeviceManagement(t *testing.T) {
	// Skip if not running as root
	if os.Geteuid() != 0 {
		t.Skip("Test requires root privileges")
	}

	t.Run("nbd module loading", func(t *testing.T) {
		tempDir := t.TempDir()
		log := slog.Default()
		client := NewLsvdClient(log, tempDir).(*lsvdClientImpl)

		// Ensure NBD module is loaded
		err := client.ensureNBDModule()
		require.NoError(t, err)

		// Check module is loaded
		_, err = os.Stat("/sys/module/nbd")
		assert.NoError(t, err)
	})

	t.Run("nbd range detection", func(t *testing.T) {
		rng, err := nbdRange()
		require.NoError(t, err)
		assert.Greater(t, rng, 0)
	})
}

// BenchmarkVolumeMount benchmarks volume mount operation
func BenchmarkVolumeMount(b *testing.B) {
	if os.Geteuid() != 0 {
		b.Skip("Benchmark requires root privileges")
	}

	tempDir := b.TempDir()
	log := slog.New(slog.NewTextHandler(os.Discard, nil))
	client := NewLsvdClient(log, tempDir)

	ctx := context.Background()

	// Create volume once
	volumeId := "bench-mount"
	err := client.CreateVolume(ctx, volumeId, 1, "ext4")
	require.NoError(b, err)

	mountPath := filepath.Join(tempDir, "mounts", volumeId)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Mount
		err := client.MountVolume(ctx, volumeId, mountPath, false)
		if err != nil {
			b.Fatal(err)
		}

		// Unmount
		err = client.UnmountVolume(ctx, volumeId)
		if err != nil {
			b.Fatal(err)
		}
	}

	// Cleanup
	client.UnprovisionVolume(ctx, volumeId)
}
