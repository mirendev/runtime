package diskio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/snapshot"
)

// newImageWatcherFixture builds a watcher over one universal-mode volume with a
// backing image already written.
func newImageWatcherFixture(t *testing.T, content []byte) (*ImageWatcher, *fakeUpdatesClient, *VolumeState) {
	t.Helper()

	diskPath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(diskPath, "disk.img"), content, 0644))

	vol := &VolumeState{
		EntityId:   "volume-1",
		VolumeId:   "vol-1",
		Name:       "data",
		DiskPath:   diskPath,
		SizeBytes:  int64(len(content)),
		Filesystem: "ext4",
		Mode:       storage_v1alpha.VM_UNIVERSAL,
	}

	state := NewState()
	state.SetVolume(vol.EntityId, vol)

	fake := &fakeUpdatesClient{}
	watcher := NewImageWatcher(slog.Default(), state, fake, time.Hour)

	return watcher, fake, vol
}

func TestImageWatcherUploadsUniversalVolume(t *testing.T) {
	content := bytes.Repeat([]byte("disk contents "), 512)
	watcher, fake, vol := newImageWatcherFixture(t, content)

	watcher.snapshotAll(context.Background())

	require.Len(t, fake.uploads, 1)
	up := fake.uploads[0]
	assert.Equal(t, "vol-1", up.VolumeID)
	assert.Equal(t, KindLoopImage, up.Request.Kind)

	// The ordering key must be 16 lowercase hex, which is what the cloud
	// accepts for this kind
	assert.Len(t, up.Request.OrderingKey, 16)
	assert.Regexp(t, "^[0-9a-f]{16}$", up.Request.OrderingKey)

	assert.Equal(t, "zstd", up.Request.Metadata["compression"])
	assert.Equal(t, "ext4", up.Request.Metadata["filesystem"])
	assert.Equal(t, int64(len(content)), up.Request.Metadata["image_size"])

	// The payload is a real snapshot: header, then the image compressed
	meta, err := snapshot.ReadHeader(bytes.NewReader(up.Body))
	require.NoError(t, err)
	assert.Equal(t, "data", meta.Name)
	assert.Equal(t, int64(len(content)), meta.SizeBytes)
	assert.Equal(t, "ext4", meta.Filesystem)
	assert.Equal(t, meta.Checksum, up.Request.Metadata["image_sha256"])

	// And a marker records what was sent
	marker := readImageMarker(vol.DiskPath)
	require.NotNil(t, marker)
	assert.Equal(t, int64(len(content)), marker.SizeBytes)
	assert.Equal(t, up.Request.OrderingKey, marker.OrderingKey)
}

// Re-uploading an unchanged image would re-send the whole thing for nothing.
func TestImageWatcherSkipsUnchangedImage(t *testing.T) {
	watcher, fake, _ := newImageWatcherFixture(t, []byte("stable contents"))

	watcher.snapshotAll(context.Background())
	require.Len(t, fake.uploads, 1)

	watcher.snapshotAll(context.Background())
	assert.Len(t, fake.uploads, 1, "an unchanged image should not be uploaded again")
}

func TestImageWatcherUploadsAgainWhenImageChanges(t *testing.T) {
	watcher, fake, vol := newImageWatcherFixture(t, []byte("first contents"))

	watcher.snapshotAll(context.Background())
	require.Len(t, fake.uploads, 1)

	// Rewrite the image with different contents and a later mtime
	imagePath := filepath.Join(vol.DiskPath, "disk.img")
	require.NoError(t, os.WriteFile(imagePath, []byte("second contents, longer"), 0644))
	later := time.Now().Add(time.Minute)
	require.NoError(t, os.Chtimes(imagePath, later, later))

	watcher.snapshotAll(context.Background())
	require.Len(t, fake.uploads, 2)

	// Ordering keys must advance so replay order is well defined
	assert.Greater(t, fake.uploads[1].Request.OrderingKey, fake.uploads[0].Request.OrderingKey)
}

// Accelerator volumes ship their writes as lbd log segments; snapshotting their
// image as well would duplicate the whole disk.
func TestImageWatcherIgnoresAcceleratorVolumes(t *testing.T) {
	watcher, fake, vol := newImageWatcherFixture(t, []byte("contents"))
	vol.Mode = storage_v1alpha.VM_ACCELERATOR

	watcher.snapshotAll(context.Background())
	assert.Empty(t, fake.uploads)
}

