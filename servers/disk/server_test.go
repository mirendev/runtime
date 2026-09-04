package disk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/components/diskio"
	"miren.dev/runtime/pkg/snapshot"
)

// fakeDisks stands in for the entity store.
type fakeDisks struct {
	disk    *snapshot.DiskState
	volume  *snapshot.VolumeState
	leases  []snapshot.LeaseState
	created *snapshot.RestoreTarget
}

func (f *fakeDisks) FindDisk(_ context.Context, name string) (*snapshot.DiskState, error) {
	if f.disk == nil {
		return nil, fmt.Errorf("disk %q not found", name)
	}
	return f.disk, nil
}

func (f *fakeDisks) FindVolume(context.Context, string) (*snapshot.VolumeState, error) {
	if f.volume == nil {
		return nil, errors.New("no volume")
	}
	return f.volume, nil
}

func (f *fakeDisks) FindLeases(context.Context, string) ([]snapshot.LeaseState, error) {
	return f.leases, nil
}

func (f *fakeDisks) CreateDiskAndVolume(context.Context, string, int64, string, string) (*snapshot.RestoreTarget, error) {
	if f.created == nil {
		return nil, errors.New("creation not configured")
	}
	return f.created, nil
}

// fakeUpdates stands in for miren.cloud.
type fakeUpdates struct {
	uploaded []diskio.UploadRequest
	bodies   [][]byte
	list     []diskio.UpdateInfo
	download map[string][]byte

	listErr     error
	downloadErr error
}

func (f *fakeUpdates) Upload(_ context.Context, _ string, req diskio.UploadRequest, body io.Reader, size int64) (string, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	if int64(len(data)) != size {
		return "", fmt.Errorf("declared %d bytes but body is %d", size, len(data))
	}
	f.uploaded = append(f.uploaded, req)
	f.bodies = append(f.bodies, data)
	return "volup-test", nil
}

func (f *fakeUpdates) List(context.Context, string, diskio.ListOptions) ([]diskio.UpdateInfo, error) {
	return f.list, f.listErr
}

func (f *fakeUpdates) Download(_ context.Context, _, updateID string) (io.ReadCloser, error) {
	if f.downloadErr != nil {
		return nil, f.downloadErr
	}
	data, ok := f.download[updateID]
	if !ok {
		return nil, fmt.Errorf("no such update %q", updateID)
	}
	return io.NopCloser(newReader(data)), nil
}

// fakeMountOps reports whether an image is loop-attached.
type fakeMountOps struct {
	diskio.DiskMountOps
	device string
	err    error
}

func (f fakeMountOps) FindLoopByBacking(string) (string, error) { return f.device, f.err }

// newTestServer builds a server over a temp data directory holding one disk
// image, and returns the server, its fakes, and the image path.
func newTestServer(t *testing.T, content []byte) (*Server, *fakeDisks, *fakeUpdates, string) {
	t.Helper()

	dataPath := t.TempDir()
	volDir := filepath.Join(dataPath, "disk-data", "volumes", "vol-1")
	require.NoError(t, os.MkdirAll(volDir, 0700))
	imagePath := filepath.Join(volDir, "disk.img")
	require.NoError(t, os.WriteFile(imagePath, content, 0644))

	disks := &fakeDisks{
		disk: &snapshot.DiskState{
			ID: "disk/1", Name: "data", Status: "PROVISIONED", Filesystem: "ext4",
		},
		volume: &snapshot.VolumeState{
			VolumeID: "vol-1", CloudVolumeID: "cloud-vol-1", ImagePath: imagePath,
		},
	}
	updates := &fakeUpdates{download: map[string][]byte{}}

	s := &Server{
		log:      slog.Default(),
		disks:    disks,
		dataPath: dataPath,
		updates:  updates,
		mntOps:   fakeMountOps{},
	}
	return s, disks, updates, imagePath
}

