package diskio

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/snapshot"
)

// restoreUpdatesClient serves a fixed set of updates and their payloads.
type restoreUpdatesClient struct {
	updates  []UpdateInfo
	payloads map[string][]byte
	listErr  error
	dlErr    error

	listedKinds []UpdateKind
}

func (r *restoreUpdatesClient) Upload(ctx context.Context, volumeID string, req UploadRequest, body io.Reader, size int64) (string, error) {
	return "", errors.New("not implemented")
}

func (r *restoreUpdatesClient) List(ctx context.Context, volumeID string, opts ListOptions) ([]UpdateInfo, error) {
	r.listedKinds = append(r.listedKinds, opts.Kind)
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.updates, nil
}

func (r *restoreUpdatesClient) Download(ctx context.Context, volumeID, updateID string) (io.ReadCloser, error) {
	if r.dlErr != nil {
		return nil, r.dlErr
	}
	payload, ok := r.payloads[updateID]
	if !ok {
		return nil, errors.New("update not found")
	}
	return io.NopCloser(bytes.NewReader(payload)), nil
}

// buildSnapshot produces a real .miren.zst payload for the given image bytes.
func buildSnapshot(t *testing.T, name string, image []byte) []byte {
	t.Helper()

	staged, err := os.CreateTemp(t.TempDir(), "snap-*")
	require.NoError(t, err)
	defer staged.Close()

	_, err = snapshot.Backup(staged, bytes.NewReader(image), name, int64(len(image)), "ext4")
	require.NoError(t, err)

	_, err = staged.Seek(0, 0)
	require.NoError(t, err)

	data, err := io.ReadAll(staged)
	require.NoError(t, err)
	return data
}

func newRestoreFixture(t *testing.T) (*DiskMountController, *VolumeState, string) {
	t.Helper()

	root := t.TempDir()
	diskPath := filepath.Join(root, "vol")
	require.NoError(t, os.MkdirAll(diskPath, 0755))

	mc := NewDiskMountController(slog.Default(), root,
		compute_v1alpha.NewNodeId("node-1"), NewState(), newMockDiskMountOps())

	vol := &VolumeState{
		VolumeId:   "vol-1",
		Name:       "data",
		DiskPath:   diskPath,
		Filesystem: "ext4",
	}
	return mc, vol, filepath.Join(diskPath, "disk.img")
}

func TestRestoreImageRebuildsMissingImage(t *testing.T) {
	mc, vol, imagePath := newRestoreFixture(t)

	image := bytes.Repeat([]byte("original disk contents "), 256)
	client := &restoreUpdatesClient{
		updates: []UpdateInfo{
			{UpdateID: "volup-old", Kind: "loop_image", OrderingKey: "0000000000000001"},
			{UpdateID: "volup-new", Kind: "loop_image", OrderingKey: "0000000000000002"},
		},
		payloads: map[string][]byte{
			"volup-old": buildSnapshot(t, "data", []byte("stale contents")),
			"volup-new": buildSnapshot(t, "data", image),
		},
	}
	mc.SetUpdatesClient(client)

	require.NoError(t, mc.RestoreImageIfMissing(context.Background(), vol, imagePath))

	// The newest snapshot wins
	restored, err := os.ReadFile(imagePath)
	require.NoError(t, err)
	assert.Equal(t, image, restored)

	assert.Equal(t, []UpdateKind{KindLoopImage}, client.listedKinds,
		"only loop images are candidates for a universal volume")
}

// The local image is by definition newer than anything the cloud holds, so a
// restore must never overwrite one.
func TestRestoreImageLeavesExistingImageAlone(t *testing.T) {
	mc, vol, imagePath := newRestoreFixture(t)

	live := []byte("live contents that must survive")
	require.NoError(t, os.WriteFile(imagePath, live, 0644))

	client := &restoreUpdatesClient{
		updates:  []UpdateInfo{{UpdateID: "volup-1", Kind: "loop_image", OrderingKey: "0000000000000001"}},
		payloads: map[string][]byte{"volup-1": buildSnapshot(t, "data", []byte("cloud contents"))},
	}
	mc.SetUpdatesClient(client)

	require.NoError(t, mc.RestoreImageIfMissing(context.Background(), vol, imagePath))

	got, err := os.ReadFile(imagePath)
	require.NoError(t, err)
	assert.Equal(t, live, got)
	assert.Empty(t, client.listedKinds, "an existing image should not even prompt a listing")
}

// A volume that has never been backed up is a fresh volume, not a lost one.
func TestRestoreImageNoSnapshotsIsNotAnError(t *testing.T) {
	mc, vol, imagePath := newRestoreFixture(t)
	mc.SetUpdatesClient(&restoreUpdatesClient{})

	require.NoError(t, mc.RestoreImageIfMissing(context.Background(), vol, imagePath))
	assert.NoFileExists(t, imagePath)
}

func TestRestoreImageWithoutCloudIsNotAnError(t *testing.T) {
	mc, vol, imagePath := newRestoreFixture(t)

	require.NoError(t, mc.RestoreImageIfMissing(context.Background(), vol, imagePath))
	assert.NoFileExists(t, imagePath)
}

func TestRestoreImageSurfacesListFailure(t *testing.T) {
	mc, vol, imagePath := newRestoreFixture(t)
	mc.SetUpdatesClient(&restoreUpdatesClient{listErr: errors.New("cloud unavailable")})

	err := mc.RestoreImageIfMissing(context.Background(), vol, imagePath)
	require.ErrorContains(t, err, "listing image snapshots")
}

func TestRestoreImageSurfacesDownloadFailure(t *testing.T) {
	mc, vol, imagePath := newRestoreFixture(t)
	mc.SetUpdatesClient(&restoreUpdatesClient{
		updates: []UpdateInfo{{UpdateID: "volup-1", Kind: "loop_image", OrderingKey: "0000000000000001"}},
		dlErr:   errors.New("blob gone"),
	})

	err := mc.RestoreImageIfMissing(context.Background(), vol, imagePath)
	require.ErrorContains(t, err, "downloading image snapshot")
}

// A truncated payload must not leave something that looks like a usable image.
func TestRestoreImageLeavesNothingBehindOnCorruptPayload(t *testing.T) {
	mc, vol, imagePath := newRestoreFixture(t)

	full := buildSnapshot(t, "data", bytes.Repeat([]byte("contents "), 512))
	client := &restoreUpdatesClient{
		updates:  []UpdateInfo{{UpdateID: "volup-1", Kind: "loop_image", OrderingKey: "0000000000000001"}},
		payloads: map[string][]byte{"volup-1": full[:len(full)/2]},
	}
	mc.SetUpdatesClient(client)

	err := mc.RestoreImageIfMissing(context.Background(), vol, imagePath)
	require.Error(t, err)

	assert.NoFileExists(t, imagePath)
	assert.NoFileExists(t, imagePath+".restoring")
}
