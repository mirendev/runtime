package diskio

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/storage/storage_v1alpha"
)

// Run a scan window synchronously for the segment retirement tests.
func (w *LogWatcher) scanAndUpload(ctx context.Context) {
	for _, vol := range w.state.ListVolumes() {
		paths := w.scanVolume(ctx, vol, nil)
		for len(paths) > 0 {
			paths = w.scanVolume(ctx, vol, paths)
		}
	}
}

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

type logUploaderFunc func(context.Context, string, string) (string, error)

func (f logUploaderFunc) UploadSegment(ctx context.Context, id, path string) (string, error) {
	return f(ctx, id, path)
}

func TestLogWatcherBoundsVolumesAndJoinsUploads(t *testing.T) {
	state := NewState()
	const key = "400000000000000100000001"
	for i := 0; i < logVolumeConcurrency+2; i++ {
		id := fmt.Sprintf("vol-%d", i)
		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, "logs"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "logs", "disk."+key+".log"), []byte(id), 0600))
		state.SetVolume(id, &VolumeState{EntityId: id, VolumeId: id, CloudVolumeId: id, DiskPath: dir, Mode: storage_v1alpha.VM_ACCELERATOR})
	}
	started := make(chan string, 16)
	cancelled := make(chan struct{}, 16)
	allowOne := make(chan struct{})
	exit := make(chan struct{})
	uploader := logUploaderFunc(func(ctx context.Context, id, path string) (string, error) {
		started <- id
		select {
		case <-allowOne:
			return id, nil
		case <-ctx.Done():
			cancelled <- struct{}{}
			<-exit
			return "", ctx.Err()
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	watch := NewLogWatcher(slog.Default(), state, uploader, time.Millisecond)
	runErr := make(chan error, 1)
	go func() { runErr <- watch.Run(ctx) }()
	t.Cleanup(func() { cancel(); close(exit); watch.Wait() })
	seen := make(map[string]bool)
	for i := 0; i < logVolumeConcurrency; i++ {
		select {
		case id := <-started:
			require.False(t, seen[id], "one scan per volume even across ticks")
			seen[id] = true
		case <-time.After(5 * time.Second):
			t.Fatal("independent volumes did not start concurrently")
		}
	}
	select {
	case id := <-started:
		t.Fatalf("volume %s exceeded concurrency bound", id)
	case <-time.After(25 * time.Millisecond):
	}
	allowOne <- struct{}{}
	select {
	case id := <-started:
		require.False(t, seen[id], "pending volume must receive the freed slot")
	case <-time.After(5 * time.Second):
		t.Fatal("pending volume was not serviced")
	}
	cancel()
	for i := 0; i < logVolumeConcurrency; i++ {
		select {
		case <-cancelled:
		case <-time.After(5 * time.Second):
			t.Fatal("upload did not observe cancellation")
		}
	}
	select {
	case <-runErr:
		t.Fatal("Run returned before its uploads exited")
	default:
	}
	// Cleanup releases the uploads; Wait must not return before that.
}

func TestLogWatcherRotatesBackloggedVolumes(t *testing.T) {
	state := NewState()
	const count = uploadConcurrency*3 + 1
	addVolume := func(id string, count int) string {
		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, "logs"), 0755))
		for i := 1; i <= count; i++ {
			name := fmt.Sprintf("disk.4000000000000001%08x.log", i)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "logs", name), []byte(id), 0600))
		}
		state.SetVolume(id, &VolumeState{EntityId: id, CloudVolumeId: id, DiskPath: dir, Mode: storage_v1alpha.VM_ACCELERATOR})
		return dir
	}
	for i := 0; i < logVolumeConcurrency; i++ {
		addVolume(fmt.Sprintf("old-%d", i), count)
	}
	started := make(chan struct{}, logVolumeConcurrency)
	gate := make(chan struct{})
	tail := make(chan struct{})
	events := make(chan mockUploadCall, logVolumeConcurrency*count+1)
	uploader := logUploaderFunc(func(ctx context.Context, id, path string) (string, error) {
		if filepath.Base(path) == "disk.400000000000000100000001.log" && id != "new" {
			started <- struct{}{}
			select {
			case <-gate:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		if filepath.Base(path) == fmt.Sprintf("disk.4000000000000001%08x.log", uploadConcurrency+1) {
			select {
			case <-tail:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		events <- mockUploadCall{id, filepath.Base(path)}
		return id, nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	watch := NewLogWatcher(slog.Default(), state, uploader, time.Millisecond)
	go func() { assert.NoError(t, watch.Run(ctx)) }()
	t.Cleanup(func() { cancel(); watch.Wait() })
	for i := 0; i < logVolumeConcurrency; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("backlogs did not start")
		}
	}
	newDir := addVolume("new", 1)
	// Leave discovery ticks running while all slots are occupied.
	time.Sleep(25 * time.Millisecond)
	close(gate)
	require.Eventually(t, func() bool {
		horizon, _ := readLogHorizon(newDir)
		return horizon == "400000000000000100000001"
	}, 5*time.Second, time.Millisecond)
	close(tail)
	calls := make(map[string][]string)
	for i := 0; i < logVolumeConcurrency*count+1; i++ {
		select {
		case call := <-events:
			if call.volumeID == "new" {
				for _, old := range calls {
					require.LessOrEqual(t, len(old), uploadConcurrency, "new disk waits at most one transport batch, not a scan window")
				}
			}
			calls[call.volumeID] = append(calls[call.volumeID], call.segmentPath)
		case <-time.After(5 * time.Second):
			t.Fatal("cached backlog window did not finish")
		}
	}
	for i := 0; i < logVolumeConcurrency; i++ {
		for j, path := range calls[fmt.Sprintf("old-%d", i)] {
			require.Equal(t, fmt.Sprintf("disk.4000000000000001%08x.log", j+1), path)
		}
	}
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

func TestLogWatcherCleansUpSegmentsAtOrBeforeHorizon(t *testing.T) {
	diskPath := filepath.Join(t.TempDir(), "vol1")
	logDir := filepath.Join(diskPath, "logs")
	require.NoError(t, os.MkdirAll(logDir, 0755))

	older := "disk.40000000682f1a2c1dcd6500.log"
	first := "disk.40000000682f1a2c1dcd6501.log"
	newer := "disk.40000000682f1a2c1dcd6502.log"
	require.NoError(t, os.WriteFile(filepath.Join(logDir, first), []byte("data"), 0644))

	state := NewState()
	state.SetVolume("disk_volume/vol1", &VolumeState{
		EntityId:      "disk_volume/vol1",
		VolumeId:      "vol1",
		CloudVolumeId: "cloud-vol1",
		DiskPath:      diskPath,
		Mode:          storage_v1alpha.VM_ACCELERATOR,
	})
	uploader := &mockUploader{}
	var logs bytes.Buffer
	watcher := NewLogWatcher(slog.New(slog.NewTextHandler(&logs, nil)), state, uploader, time.Second)
	watcher.scanAndUpload(context.Background())
	require.Len(t, uploader.uploaded, 1)

	horizon, err := readLogHorizon(diskPath)
	require.NoError(t, err)
	require.Equal(t, "40000000682f1a2c1dcd6501", horizon)

	// A leftover segment at the horizon (or older) must not be claimed again.
	// The uploader rejects both so a retry would block the newer segment.
	for _, name := range []string{older, first, newer} {
		require.NoError(t, os.WriteFile(filepath.Join(logDir, name), []byte("data"), 0644))
	}
	uploader.failPaths = map[string]error{older: os.ErrExist, first: os.ErrExist}
	watcher.scanAndUpload(context.Background())

	require.Len(t, uploader.uploaded, 2)
	assert.Equal(t, newer, filepath.Base(uploader.uploaded[1].segmentPath))
	for _, name := range []string{older, first, newer} {
		assert.NoFileExists(t, filepath.Join(logDir, name))
	}
	assert.Contains(t, logs.String(), "removed segment covered by log horizon without upload")
	assert.Contains(t, logs.String(), older)
	assert.Contains(t, logs.String(), first)
	horizon, err = readLogHorizon(diskPath)
	require.NoError(t, err)
	assert.Equal(t, "40000000682f1a2c1dcd6502", horizon)
}
