package diskio

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/snapshot"
)

func newSnapshotFixture(t *testing.T, content []byte) (*ImageSnapshotter, *fakeUpdatesClient, string) {
	t.Helper()

	dir := t.TempDir()
	imagePath := filepath.Join(dir, "disk.img")
	require.NoError(t, os.WriteFile(imagePath, content, 0644))

	fake := &fakeUpdatesClient{}
	return NewImageSnapshotter(slog.Default(), fake), fake, imagePath
}

func TestImageSnapshotUploadsCompressedImage(t *testing.T) {
	content := bytes.Repeat([]byte("disk contents "), 512)
	snapshotter, fake, imagePath := newSnapshotFixture(t, content)

	updateID, err := snapshotter.Snapshot(context.Background(), SnapshotRequest{
		VolumeID:     "vol-1",
		ImagePath:    imagePath,
		Name:         "data",
		Filesystem:   "ext4",
		SnapshotName: "pre-migration",
	})
	require.NoError(t, err)
	assert.Equal(t, "volup-fake", updateID)

	require.Len(t, fake.uploads, 1)
	up := fake.uploads[0]
	assert.Equal(t, "vol-1", up.VolumeID)
	assert.Equal(t, KindLoopImage, up.Request.Kind)
	assert.Equal(t, "pre-migration", up.Request.SnapshotName)

	// The ordering key must be 16 lowercase hex, which is what the cloud
	// accepts for this kind
	assert.Regexp(t, "^[0-9a-f]{16}$", up.Request.OrderingKey)

	assert.Equal(t, "zstd", up.Request.Metadata["compression"])
	assert.Equal(t, "ext4", up.Request.Metadata["filesystem"])
	assert.Equal(t, int64(len(content)), up.Request.Metadata["image_size"])

	// The payload is a real snapshot: header, then the image compressed
	meta, err := snapshot.ReadHeader(bytes.NewReader(up.Body))
	require.NoError(t, err)
	assert.Equal(t, "data", meta.Name)
	assert.Equal(t, int64(len(content)), meta.SizeBytes)
	assert.Equal(t, meta.Checksum, up.Request.Metadata["image_sha256"])
}

// Every snapshot is a deliberate act, so two in a row both upload. There is no
// skip-if-unchanged heuristic: that only existed to spare a timer.
func TestImageSnapshotAlwaysUploads(t *testing.T) {
	snapshotter, fake, imagePath := newSnapshotFixture(t, []byte("stable contents"))

	for range 2 {
		_, err := snapshotter.Snapshot(context.Background(), SnapshotRequest{
			VolumeID: "vol-1", ImagePath: imagePath, Filesystem: "ext4",
		})
		require.NoError(t, err)
	}

	require.Len(t, fake.uploads, 2)
	assert.Greater(t, fake.uploads[1].Request.OrderingKey, fake.uploads[0].Request.OrderingKey,
		"ordering keys must advance so replay order is well defined")
}

func TestImageSnapshotRequiresVolumeAndClient(t *testing.T) {
	snapshotter, _, imagePath := newSnapshotFixture(t, []byte("contents"))

	_, err := snapshotter.Snapshot(context.Background(), SnapshotRequest{ImagePath: imagePath})
	require.ErrorContains(t, err, "volume ID is required")

	none := NewImageSnapshotter(slog.Default(), nil)
	_, err = none.Snapshot(context.Background(), SnapshotRequest{VolumeID: "vol-1", ImagePath: imagePath})
	require.ErrorContains(t, err, "no cloud updates client")
}

func TestImageSnapshotSurfacesMissingImage(t *testing.T) {
	snapshotter, fake, _ := newSnapshotFixture(t, []byte("contents"))

	_, err := snapshotter.Snapshot(context.Background(), SnapshotRequest{
		VolumeID: "vol-1", ImagePath: "/nonexistent/disk.img",
	})
	require.ErrorContains(t, err, "stat image")
	assert.Empty(t, fake.uploads)
}

func TestImageSnapshotSurfacesUploadFailure(t *testing.T) {
	snapshotter, fake, imagePath := newSnapshotFixture(t, []byte("contents"))
	fake.uploadErr = errors.New("cloud unavailable")

	_, err := snapshotter.Snapshot(context.Background(), SnapshotRequest{
		VolumeID: "vol-1", ImagePath: imagePath,
	})
	require.ErrorContains(t, err, "cloud unavailable")
}

// The staging file is as large as the compressed image; leaving one behind
// every backup would fill the disk.
func TestImageSnapshotCleansUpStagingFile(t *testing.T) {
	snapshotter, _, imagePath := newSnapshotFixture(t, bytes.Repeat([]byte("x"), 4096))
	dir := filepath.Dir(imagePath)

	_, err := snapshotter.Snapshot(context.Background(), SnapshotRequest{
		VolumeID: "vol-1", ImagePath: imagePath, Filesystem: "ext4",
	})
	require.NoError(t, err)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), "image-snapshot", "staging file left behind: %s", e.Name())
	}
}

// A caller whose image lives on a small filesystem can stage elsewhere.
func TestImageSnapshotHonoursStagingDir(t *testing.T) {
	snapshotter, _, imagePath := newSnapshotFixture(t, []byte("contents"))
	staging := t.TempDir()

	_, err := snapshotter.Snapshot(context.Background(), SnapshotRequest{
		VolumeID: "vol-1", ImagePath: imagePath, Filesystem: "ext4", StagingDir: staging,
	})
	require.NoError(t, err)

	entries, err := os.ReadDir(staging)
	require.NoError(t, err)
	assert.Empty(t, entries, "the staging directory should be left clean")
}
