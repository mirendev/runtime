package diskio

import (
	"bytes"
	"context"
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/lbd"
	"miren.dev/runtime/api/storage/storage_v1alpha"
)

// mockCloudDiskClient implements CloudDiskClient for testing.
type mockCloudDiskClient struct {
	acquireLeaseNonce string
	acquireLeaseErr   error
	acquireLeaseCalls []string

	releaseLeaseErr   error
	releaseLeaseCalls []mockReleaseLease

	segments    []LogSegmentInfo
	segmentsErr error

	downloadData map[string][]byte
	downloadErr  error
}

type mockReleaseLease struct {
	volumeID string
	nonce    string
}

func (m *mockCloudDiskClient) AcquireLease(_ context.Context, volumeID string) (string, error) {
	m.acquireLeaseCalls = append(m.acquireLeaseCalls, volumeID)
	return m.acquireLeaseNonce, m.acquireLeaseErr
}

func (m *mockCloudDiskClient) ReleaseLease(_ context.Context, volumeID, nonce string) error {
	m.releaseLeaseCalls = append(m.releaseLeaseCalls, mockReleaseLease{volumeID, nonce})
	return m.releaseLeaseErr
}

func (m *mockCloudDiskClient) ListLogSegments(_ context.Context, _ string) ([]LogSegmentInfo, error) {
	return m.segments, m.segmentsErr
}

func (m *mockCloudDiskClient) DownloadLogSegment(_ context.Context, _, segmentID string) (io.ReadCloser, error) {
	if m.downloadErr != nil {
		return nil, m.downloadErr
	}
	data, ok := m.downloadData[segmentID]
	if !ok {
		return nil, fmt.Errorf("segment %s not found", segmentID)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// buildSegmentData creates a CBOR log segment with the given entries.
func buildSegmentData(t *testing.T, blockSize uint32, label string, entries []lbd.Entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := lbd.NewWriter(&buf, lbd.Header{
		Version:      2,
		BlockSize:    blockSize,
		SegmentLabel: label,
		DeviceSize:   1024 * 1024,
	})
	require.NoError(t, err)

	for i := range entries {
		require.NoError(t, w.WriteEntry(&entries[i]))
	}
	return buf.Bytes()
}

func TestReplayOneSegmentWriteEntries(t *testing.T) {
	tmpDir := t.TempDir()
	diskPath := filepath.Join(tmpDir, "disk.img")

	// Create a 64KB raw disk image
	f, err := os.Create(diskPath)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(64*1024))
	f.Close()

	blockSize := uint32(4096)
	writeData := bytes.Repeat([]byte{0xAB}, 4096)
	crc := crc32.ChecksumIEEE(writeData)

	segData := buildSegmentData(t, blockSize, "0001", []lbd.Entry{
		{Op: "W", Block: 0, Length: 4096, Checksum: crc, Data: writeData},
		{Op: "W", Block: 2, Length: 4096, Checksum: crc, Data: writeData},
	})

	cloud := &mockCloudDiskClient{
		downloadData: map[string][]byte{"seg-1": segData},
	}

	mc := NewDiskMountController(slog.Default(), tmpDir, compute.NewNodeId("node-1"), NewState(), newMockDiskMountOps())
	mc.SetCloudClient(cloud)

	img, err := openDiskImage(diskPath)
	require.NoError(t, err)
	defer img.Close()

	err = mc.replayOneSegment(context.Background(), "vol-1", "seg-1", img)
	require.NoError(t, err)
	require.NoError(t, img.Sync())
	img.Close()

	// Verify data was written at block 0 and block 2
	result, err := os.ReadFile(diskPath)
	require.NoError(t, err)

	assert.Equal(t, writeData, result[0:4096], "block 0 should contain write data")
	assert.Equal(t, make([]byte, 4096), result[4096:8192], "block 1 should be zeros")
	assert.Equal(t, writeData, result[8192:12288], "block 2 should contain write data")
}

func TestReplayMissingSegmentsFiltersbyHorizon(t *testing.T) {
	tmpDir := t.TempDir()
	volDir := filepath.Join(tmpDir, "vol1")
	require.NoError(t, os.MkdirAll(volDir, 0755))

	// Create a raw disk image
	diskPath := filepath.Join(volDir, "disk.img")
	f, err := os.Create(diskPath)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(64*1024))
	f.Close()

	// Set horizon so label "0001" is already applied
	require.NoError(t, writeLogHorizon(volDir, "0001"))

	blockSize := uint32(4096)
	data := bytes.Repeat([]byte{0xCD}, 4096)
	crc := crc32.ChecksumIEEE(data)

	seg2Data := buildSegmentData(t, blockSize, "0002", []lbd.Entry{
		{Op: "W", Block: 0, Length: 4096, Checksum: crc, Data: data},
	})

	cloud := &mockCloudDiskClient{
		segments: []LogSegmentInfo{
			{SegmentID: "seg-old", Label: "0001"},
			{SegmentID: "seg-new", Label: "0002"},
		},
		downloadData: map[string][]byte{"seg-new": seg2Data},
	}

	state := NewState()
	state.SetVolume("disk_volume/vol1", &VolumeState{
		EntityId: "disk_volume/vol1",
		VolumeId: "vol1",
		DiskPath: volDir,
	})

	mc := NewDiskMountController(slog.Default(), tmpDir, compute.NewNodeId("node-1"), state, newMockDiskMountOps())
	mc.SetCloudClient(cloud)

	volState := state.GetVolume("disk_volume/vol1")
	err = mc.replayMissingSegments(context.Background(), volState)
	require.NoError(t, err)

	// Verify horizon was updated to "0002"
	horizon, err := readLogHorizon(volDir)
	require.NoError(t, err)
	assert.Equal(t, "0002", horizon)

	// Verify data was written to disk
	result, err := os.ReadFile(diskPath)
	require.NoError(t, err)
	assert.Equal(t, data, result[0:4096])
}