func TestPrepareBackupRequiresAName(t *testing.T) {
	s, _, _, _ := newTestServer(t, []byte("hello"))

	_, err := s.prepareBackup(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk name is required")
}

func TestPrepareBackupResolvesTheImage(t *testing.T) {
	s, _, _, imagePath := newTestServer(t, []byte("hello"))

	target, err := s.prepareBackup(context.Background(), "data")
	require.NoError(t, err)
	assert.Equal(t, imagePath, target.ImagePath)
	assert.Equal(t, "cloud-vol-1", target.CloudVolumeID)
}

// A cluster with no cloud must say so rather than failing obscurely, and it
// must point at the thing that would fix it.
func TestNoCloudErrorNamesTheRemedy(t *testing.T) {
	err := errNoCloud("backing up to miren.cloud")
	assert.Contains(t, err.Error(), "backing up to miren.cloud")
	assert.Contains(t, err.Error(), "miren register")
	assert.Contains(t, err.Error(), "local file")
}

// Backup of an unregistered disk is what makes an air-gapped cluster work, so
// resolution must succeed and simply report no cloud volume.
func TestPrepareBackupToleratesAnUnregisteredDisk(t *testing.T) {
	s, disks, _, imagePath := newTestServer(t, []byte("hello"))
	disks.volume.CloudVolumeID = ""

	target, err := s.prepareBackup(context.Background(), "data")
	require.NoError(t, err)
	assert.Equal(t, imagePath, target.ImagePath)
	assert.Empty(t, target.CloudVolumeID)
}

func TestRestorePointCarriesATimestampDecodedFromTheOrderingKey(t *testing.T) {
	when := time.Unix(0, 1_700_000_000_123_456_789)

	rp := restorePointFromUpdate(diskio.UpdateInfo{
		UpdateID:     "u-1",
		Kind:         string(diskio.KindLoopImage),
		OrderingKey:  fmt.Sprintf("%016x", when.UnixNano()),
		Size:         4096,
		SnapshotName: "pre-migration",
	})

	assert.Equal(t, "u-1", rp.Id())
	assert.Equal(t, int64(4096), rp.SizeBytes())
	assert.Equal(t, "pre-migration", rp.Name())
	assert.Equal(t, "universal", rp.Mode())
	require.True(t, rp.HasCreatedAt())
	assert.Equal(t, when.Unix(), rp.CreatedAt().Seconds())
	assert.Equal(t, int32(when.Nanosecond()), rp.CreatedAt().Nanoseconds())
}

// An accelerator-mode segment has a TAI64N key, which is not Unix nanoseconds.
// Reporting a garbage date would be worse than reporting none.
func TestRestorePointOmitsATimestampItCannotDecode(t *testing.T) {
	rp := restorePointFromUpdate(diskio.UpdateInfo{
		UpdateID:    "u-2",
		Kind:        string(diskio.KindLBDLog),
		OrderingKey: "400000005f5e1000",
	})

	assert.Equal(t, "accelerator", rp.Mode())
	assert.False(t, rp.HasCreatedAt())
}

func TestLiveImageDeviceReportsAnAttachedLoop(t *testing.T) {
	s, _, _, imagePath := newTestServer(t, []byte("hello"))
	s.mntOps = fakeMountOps{device: "/dev/loop3"}

	dev, err := s.liveImageDevice(imagePath)
	require.NoError(t, err)
	assert.Equal(t, "/dev/loop3", dev)
}

// Not knowing whether an image is in use is not the same as knowing it is idle.
func TestLiveImageDeviceFailsClosed(t *testing.T) {
	s, _, _, imagePath := newTestServer(t, []byte("hello"))
	s.mntOps = fakeMountOps{err: errors.New("sysfs unreadable")}

	_, err := s.liveImageDevice(imagePath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sysfs unreadable")
}

// The refusal is the whole point of the check: renaming an image over a live
// loop device is silently a no-op, so a restore that "succeeded" would have
// changed nothing.
func TestRestoreRefusesALiveImage(t *testing.T) {
	s, _, _, imagePath := newTestServer(t, []byte("hello"))
	s.mntOps = fakeMountOps{device: "/dev/loop7"}

	err := s.refuseLiveImage(&snapshot.RestoreTarget{ImagePath: imagePath}, "data")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/dev/loop7")
	assert.Contains(t, err.Error(), "nothing reads")
}

func TestRestoreAllowsAnIdleImage(t *testing.T) {
	s, _, _, imagePath := newTestServer(t, []byte("hello"))

	require.NoError(t, s.refuseLiveImage(&snapshot.RestoreTarget{ImagePath: imagePath}, "data"))
}

// A backup written by this server must be restorable by it, byte for byte.
func TestWriteImageRoundTripsASnapshot(t *testing.T) {
	content := make([]byte, 128*1024)
	for i := range content {
		content[i] = byte(i % 251)
	}

	s, _, _, imagePath := newTestServer(t, content)

	staged := filepath.Join(t.TempDir(), "snap.miren.zst")
	out, err := os.Create(staged)
	require.NoError(t, err)

	img, err := os.Open(imagePath)
	require.NoError(t, err)
	defer img.Close()

	_, err = snapshot.Backup(out, img, "data", int64(len(content)), "ext4")
	require.NoError(t, err)
	require.NoError(t, out.Close())

	src, err := os.Open(staged)
	require.NoError(t, err)
	defer src.Close()

	meta, err := snapshot.ReadHeader(src)
	require.NoError(t, err)

	restored := filepath.Join(t.TempDir(), "restored.img")
	prog := s.newProgress(context.Background(), nil)
	require.NoError(t, s.writeImage(restored, src, 0, meta, prog))

	got, err := os.ReadFile(restored)
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

// An interrupted restore must not leave something that looks like a finished
// image.
func TestWriteImageLeavesNothingBehindOnACorruptSnapshot(t *testing.T) {
	s, _, _, _ := newTestServer(t, []byte("hello"))

	restored := filepath.Join(t.TempDir(), "restored.img")
	prog := s.newProgress(context.Background(), nil)

	meta := &snapshot.Meta{Name: "data", SizeBytes: 4096, Checksum: "deadbeef"}
	err := s.writeImage(restored, newReader([]byte("not a zstd stream")), 0, meta, prog)
	require.Error(t, err)

	_, statErr := os.Stat(restored)
	assert.True(t, os.IsNotExist(statErr), "a failed restore must not leave an image behind")

	_, tmpErr := os.Stat(restored + ".restore.tmp")
	assert.True(t, os.IsNotExist(tmpErr), "a failed restore must not leave its temp file behind")
}

func newReader(b []byte) io.Reader { return &sliceReader{b: b} }

type sliceReader struct {
	b []byte
	i int
}

func (r *sliceReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}
