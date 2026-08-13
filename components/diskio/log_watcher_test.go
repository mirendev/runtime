package diskio

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/storage/storage_v1alpha"
)

type mockUploader struct {
	uploaded []mockUploadCall
	err      error
	// failPaths fails only the named segments, so a test can make one segment
	// in a run fail while the rest would otherwise succeed.
	failPaths map[string]error
}

type mockUploadCall struct {
	volumeID    string
	segmentPath string
}

func (m *mockUploader) UploadSegment(_ context.Context, volumeID, segmentPath string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	if err, ok := m.failPaths[filepath.Base(segmentPath)]; ok {
		return "", err
	}
	m.uploaded = append(m.uploaded, mockUploadCall{volumeID: volumeID, segmentPath: segmentPath})
	return "seg-id-" + filepath.Base(segmentPath), nil
}

func TestLogWatcherScanAndUpload(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a volume with accelerator mode
	logDir := filepath.Join(tmpDir, "vol1", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create completed .log files
	for _, name := range []string{"seg-001.log", "seg-002.log"} {
		if err := os.WriteFile(filepath.Join(logDir, name), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create an in-progress .log.tmp file (should be skipped)
	if err := os.WriteFile(filepath.Join(logDir, "seg-003.log.tmp"), []byte("partial"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a non-log file (should be skipped)
	if err := os.WriteFile(filepath.Join(logDir, "metadata.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	state := NewState()
	state.SetVolume("disk_volume/vol1", &VolumeState{
		EntityId:      "disk_volume/vol1",
		VolumeId:      "vol1",
		CloudVolumeId: "cloud-vol1",
		DiskPath:      filepath.Join(tmpDir, "vol1"),
		Mode:          storage_v1alpha.VM_ACCELERATOR,
	})

	uploader := &mockUploader{}
	watcher := NewLogWatcher(slog.Default(), state, uploader, time.Second)

	// Manually call scanAndUpload
	watcher.scanAndUpload(context.Background())

	// Should have uploaded exactly 2 completed .log files
	if len(uploader.uploaded) != 2 {
		t.Fatalf("expected 2 uploads, got %d", len(uploader.uploaded))
	}

	// Verify the uploaded files
	uploadedPaths := make(map[string]bool)
	for _, u := range uploader.uploaded {
		// Uploads address the cloud's id for the volume, not the local one
		if u.volumeID != "cloud-vol1" {
			t.Errorf("expected volumeID 'cloud-vol1', got %q", u.volumeID)
		}
		uploadedPaths[filepath.Base(u.segmentPath)] = true
	}

	if !uploadedPaths["seg-001.log"] {
		t.Error("seg-001.log was not uploaded")
	}
	if !uploadedPaths["seg-002.log"] {
		t.Error("seg-002.log was not uploaded")
	}

	// Verify completed segments were deleted
	if _, err := os.Stat(filepath.Join(logDir, "seg-001.log")); !os.IsNotExist(err) {
		t.Error("seg-001.log should have been deleted after upload")
	}
	if _, err := os.Stat(filepath.Join(logDir, "seg-002.log")); !os.IsNotExist(err) {
		t.Error("seg-002.log should have been deleted after upload")
	}

	// Verify .tmp file was not deleted
	if _, err := os.Stat(filepath.Join(logDir, "seg-003.log.tmp")); err != nil {
		t.Error("seg-003.log.tmp should still exist")
	}

	// Verify non-log file was not deleted
	if _, err := os.Stat(filepath.Join(logDir, "metadata.json")); err != nil {
		t.Error("metadata.json should still exist")
	}
}

func TestLogWatcherSkipsUniversalVolumes(t *testing.T) {
	tmpDir := t.TempDir()

	logDir := filepath.Join(tmpDir, "vol1", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(logDir, "seg-001.log"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	state := NewState()
	state.SetVolume("disk_volume/vol1", &VolumeState{
		EntityId:      "disk_volume/vol1",
		VolumeId:      "vol1",
		CloudVolumeId: "cloud-vol1",
		DiskPath:      filepath.Join(tmpDir, "vol1"),
		Mode:          storage_v1alpha.VM_UNIVERSAL,
	})

	uploader := &mockUploader{}
	watcher := NewLogWatcher(slog.Default(), state, uploader, time.Second)

	watcher.scanAndUpload(context.Background())

	if len(uploader.uploaded) != 0 {
		t.Fatalf("expected 0 uploads for universal volume, got %d", len(uploader.uploaded))
	}
}

func TestLogWatcherNilUploaderDeletesOnly(t *testing.T) {
	tmpDir := t.TempDir()

	logDir := filepath.Join(tmpDir, "vol1", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create completed .log files
	for _, name := range []string{"seg-001.log", "seg-002.log"} {
		if err := os.WriteFile(filepath.Join(logDir, name), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create an in-progress .log.tmp file (should be skipped)
	if err := os.WriteFile(filepath.Join(logDir, "seg-003.log.tmp"), []byte("partial"), 0644); err != nil {
		t.Fatal(err)
	}

	state := NewState()
	state.SetVolume("disk_volume/vol1", &VolumeState{
		EntityId:      "disk_volume/vol1",
		VolumeId:      "vol1",
		CloudVolumeId: "cloud-vol1",
		DiskPath:      filepath.Join(tmpDir, "vol1"),
		Mode:          storage_v1alpha.VM_ACCELERATOR,
	})

	// nil uploader = delete-only mode
	watcher := NewLogWatcher(slog.Default(), state, nil, time.Second)

	watcher.scanAndUpload(context.Background())

	// Completed segments should be deleted
	if _, err := os.Stat(filepath.Join(logDir, "seg-001.log")); !os.IsNotExist(err) {
		t.Error("seg-001.log should have been deleted")
	}
	if _, err := os.Stat(filepath.Join(logDir, "seg-002.log")); !os.IsNotExist(err) {
		t.Error("seg-002.log should have been deleted")
	}

	// .tmp file should still exist
	if _, err := os.Stat(filepath.Join(logDir, "seg-003.log.tmp")); err != nil {
		t.Error("seg-003.log.tmp should still exist")
	}
}

func TestLogWatcherUploadErrorLeavesFile(t *testing.T) {
	tmpDir := t.TempDir()

	logDir := filepath.Join(tmpDir, "vol1", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(logDir, "seg-001.log"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	state := NewState()
	state.SetVolume("disk_volume/vol1", &VolumeState{
		EntityId:      "disk_volume/vol1",
		VolumeId:      "vol1",
		CloudVolumeId: "cloud-vol1",
		DiskPath:      filepath.Join(tmpDir, "vol1"),
		Mode:          storage_v1alpha.VM_ACCELERATOR,
	})

	uploader := &mockUploader{err: os.ErrPermission}
	watcher := NewLogWatcher(slog.Default(), state, uploader, time.Second)

	watcher.scanAndUpload(context.Background())

	// File should still exist since upload failed
	if _, err := os.Stat(filepath.Join(logDir, "seg-001.log")); err != nil {
		t.Error("seg-001.log should still exist after failed upload")
	}
}

// Until a volume is registered with the cloud there is nowhere to send its
// segments, and deleting them would throw away data that was never backed up.
func TestLogWatcherDefersUnregisteredVolume(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "vol1", "logs")
	require.NoError(t, os.MkdirAll(logDir, 0755))

	segPath := filepath.Join(logDir, "seg-001.log")
	require.NoError(t, os.WriteFile(segPath, []byte("data"), 0644))

	state := NewState()
	state.SetVolume("disk_volume/vol1", &VolumeState{
		EntityId: "disk_volume/vol1",
		VolumeId: "vol1",
		DiskPath: filepath.Join(tmpDir, "vol1"),
		Mode:     storage_v1alpha.VM_ACCELERATOR,
	})

	uploader := &mockUploader{}
	NewLogWatcher(slog.Default(), state, uploader, time.Second).
		scanAndUpload(context.Background())

	assert.Empty(t, uploader.uploaded, "nothing should be uploaded without a cloud id")
	assert.FileExists(t, segPath, "the segment must survive for a later scan")
}

// The horizon is a high-water mark, not a cursor. If a failed segment were
// skipped and a later one uploaded, the horizon would step over the gap, and
// replay (which asks only for what sorts above the horizon) would never request
// the skipped segment again. That is a silent hole in the restore chain.
func TestLogWatcherStopsAtTheFirstFailedSegment(t *testing.T) {
	tmpDir := t.TempDir()
	diskPath := filepath.Join(tmpDir, "vol1")
	logDir := filepath.Join(diskPath, "logs")
	require.NoError(t, os.MkdirAll(logDir, 0755))

	// TAI64N labels sort chronologically, and ReadDir returns them sorted, so
	// "first" here means both first on disk and first in time.
	first := "disk.40000000682f1a2c1dcd6500.log"
	second := "disk.40000000682f1a2c1dcd6501.log"
	for _, name := range []string{first, second} {
		require.NoError(t, os.WriteFile(filepath.Join(logDir, name), []byte("data"), 0644))
	}

	state := NewState()
	state.SetVolume("disk_volume/vol1", &VolumeState{
		EntityId:      "disk_volume/vol1",
		VolumeId:      "vol1",
		CloudVolumeId: "cloud-vol1",
		DiskPath:      diskPath,
		Mode:          storage_v1alpha.VM_ACCELERATOR,
	})

	uploader := &mockUploader{failPaths: map[string]error{first: os.ErrPermission}}
	NewLogWatcher(slog.Default(), state, uploader, time.Second).
		scanAndUpload(context.Background())

	assert.Empty(t, uploader.uploaded, "the later segment must not upload past the gap")
	assert.FileExists(t, filepath.Join(logDir, first))
	assert.FileExists(t, filepath.Join(logDir, second))

	horizon, err := readLogHorizon(diskPath)
	require.NoError(t, err)
	assert.Empty(t, horizon, "the horizon must not move past a segment that never uploaded")

	// Once the failure clears, both go up and the horizon lands on the newest
	uploader.failPaths = nil
	NewLogWatcher(slog.Default(), state, uploader, time.Second).
		scanAndUpload(context.Background())

	require.Len(t, uploader.uploaded, 2)
	horizon, err = readLogHorizon(diskPath)
	require.NoError(t, err)
	assert.Equal(t, "40000000682f1a2c1dcd6501", horizon)
}