func TestReplayMissingSegmentsNoRemoteSegments(t *testing.T) {
	tmpDir := t.TempDir()
	volDir := filepath.Join(tmpDir, "vol1")
	require.NoError(t, os.MkdirAll(volDir, 0755))

	cloud := &mockCloudDiskClient{
		segments: nil,
	}

	state := NewState()
	mc := NewDiskMountController(slog.Default(), tmpDir, compute.NewNodeId("node-1"), state, newMockDiskMountOps())
	mc.SetCloudClient(cloud)

	err := mc.replayMissingSegments(context.Background(), &VolumeState{
		VolumeId: "vol1",
		DiskPath: volDir,
	})
	require.NoError(t, err)
}

func TestReplayMissingSegmentsAllAlreadyApplied(t *testing.T) {
	tmpDir := t.TempDir()
	volDir := filepath.Join(tmpDir, "vol1")
	require.NoError(t, os.MkdirAll(volDir, 0755))

	// Set horizon past all remote segments
	require.NoError(t, writeLogHorizon(volDir, "0005"))

	cloud := &mockCloudDiskClient{
		segments: []LogSegmentInfo{
			{SegmentID: "seg-1", Label: "0001"},
			{SegmentID: "seg-2", Label: "0003"},
		},
	}

	state := NewState()
	mc := NewDiskMountController(slog.Default(), tmpDir, compute.NewNodeId("node-1"), state, newMockDiskMountOps())
	mc.SetCloudClient(cloud)

	err := mc.replayMissingSegments(context.Background(), &VolumeState{
		VolumeId: "vol1",
		DiskPath: volDir,
	})
	require.NoError(t, err)

	// Horizon should remain unchanged
	horizon, err := readLogHorizon(volDir)
	require.NoError(t, err)
	assert.Equal(t, "0005", horizon)
}

func TestReplayMissingSegmentsMultiple(t *testing.T) {
	tmpDir := t.TempDir()
	volDir := filepath.Join(tmpDir, "vol1")
	require.NoError(t, os.MkdirAll(volDir, 0755))

	diskPath := filepath.Join(volDir, "disk.img")
	f, err := os.Create(diskPath)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(64*1024))
	f.Close()

	blockSize := uint32(4096)
	dataA := bytes.Repeat([]byte{0xAA}, 4096)
	dataB := bytes.Repeat([]byte{0xBB}, 4096)
	crcA := crc32.ChecksumIEEE(dataA)
	crcB := crc32.ChecksumIEEE(dataB)

	seg1Data := buildSegmentData(t, blockSize, "0001", []lbd.Entry{
		{Op: "W", Block: 0, Length: 4096, Checksum: crcA, Data: dataA},
	})
	seg2Data := buildSegmentData(t, blockSize, "0002", []lbd.Entry{
		{Op: "W", Block: 1, Length: 4096, Checksum: crcB, Data: dataB},
	})

	cloud := &mockCloudDiskClient{
		segments: []LogSegmentInfo{
			{SegmentID: "seg-1", Label: "0001"},
			{SegmentID: "seg-2", Label: "0002"},
		},
		downloadData: map[string][]byte{
			"seg-1": seg1Data,
			"seg-2": seg2Data,
		},
	}

	state := NewState()
	mc := NewDiskMountController(slog.Default(), tmpDir, compute.NewNodeId("node-1"), state, newMockDiskMountOps())
	mc.SetCloudClient(cloud)

	err = mc.replayMissingSegments(context.Background(), &VolumeState{
		VolumeId: "vol1",
		DiskPath: volDir,
	})
	require.NoError(t, err)

	result, err := os.ReadFile(diskPath)
	require.NoError(t, err)
	assert.Equal(t, dataA, result[0:4096], "block 0 from seg-1")
	assert.Equal(t, dataB, result[4096:8192], "block 1 from seg-2")

	horizon, err := readLogHorizon(volDir)
	require.NoError(t, err)
	assert.Equal(t, "0002", horizon)
}

