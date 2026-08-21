package diskio

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateNewState(t *testing.T) {
	state := NewState()

	assert.NotNil(t, state.Volumes)
	assert.NotNil(t, state.Mounts)
	assert.Empty(t, state.Volumes)
	assert.Empty(t, state.Mounts)
}

func TestStateLoadNonExistent(t *testing.T) {
	tempDir := t.TempDir()

	state, err := LoadState(tempDir)
	require.NoError(t, err)
	assert.NotNil(t, state)
	assert.Empty(t, state.Volumes)
	assert.Empty(t, state.Mounts)
	assert.Equal(t, filepath.Join(tempDir, stateFileName), state.path)
}

func TestStateSaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()

	// Create state and add data
	state, err := LoadState(tempDir)
	require.NoError(t, err)

	state.SetVolume("vol-1", &VolumeState{
		EntityId:   "vol-1",
		VolumeId:   "uuid-1234",
		DiskPath:   "/var/lib/data/volumes/uuid-1234",
		SizeBytes:  1073741824, // 1GB
		Filesystem: "ext4",
		RemoteOnly: false,
	})

	state.SetMount("mount-1", &MountState{
		EntityId:   "mount-1",
		VolumeId:   "vol-1",
		DevicePath: "/dev/loop1",
		MountPath:  "/mnt/data",
		Mounted:    true,
		ReadOnly:   false,
		LeaseNonce: "nonce-abc",
	})

	// Save state
	err = state.Save()
	require.NoError(t, err)

	// Verify file exists
	statePath := filepath.Join(tempDir, stateFileName)
	_, err = os.Stat(statePath)
	require.NoError(t, err)

	// Load state in new instance
	loaded, err := LoadState(tempDir)
	require.NoError(t, err)

	// Verify volumes
	assert.Len(t, loaded.Volumes, 1)
	vol := loaded.GetVolume("vol-1")
	require.NotNil(t, vol)
	assert.Equal(t, "vol-1", vol.EntityId)
	assert.Equal(t, "uuid-1234", vol.VolumeId)
	assert.Equal(t, "/var/lib/data/volumes/uuid-1234", vol.DiskPath)
	assert.Equal(t, int64(1073741824), vol.SizeBytes)
	assert.Equal(t, "ext4", vol.Filesystem)
	assert.False(t, vol.RemoteOnly)

	// Verify mounts
	assert.Len(t, loaded.Mounts, 1)
	mnt := loaded.GetMount("mount-1")
	require.NotNil(t, mnt)
	assert.Equal(t, "mount-1", mnt.EntityId)
	assert.Equal(t, "vol-1", mnt.VolumeId)
	assert.Equal(t, "/dev/loop1", mnt.DevicePath)
	assert.Equal(t, "/mnt/data", mnt.MountPath)
	assert.True(t, mnt.Mounted)
	assert.False(t, mnt.ReadOnly)
	assert.Equal(t, "nonce-abc", mnt.LeaseNonce)
}

func TestStateDeleteVolume(t *testing.T) {
	state := NewState()

	state.SetVolume("vol-1", &VolumeState{EntityId: "vol-1"})
	state.SetVolume("vol-2", &VolumeState{EntityId: "vol-2"})

	assert.Len(t, state.Volumes, 2)
	assert.NotNil(t, state.GetVolume("vol-1"))

	state.DeleteVolume("vol-1")

	assert.Len(t, state.Volumes, 1)
	assert.Nil(t, state.GetVolume("vol-1"))
	assert.NotNil(t, state.GetVolume("vol-2"))
}

func TestStateDeleteMount(t *testing.T) {
	state := NewState()

	state.SetMount("mount-1", &MountState{EntityId: "mount-1"})
	state.SetMount("mount-2", &MountState{EntityId: "mount-2"})

	assert.Len(t, state.Mounts, 2)
	assert.NotNil(t, state.GetMount("mount-1"))

	state.DeleteMount("mount-1")

	assert.Len(t, state.Mounts, 1)
	assert.Nil(t, state.GetMount("mount-1"))
	assert.NotNil(t, state.GetMount("mount-2"))
}

func TestStateGetVolumeByVolumeId(t *testing.T) {
	state := NewState()

	state.SetVolume("vol-1", &VolumeState{EntityId: "vol-1", VolumeId: "uuid-1"})
	state.SetVolume("vol-2", &VolumeState{EntityId: "vol-2", VolumeId: "uuid-2"})

	vol := state.GetVolumeByVolumeId("uuid-2")
	require.NotNil(t, vol)
	assert.Equal(t, "vol-2", vol.EntityId)

	vol = state.GetVolumeByVolumeId("nonexistent")
	assert.Nil(t, vol)
}

func TestStateListVolumes(t *testing.T) {
	state := NewState()

	state.SetVolume("vol-1", &VolumeState{EntityId: "vol-1"})
	state.SetVolume("vol-2", &VolumeState{EntityId: "vol-2"})
	state.SetVolume("vol-3", &VolumeState{EntityId: "vol-3"})

	volumes := state.ListVolumes()
	assert.Len(t, volumes, 3)

	entityIds := make(map[string]bool)
	for _, v := range volumes {
		entityIds[v.EntityId] = true
	}
	assert.True(t, entityIds["vol-1"])
	assert.True(t, entityIds["vol-2"])
	assert.True(t, entityIds["vol-3"])
}