func TestImageWatcherIgnoresMissingImage(t *testing.T) {
	watcher, fake, vol := newImageWatcherFixture(t, []byte("contents"))
	require.NoError(t, os.Remove(filepath.Join(vol.DiskPath, "disk.img")))

	watcher.snapshotAll(context.Background())
	assert.Empty(t, fake.uploads, "a volume with no image yet has nothing to back up")
}

// A failed upload must not record a marker, or the next tick would skip the
// volume and the snapshot would be lost.
func TestImageWatcherRetriesAfterFailedUpload(t *testing.T) {
	watcher, fake, vol := newImageWatcherFixture(t, []byte("contents"))
	fake.uploadErr = errors.New("cloud unavailable")

	watcher.snapshotAll(context.Background())
	assert.Nil(t, readImageMarker(vol.DiskPath), "no marker should be written for a failed upload")

	fake.uploadErr = nil
	watcher.snapshotAll(context.Background())
	assert.Len(t, fake.uploads, 1, "the next tick should retry")
}

// The staging file is large; leaving one behind every tick would fill the disk.
func TestImageWatcherCleansUpStagingFiles(t *testing.T) {
	watcher, _, vol := newImageWatcherFixture(t, bytes.Repeat([]byte("x"), 4096))

	watcher.snapshotAll(context.Background())

	entries, err := os.ReadDir(vol.DiskPath)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), "image-snapshot", "staging file left behind: %s", e.Name())
	}
}

func TestImageWatcherIncludesLeaseNonce(t *testing.T) {
	watcher, fake, _ := newImageWatcherFixture(t, []byte("contents"))
	watcher.state.SetMount("mount-1", &MountState{
		EntityId:   "mount-1",
		VolumeId:   "vol-1",
		LeaseNonce: "nonce-abc",
	})

	watcher.snapshotAll(context.Background())

	require.Len(t, fake.uploads, 1)
	assert.Equal(t, "nonce-abc", fake.uploads[0].Request.LeaseNonce)
}

func TestImageWatcherWithoutCloudDoesNothing(t *testing.T) {
	watcher, _, vol := newImageWatcherFixture(t, []byte("contents"))
	watcher.updates = nil

	watcher.snapshotAll(context.Background())
	assert.Nil(t, readImageMarker(vol.DiskPath))
}

func TestImageWatcherRunRejectsNonPositiveInterval(t *testing.T) {
	watcher, _, _ := newImageWatcherFixture(t, []byte("contents"))
	watcher.interval = 0

	err := watcher.Run(context.Background())
	require.ErrorContains(t, err, "must be positive")
}

func TestImageWatcherRunStopsOnContextCancel(t *testing.T) {
	watcher, _, _ := newImageWatcherFixture(t, []byte("contents"))
	watcher.interval = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	require.NoError(t, watcher.Run(ctx))
	watcher.Wait()
}

func TestImageMarkerRoundTrips(t *testing.T) {
	dir := t.TempDir()
	assert.Nil(t, readImageMarker(dir), "no marker reads as never uploaded")

	written := imageMarker{
		SizeBytes:   4096,
		ModTime:     time.Now().Truncate(time.Second),
		OrderingKey: "0000000000000001",
	}
	require.NoError(t, writeImageMarker(dir, written))

	got := readImageMarker(dir)
	require.NotNil(t, got)
	assert.Equal(t, written.SizeBytes, got.SizeBytes)
	assert.True(t, written.ModTime.Equal(got.ModTime))
	assert.Equal(t, written.OrderingKey, got.OrderingKey)

	// A corrupt marker reads as never uploaded rather than failing the tick
	require.NoError(t, os.WriteFile(imageMarkerPath(dir), []byte("{not json"), 0644))
	assert.Nil(t, readImageMarker(dir))

	// And no staging file is left behind by the atomic replace
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp")
	}

	// Sanity: the marker really is JSON
	data, err := os.ReadFile(imageMarkerPath(dir))
	require.NoError(t, err)
	assert.Error(t, json.Unmarshal(data, &written), "the corrupt marker should not parse")
}