func TestReplayMissingSegmentsDownloadError(t *testing.T) {
	tmpDir := t.TempDir()
	volDir := filepath.Join(tmpDir, "vol1")
	require.NoError(t, os.MkdirAll(volDir, 0755))

	diskPath := filepath.Join(volDir, "disk.img")
	f, err := os.Create(diskPath)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(64*1024))
	f.Close()

	cloud := &mockCloudDiskClient{
		segments: []LogSegmentInfo{
			{SegmentID: "seg-1", Label: "0001"},
		},
		downloadErr: fmt.Errorf("network error"),
	}

	state := NewState()
	mc := NewDiskMountController(slog.Default(), tmpDir, compute.NewNodeId("node-1"), state, newMockDiskMountOps())
	mc.SetCloudClient(cloud)

	err = mc.replayMissingSegments(context.Background(), &VolumeState{
		VolumeId: "vol1",
		DiskPath: volDir,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "network error")
}

func TestLogHorizonReadWrite(t *testing.T) {
	tmpDir := t.TempDir()

	// Reading non-existent horizon returns empty string
	h, err := readLogHorizon(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, "", h)

	// Write and read back
	require.NoError(t, writeLogHorizon(tmpDir, "400002b3a1c5f2b400000000"))
	h, err = readLogHorizon(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, "400002b3a1c5f2b400000000", h)

	// Overwrite with newer label
	require.NoError(t, writeLogHorizon(tmpDir, "400002b3a1c5f2b400000001"))
	h, err = readLogHorizon(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, "400002b3a1c5f2b400000001", h)
}

func TestUpdateLogHorizonFromPath(t *testing.T) {
	tmpDir := t.TempDir()

	// First log sets the horizon
	err := updateLogHorizonFromPath(tmpDir, "/var/lib/miren/logs/disk.400002b3a1c5f2b400000001.log")
	require.NoError(t, err)
	h, _ := readLogHorizon(tmpDir)
	assert.Equal(t, "400002b3a1c5f2b400000001", h)

	// Newer log updates the horizon
	err = updateLogHorizonFromPath(tmpDir, "/var/lib/miren/logs/disk.400002b3a1c5f2b400000005.log")
	require.NoError(t, err)
	h, _ = readLogHorizon(tmpDir)
	assert.Equal(t, "400002b3a1c5f2b400000005", h)

	// Older log does not regress the horizon
	err = updateLogHorizonFromPath(tmpDir, "/var/lib/miren/logs/disk.400002b3a1c5f2b400000003.log")
	require.NoError(t, err)
	h, _ = readLogHorizon(tmpDir)
	assert.Equal(t, "400002b3a1c5f2b400000005", h)
}

func TestUpdateLogHorizonFromPathNonLogFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Non-log file should be a no-op (LabelFromLogPath returns empty for bad names)
	err := updateLogHorizonFromPath(tmpDir, "/some/path/metadata.json")
	require.NoError(t, err)
	h, _ := readLogHorizon(tmpDir)
	assert.Equal(t, "", h)
}

func TestOpenDiskImageRaw(t *testing.T) {
	tmpDir := t.TempDir()
	diskPath := filepath.Join(tmpDir, "disk.img")

	// Create a raw disk image (first 8 bytes are zeros, not qcow2 magic)
	f, err := os.Create(diskPath)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(64*1024))
	f.Close()

	img, err := openDiskImage(diskPath)
	require.NoError(t, err)
	defer img.Close()

	// Verify it's a rawDiskImage
	_, ok := img.(*rawDiskImage)
	assert.True(t, ok, "should be a rawDiskImage")

	// Write and verify
	data := []byte("hello world")
	_, err = img.WriteAt(data, 0)
	require.NoError(t, err)
	require.NoError(t, img.Sync())
	img.Close()

	result, err := os.ReadFile(diskPath)
	require.NoError(t, err)
	assert.Equal(t, data, result[:len(data)])
}

