package diskio

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/snapshot"
)

// restoreUpdatesClient serves a fixed set of updates and their payloads.
type restoreUpdatesClient struct {
	updates  []UpdateInfo
	payloads map[string][]byte
	listErr  error
	dlErr    error

	listedKinds []UpdateKind
	listedOpts  []ListOptions
}

func (r *restoreUpdatesClient) Upload(ctx context.Context, volumeID string, req UploadRequest, body io.Reader, size int64) (string, error) {
	return "", errors.New("not implemented")
}

func (r *restoreUpdatesClient) List(ctx context.Context, volumeID string, opts ListOptions) ([]UpdateInfo, error) {
	r.listedKinds = append(r.listedKinds, opts.Kind)
	r.listedOpts = append(r.listedOpts, opts)
	if r.listErr != nil {
		return nil, r.listErr
	}

	// Imitate the server: r.updates is held in ascending ordering-key order, so
	// a descending request gets the reverse, and Limit truncates. A double that
	// ignored these would hide the restore path asking for the wrong end.
	out := slices.Clone(r.updates)
	if opts.Descending {
		slices.Reverse(out)
	}
	if opts.Limit > 0 && len(out) > opts.Limit {
		out = out[:opts.Limit]
	}
	return out, nil
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
		VolumeId:      "vol-1",
		CloudVolumeId: "cloud-vol-1",
		Name:          "data",
		DiskPath:      diskPath,
		Filesystem:    "ext4",
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

	// One row from the newest end, rather than a walk of the whole history
	require.Len(t, client.listedOpts, 1)
	assert.True(t, client.listedOpts[0].Descending)
	assert.Equal(t, 1, client.listedOpts[0].Limit)
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

// An unregistered volume has nothing in the cloud to restore from, and asking
// with an empty id would just be a bad request.
func TestRestoreImageSkipsUnregisteredVolume(t *testing.T) {
	mc, vol, imagePath := newRestoreFixture(t)
	vol.CloudVolumeId = ""

	client := &restoreUpdatesClient{
		updates:  []UpdateInfo{{UpdateID: "volup-1", Kind: "loop_image", OrderingKey: "0000000000000001"}},
		payloads: map[string][]byte{"volup-1": buildSnapshot(t, "data", []byte("cloud contents"))},
	}
	mc.SetUpdatesClient(client)

	require.NoError(t, mc.RestoreImageIfMissing(context.Background(), vol, imagePath))
	assert.NoFileExists(t, imagePath)
	assert.Empty(t, client.listedKinds)
}

// Universal volumes are mounted by the volume controller, not the mount
// controller, so the restore hook has to live on that path or it never runs for
// the one mode that needs it.
func TestVolumeControllerRestoresImageBeforeMounting(t *testing.T) {
	root := t.TempDir()
	diskPath := filepath.Join(root, "volumes", "vol-1")
	require.NoError(t, os.MkdirAll(diskPath, 0755))
	imagePath := filepath.Join(diskPath, "disk.img")

	image := bytes.Repeat([]byte("recovered contents "), 256)
	client := &restoreUpdatesClient{
		updates:  []UpdateInfo{{UpdateID: "volup-1", Kind: "loop_image", OrderingKey: "0000000000000001"}},
		payloads: map[string][]byte{"volup-1": buildSnapshot(t, "data", image)},
	}

	mntOps := newMockDiskMountOps()
	vc := NewDiskVolumeController(slog.Default(), root, compute_v1alpha.NewNodeId("node-1"),
		NewState(), newMockDiskVolumeOps(), mntOps)
	vc.SetUpdatesClient(client)

	volState := &VolumeState{
		EntityId:      "disk_volume/vol-1",
		VolumeId:      "vol-1",
		CloudVolumeId: "cloud-vol-1",
		DiskPath:      diskPath,
		Filesystem:    "ext4",
		Mode:          storage_v1alpha.VM_UNIVERSAL,
	}

	require.NoError(t, vc.ensureVolumeMount(context.Background(), volState.EntityId, volState))

	restored, err := os.ReadFile(imagePath)
	require.NoError(t, err)
	assert.Equal(t, image, restored)
}
