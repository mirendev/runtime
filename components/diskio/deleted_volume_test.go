package diskio

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveAndLoadDeletedVolumeMetadata(t *testing.T) {
	dir := t.TempDir()

	meta := &DeletedVolumeMetadata{
		DiskID:     "disk/disk-abc123",
		DiskName:   "my-database",
		SizeGb:     10,
		Filesystem: "ext4",
		VolumeID:   "disk-vol-xyz",
		VolumeMode: "volume_mode.vm_universal",
		CreatedBy:  "user/user-1",
		NodeID:     compute.NewNodeId("node-1"),
		DeletedAt:  time.Now().Truncate(time.Millisecond),
	}

	err := SaveDeletedVolumeMetadata(dir, meta)
	require.NoError(t, err)

	// Verify file exists
	_, err = os.Stat(filepath.Join(dir, metadataFilename))
	require.NoError(t, err)

	loaded, err := LoadDeletedVolumeMetadata(dir)
	require.NoError(t, err)

	assert.Equal(t, meta.DiskID, loaded.DiskID)
	assert.Equal(t, meta.DiskName, loaded.DiskName)
	assert.Equal(t, meta.SizeGb, loaded.SizeGb)
	assert.Equal(t, meta.Filesystem, loaded.Filesystem)
	assert.Equal(t, meta.VolumeID, loaded.VolumeID)
	assert.Equal(t, meta.VolumeMode, loaded.VolumeMode)
	assert.Equal(t, meta.CreatedBy, loaded.CreatedBy)
	assert.Equal(t, meta.NodeID, loaded.NodeID)
	assert.True(t, meta.DeletedAt.Equal(loaded.DeletedAt))
}

func TestLoadDeletedVolumeMetadata_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadDeletedVolumeMetadata(dir)
	assert.Error(t, err)
}

func TestListDeletedVolumes(t *testing.T) {
	dataPath := t.TempDir()
	deletedDir := filepath.Join(dataPath, deletedVolumesDir)
	require.NoError(t, os.MkdirAll(deletedDir, 0755))

	// Create two deleted volume entries
	vol1Dir := filepath.Join(deletedDir, "vol-1")
	require.NoError(t, os.MkdirAll(vol1Dir, 0755))
	require.NoError(t, SaveDeletedVolumeMetadata(vol1Dir, &DeletedVolumeMetadata{
		DiskName:  "db-disk",
		VolumeID:  "vol-1",
		DeletedAt: time.Now().Add(-1 * time.Hour),
	}))

	vol2Dir := filepath.Join(deletedDir, "vol-2")
	require.NoError(t, os.MkdirAll(vol2Dir, 0755))
	require.NoError(t, SaveDeletedVolumeMetadata(vol2Dir, &DeletedVolumeMetadata{
		DiskName:  "cache-disk",
		VolumeID:  "vol-2",
		DeletedAt: time.Now().Add(-48 * time.Hour),
	}))

	// Create a directory without metadata (should be skipped)
	badDir := filepath.Join(deletedDir, "no-metadata")
	require.NoError(t, os.MkdirAll(badDir, 0755))

	entries, err := ListDeletedVolumes(dataPath)
	require.NoError(t, err)
	assert.Len(t, entries, 2)

	names := make(map[string]bool)
	for _, e := range entries {
		names[e.Metadata.DiskName] = true
	}
	assert.True(t, names["db-disk"])
	assert.True(t, names["cache-disk"])
}

func TestListDeletedVolumes_EmptyDir(t *testing.T) {
	dataPath := t.TempDir()
	entries, err := ListDeletedVolumes(dataPath)
	require.NoError(t, err)
	assert.Nil(t, entries)
}