func TestOpenDiskImageQCow2(t *testing.T) {
	tmpDir := t.TempDir()
	diskPath := filepath.Join(tmpDir, "disk.img")

	// Create a qcow2 image
	qimg, err := lbd.CreateQCow2(diskPath, 1024*1024, 16) // 64KB clusters
	require.NoError(t, err)
	require.NoError(t, qimg.Close())

	img, err := openDiskImage(diskPath)
	require.NoError(t, err)
	defer img.Close()

	// Verify it's a qcow2DiskImage
	_, ok := img.(*qcow2DiskImage)
	assert.True(t, ok, "should be a qcow2DiskImage")

	// Write and verify
	data := bytes.Repeat([]byte{0xFE}, 4096)
	_, err = img.WriteAt(data, 0)
	require.NoError(t, err)
	require.NoError(t, img.Sync())

	readBuf := make([]byte, 4096)
	_, err = img.(*qcow2DiskImage).img.ReadAt(readBuf, 0)
	require.NoError(t, err)
	assert.Equal(t, data, readBuf)
}

func TestReplayToQCow2Image(t *testing.T) {
	tmpDir := t.TempDir()
	volDir := filepath.Join(tmpDir, "vol1")
	require.NoError(t, os.MkdirAll(volDir, 0755))

	diskPath := filepath.Join(volDir, "disk.img")
	qimg, err := lbd.CreateQCow2(diskPath, 1024*1024, 16)
	require.NoError(t, err)
	require.NoError(t, qimg.Close())

	blockSize := uint32(4096)
	writeData := bytes.Repeat([]byte{0xDE}, 4096)
	crc := crc32.ChecksumIEEE(writeData)

	segData := buildSegmentData(t, blockSize, "0001", []lbd.Entry{
		{Op: "W", Block: 0, Length: 4096, Checksum: crc, Data: writeData},
		{Op: "W", Block: 3, Length: 4096, Checksum: crc, Data: writeData},
	})

	cloud := &mockCloudDiskClient{
		segments: []LogSegmentInfo{
			{SegmentID: "seg-1", Label: "0001"},
		},
		downloadData: map[string][]byte{"seg-1": segData},
	}

	state := NewState()
	mc := NewDiskMountController(slog.Default(), tmpDir, compute.NewNodeId("node-1"), state, newMockDiskMountOps())
	mc.SetCloudClient(cloud)

	err = mc.replayMissingSegments(context.Background(), &VolumeState{
		VolumeId: "vol1",
		DiskPath: volDir,
	})
	require.NoError(t, err)

	// Verify data was written to the qcow2 image
	verifyImg, err := lbd.OpenQCow2(diskPath)
	require.NoError(t, err)
	defer verifyImg.Close()

	buf := make([]byte, 4096)
	_, err = verifyImg.ReadAt(buf, 0)
	require.NoError(t, err)
	assert.Equal(t, writeData, buf, "block 0")

	_, err = verifyImg.ReadAt(buf, 3*4096)
	require.NoError(t, err)
	assert.Equal(t, writeData, buf, "block 3")

	// Block 1 should be zeros (unwritten)
	_, err = verifyImg.ReadAt(buf, 4096)
	require.NoError(t, err)
	assert.Equal(t, make([]byte, 4096), buf, "block 1 should be zeros")
}

func TestLeaseAcquiredBeforeReplay(t *testing.T) {
	cloud := &mockCloudDiskClient{
		acquireLeaseNonce: "test-nonce-123",
		segments:          nil, // no segments to replay
	}

	tmpDir := t.TempDir()
	state := NewState()
	state.SetVolume("disk_volume/vol1", &VolumeState{
		EntityId: "disk_volume/vol1",
		VolumeId: "vol1",
		DiskPath: filepath.Join(tmpDir, "vol1"),
		Mode:     storage_v1alpha.VM_ACCELERATOR,
	})

	ops := newMockDiskMountOps()
	mc := NewDiskMountController(slog.Default(), tmpDir, compute.NewNodeId("node-1"), state, ops)
	mc.SetCloudClient(cloud)

	// The lease is acquired in attachAndMount, which requires an EAC.
	// Test the cloud client mock directly instead.
	nonce, err := cloud.AcquireLease(context.Background(), "vol1")
	require.NoError(t, err)
	assert.Equal(t, "test-nonce-123", nonce)
	assert.Equal(t, []string{"vol1"}, cloud.acquireLeaseCalls)
}