func TestStateListMounts(t *testing.T) {
	state := NewState()

	state.SetMount("mount-1", &MountState{EntityId: "mount-1"})
	state.SetMount("mount-2", &MountState{EntityId: "mount-2"})

	mounts := state.ListMounts()
	assert.Len(t, mounts, 2)

	entityIds := make(map[string]bool)
	for _, m := range mounts {
		entityIds[m.EntityId] = true
	}
	assert.True(t, entityIds["mount-1"])
	assert.True(t, entityIds["mount-2"])
}

func TestStateAtomicSave(t *testing.T) {
	tempDir := t.TempDir()

	state, err := LoadState(tempDir)
	require.NoError(t, err)

	// Add data
	state.SetVolume("vol-1", &VolumeState{EntityId: "vol-1"})
	err = state.Save()
	require.NoError(t, err)

	// Modify and save again
	state.SetVolume("vol-2", &VolumeState{EntityId: "vol-2"})
	err = state.Save()
	require.NoError(t, err)

	// Load and verify both exist
	loaded, err := LoadState(tempDir)
	require.NoError(t, err)
	assert.Len(t, loaded.Volumes, 2)

	// Verify no temp files left behind
	entries, err := os.ReadDir(tempDir)
	require.NoError(t, err)

	for _, entry := range entries {
		assert.NotContains(t, entry.Name(), ".tmp", "temp file should not exist after save")
	}
}

func TestStateConcurrentAccess(t *testing.T) {
	state := NewState()

	// Simulate concurrent access
	done := make(chan bool, 10)

	for i := range 5 {
		go func(idx int) {
			for range 100 {
				state.SetVolume("vol-1", &VolumeState{EntityId: "vol-1"})
				_ = state.GetVolume("vol-1")
			}
			done <- true
		}(i)
	}

	for i := range 5 {
		go func(idx int) {
			for range 100 {
				state.SetMount("mount-1", &MountState{EntityId: "mount-1"})
				_ = state.GetMount("mount-1")
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for range 10 {
		<-done
	}

	// State should still be valid
	assert.NotNil(t, state.GetVolume("vol-1"))
	assert.NotNil(t, state.GetMount("mount-1"))
}

func TestStateLeaseNonceForCloudVolume(t *testing.T) {
	t.Run("matches on the mount's stored cloud id", func(t *testing.T) {
		// No VolumeState at all — the mount alone carries the cloud id.
		state := NewState()
		state.SetMount("disk_mount/mnt-1", &MountState{
			EntityId:      "disk_mount/mnt-1",
			VolumeId:      "disk_volume/vol1",
			CloudVolumeId: "vol-cloud-1",
			LeaseNonce:    "nonce-1",
		})

		assert.Equal(t, "nonce-1", state.LeaseNonceForCloudVolume("vol-cloud-1"))
		assert.Empty(t, state.LeaseNonceForCloudVolume("vol-other"))
		assert.Empty(t, state.LeaseNonceForCloudVolume(""))
	})

	t.Run("falls back to volume state for mounts without a stored cloud id", func(t *testing.T) {
		// Models a mount persisted before CloudVolumeId was recorded on it.
		state := NewState()
		state.SetVolume("disk_volume/vol1", &VolumeState{
			EntityId:      "disk_volume/vol1",
			VolumeId:      "vol1",
			CloudVolumeId: "vol-cloud-1",
		})
		state.SetMount("disk_mount/mnt-1", &MountState{
			EntityId:   "disk_mount/mnt-1",
			VolumeId:   "disk_volume/vol1",
			LeaseNonce: "nonce-1",
		})

		assert.Equal(t, "nonce-1", state.LeaseNonceForCloudVolume("vol-cloud-1"))
	})
}

func TestLoadStateBackfillsMountCloudVolumeId(t *testing.T) {
	dir := t.TempDir()

	// Write a state file shaped like one persisted before MountState.CloudVolumeId
	// existed: the volume knows its cloud id, the mount does not.
	seed := NewState()
	seed.SetPath(dir)
	seed.SetVolume("disk_volume/vol1", &VolumeState{
		EntityId:      "disk_volume/vol1",
		VolumeId:      "vol1",
		CloudVolumeId: "vol-cloud-1",
	})
	seed.SetMount("disk_mount/mnt-1", &MountState{
		EntityId:   "disk_mount/mnt-1",
		VolumeId:   "disk_volume/vol1",
		LeaseNonce: "nonce-1",
	})
	require.NoError(t, seed.Save())

	loaded, err := LoadState(dir)
	require.NoError(t, err)

	// The mount now carries the cloud id, resolved from volume state.
	m := loaded.GetMount("disk_mount/mnt-1")
	require.NotNil(t, m)
	assert.Equal(t, "vol-cloud-1", m.CloudVolumeId)

	// The backfill was persisted, not just applied in memory. Read the file
	// directly so this is verified independently of a second load (which would
	// re-backfill from the still-present volume state and mask a missing save).
	raw, err := os.ReadFile(filepath.Join(dir, stateFileName))
	require.NoError(t, err)
	var persisted State
	require.NoError(t, json.Unmarshal(raw, &persisted))
	require.NotNil(t, persisted.Mounts["disk_mount/mnt-1"])
	assert.Equal(t, "vol-cloud-1", persisted.Mounts["disk_mount/mnt-1"].CloudVolumeId)

	// And the lease still resolves after the VolumeState is removed, which is
	// the orphan-cleanup situation the backfill protects against.
	loaded.DeleteVolume("disk_volume/vol1")
	assert.Equal(t, "nonce-1", loaded.LeaseNonceForCloudVolume("vol-cloud-1"))
}