func TestLeaseReleasedOnUnmount(t *testing.T) {
	cloud := &mockCloudDiskClient{}

	tmpDir := t.TempDir()
	state := NewState()
	state.SetMount("disk_mount/mnt-1", &MountState{
		EntityId:   "disk_mount/mnt-1",
		VolumeId:   "disk_volume/vol1",
		LeaseNonce: "nonce-abc",
	})

	mc := NewDiskMountController(slog.Default(), tmpDir, compute.NewNodeId("node-1"), state, newMockDiskMountOps())
	mc.SetCloudClient(cloud)

	// Shutdown should release leases
	mc.Shutdown()

	require.Len(t, cloud.releaseLeaseCalls, 1)
	assert.Equal(t, "disk_volume/vol1", cloud.releaseLeaseCalls[0].volumeID)
	assert.Equal(t, "nonce-abc", cloud.releaseLeaseCalls[0].nonce)
}

func TestLeaseNotReleasedWithoutNonce(t *testing.T) {
	cloud := &mockCloudDiskClient{}

	tmpDir := t.TempDir()
	state := NewState()
	state.SetMount("disk_mount/mnt-1", &MountState{
		EntityId:   "disk_mount/mnt-1",
		VolumeId:   "disk_volume/vol1",
		LeaseNonce: "", // no lease
	})

	mc := NewDiskMountController(slog.Default(), tmpDir, compute.NewNodeId("node-1"), state, newMockDiskMountOps())
	mc.SetCloudClient(cloud)

	mc.Shutdown()

	assert.Empty(t, cloud.releaseLeaseCalls)
}

func TestLeaseNotReleasedWithoutCloudClient(t *testing.T) {
	tmpDir := t.TempDir()
	state := NewState()
	state.SetMount("disk_mount/mnt-1", &MountState{
		EntityId:   "disk_mount/mnt-1",
		VolumeId:   "disk_volume/vol1",
		LeaseNonce: "some-nonce",
	})

	mc := NewDiskMountController(slog.Default(), tmpDir, compute.NewNodeId("node-1"), state, newMockDiskMountOps())
	// No cloud client set

	// Should not panic
	mc.Shutdown()
}

func TestReplayOverwriteAtSameBlock(t *testing.T) {
	tmpDir := t.TempDir()
	volDir := filepath.Join(tmpDir, "vol1")
	require.NoError(t, os.MkdirAll(volDir, 0755))

	diskPath := filepath.Join(volDir, "disk.img")
	f, err := os.Create(diskPath)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(64*1024))
	f.Close()

	blockSize := uint32(4096)
	dataOld := bytes.Repeat([]byte{0xAA}, 4096)
	dataNew := bytes.Repeat([]byte{0xBB}, 4096)
	crcOld := crc32.ChecksumIEEE(dataOld)
	crcNew := crc32.ChecksumIEEE(dataNew)

	// First segment writes 0xAA to block 0
	seg1 := buildSegmentData(t, blockSize, "0001", []lbd.Entry{
		{Op: "W", Block: 0, Length: 4096, Checksum: crcOld, Data: dataOld},
	})
	// Second segment overwrites block 0 with 0xBB
	seg2 := buildSegmentData(t, blockSize, "0002", []lbd.Entry{
		{Op: "W", Block: 0, Length: 4096, Checksum: crcNew, Data: dataNew},
	})

	cloud := &mockCloudDiskClient{
		segments: []LogSegmentInfo{
			{SegmentID: "seg-1", Label: "0001"},
			{SegmentID: "seg-2", Label: "0002"},
		},
		downloadData: map[string][]byte{
			"seg-1": seg1,
			"seg-2": seg2,
		},
	}

	state := NewState()
	mc := NewDiskMountController(slog.Default(), tmpDir, compute.NewNodeId("node-1"), state, newMockDiskMountOps())
	mc.SetCloudClient(cloud)

	err = mc.replayMissingSegments(context.Background(), &VolumeState{
		VolumeId: "vol1",
		DiskPath: volDir,
	})
	require.NoError(t, err)

	result, err := os.ReadFile(diskPath)
	require.NoError(t, err)
	assert.Equal(t, dataNew, result[0:4096], "block 0 should have the latest write")
}
