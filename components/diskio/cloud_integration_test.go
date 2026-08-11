package diskio

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/lbd"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/cloudauth"
)

// storedSegment holds a segment's data and metadata in the mock server.
type storedSegment struct {
	VolumeID string
	Data     []byte
	// Label is the update's ordering key; for lbd_log that is the TAI64N label
	Label    string
	Kind     string
	Complete bool
}

// activeLease tracks a lease held on a volume.
type activeLease struct {
	Nonce string
}

// mockCloudServer is an httptest-backed mock of the miren.cloud API that stores
// segments, leases and auth state in memory so the real HTTP clients can be
// exercised end-to-end.
type mockCloudServer struct {
	t       *testing.T
	server  *httptest.Server
	mu      sync.Mutex
	counter int

	segments map[string]*storedSegment // segmentID → data+metadata
	leases   map[string]*activeLease   // volumeID → active lease
}

func newMockCloudServer(t *testing.T) *mockCloudServer {
	t.Helper()
	m := &mockCloudServer{
		t:        t,
		segments: make(map[string]*storedSegment),
		leases:   make(map[string]*activeLease),
	}
	m.server = httptest.NewServer(m)
	t.Cleanup(m.server.Close)
	return m
}

func (m *mockCloudServer) URL() string { return m.server.URL }

func (m *mockCloudServer) nextID(prefix string) string {
	m.counter++
	return fmt.Sprintf("%s-%d", prefix, m.counter)
}

func (m *mockCloudServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Auth endpoints
	if strings.HasPrefix(path, "/auth/service-account/") {
		m.handleAuth(w, r)
		return
	}

	switch {
	// Lease endpoints
	case r.Method == "POST" && strings.Contains(path, "/disk/volumes/") && strings.HasSuffix(path, "/lease"):
		m.handleAcquireLease(w, r)
	case r.Method == "DELETE" && strings.Contains(path, "/disk/volumes/") && strings.HasSuffix(path, "/lease"):
		m.handleReleaseLease(w, r)

	// Update upload flow
	case r.Method == "POST" && strings.HasSuffix(path, "/updates/upload"):
		m.handleUploadRequest(w, r)
	case r.Method == "PUT" && strings.HasPrefix(path, "/upload/"):
		m.handleUploadData(w, r)
	case r.Method == "POST" && strings.Contains(path, "/updates/") && strings.HasSuffix(path, "/complete"):
		m.handleComplete(w, r)

	// Update list/download
	case r.Method == "GET" && strings.HasSuffix(path, "/updates"):
		m.handleListSegments(w, r)
	case r.Method == "GET" && strings.Contains(path, "/updates/") && strings.HasSuffix(path, "/download"):
		m.handleDownloadRequest(w, r)

	// Raw data download
	case r.Method == "GET" && strings.HasPrefix(path, "/data/"):
		m.handleDataDownload(w, r)

	default:
		http.NotFound(w, r)
	}
}

func (m *mockCloudServer) handleAuth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if strings.HasSuffix(r.URL.Path, "/begin") {
		json.NewEncoder(w).Encode(cloudauth.BeginAuthResponse{
			Envelope:  "test-envelope",
			Challenge: "test-challenge",
		})
		return
	}
	if strings.HasSuffix(r.URL.Path, "/complete") {
		json.NewEncoder(w).Encode(cloudauth.CompleteAuthResponse{
			Token:     "test-jwt-token",
			ExpiresIn: 3600,
		})
		return
	}
}

func (m *mockCloudServer) handleAcquireLease(w http.ResponseWriter, r *http.Request) {
	// Extract volume ID from path: /api/v1/disk/volumes/{id}/lease
	parts := strings.Split(r.URL.Path, "/")
	var volumeID string
	for i, p := range parts {
		if p == "volumes" && i+1 < len(parts) {
			volumeID = parts[i+1]
			break
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, held := m.leases[volumeID]; held {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"volume already has an active lease"}`))
		return
	}

	nonce := m.nextID("nonce")
	m.leases[volumeID] = &activeLease{Nonce: nonce}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(leaseResponse{
		LeaseID:  m.nextID("lease"),
		VolumeID: volumeID,
		Nonce:    nonce,
	})
}

func (m *mockCloudServer) handleReleaseLease(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	var volumeID string
	for i, p := range parts {
		if p == "volumes" && i+1 < len(parts) {
			volumeID = parts[i+1]
			break
		}
	}

	nonce := r.Header.Get("X-Lease-Nonce")

	m.mu.Lock()
	defer m.mu.Unlock()

	lease, held := m.leases[volumeID]
	if !held {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if lease.Nonce != nonce {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"nonce mismatch"}`))
		return
	}

	delete(m.leases, volumeID)
	w.WriteHeader(http.StatusOK)
}

// volumeIDFromUpdatesPath pulls the volume out of
// /api/v1/disk/volumes/{volumeId}/updates/...
func volumeIDFromUpdatesPath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if p == "volumes" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func (m *mockCloudServer) handleUploadRequest(w http.ResponseWriter, r *http.Request) {
	var req beginUploadRequestJSON
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// The real server derives the ordering key from the request, not the
	// payload, and refuses an update without one.
	if req.OrderingKey == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"ordering_key is required"}`)
		return
	}

	volumeID := volumeIDFromUpdatesPath(r.URL.Path)

	m.mu.Lock()
	segID := m.nextID("seg")
	m.segments[segID] = &storedSegment{
		VolumeID: volumeID,
		Label:    req.OrderingKey,
		Kind:     req.Kind,
	}
	m.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(beginUploadResponseJSON{
		UpdateID:    segID,
		UploadURL:   m.server.URL + "/upload/" + segID,
		CompleteURL: m.server.URL + "/api/v1/disk/volumes/" + volumeID + "/updates/" + segID + "/complete",
	})
}

func (m *mockCloudServer) handleUploadData(w http.ResponseWriter, r *http.Request) {
	segID := strings.TrimPrefix(r.URL.Path, "/upload/")
	data, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	seg, ok := m.segments[segID]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	seg.Data = data
	w.WriteHeader(http.StatusOK)
}

func (m *mockCloudServer) handleComplete(w http.ResponseWriter, r *http.Request) {
	// Extract update ID: /api/v1/disk/volumes/{volumeId}/updates/{id}/complete
	parts := strings.Split(r.URL.Path, "/")
	var segID string
	for i, p := range parts {
		if p == "updates" && i+1 < len(parts) {
			segID = parts[i+1]
			break
		}
	}

	var req completeUploadRequestJSON
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	seg, ok := m.segments[segID]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Validate MD5
	md5h := md5.Sum(seg.Data)
	expectedMD5 := base64.StdEncoding.EncodeToString(md5h[:])
	if req.MD5 != expectedMD5 {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":"md5 mismatch: got %s, want %s"}`, req.MD5, expectedMD5)
		return
	}

	// Validate CRC32C
	crch := crc32.New(crc32.MakeTable(crc32.Castagnoli))
	crch.Write(seg.Data)
	expectedCRC := base64.StdEncoding.EncodeToString(crch.Sum(nil))
	if req.CRC32C != expectedCRC {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":"crc32c mismatch: got %s, want %s"}`, req.CRC32C, expectedCRC)
		return
	}

	seg.Complete = true
	w.WriteHeader(http.StatusOK)
}

func (m *mockCloudServer) handleListSegments(w http.ResponseWriter, r *http.Request) {
	volumeID := volumeIDFromUpdatesPath(r.URL.Path)
	kind := r.URL.Query().Get("kind")
	// The real server filters on `after` -- the caller's replay horizon -- and
	// returns only what sorts past it.
	after := r.URL.Query().Get("after")

	m.mu.Lock()
	defer m.mu.Unlock()

	segs := []UpdateInfo{}
	for id, seg := range m.segments {
		if !seg.Complete || seg.VolumeID != volumeID || seg.Label <= after {
			continue
		}
		if kind != "" && seg.Kind != kind {
			continue
		}
		segs = append(segs, UpdateInfo{
			UpdateID:    id,
			Kind:        seg.Kind,
			OrderingKey: seg.Label,
			Size:        int64(len(seg.Data)),
		})
	}

	sort.Slice(segs, func(i, j int) bool { return segs[i].OrderingKey < segs[j].OrderingKey })

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(listUpdatesResponseJSON{Updates: segs})
}

func (m *mockCloudServer) handleDownloadRequest(w http.ResponseWriter, r *http.Request) {
	// Extract update ID: /api/v1/disk/volumes/{volumeId}/updates/{id}/download
	parts := strings.Split(r.URL.Path, "/")
	var segID string
	for i, p := range parts {
		if p == "updates" && i+1 < len(parts) {
			segID = parts[i+1]
			break
		}
	}

	m.mu.Lock()
	seg, ok := m.segments[segID]
	m.mu.Unlock()

	if !ok || !seg.Complete {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(downloadResponseJSON{
		DownloadURL: m.server.URL + "/data/" + segID,
	})
}

func (m *mockCloudServer) handleDataDownload(w http.ResponseWriter, r *http.Request) {
	segID := strings.TrimPrefix(r.URL.Path, "/data/")

	m.mu.Lock()
	seg, ok := m.segments[segID]
	m.mu.Unlock()

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(seg.Data)
}

// completedSegments returns all completed segments for the given volume.
func (m *mockCloudServer) completedSegments(volumeID string) []*storedSegment {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*storedSegment
	for _, seg := range m.segments {
		if seg.Complete && seg.VolumeID == volumeID {
			result = append(result, seg)
		}
	}
	return result
}

// --- test helpers ---

func newTestAuthClient(t *testing.T, serverURL string) *cloudauth.AuthClient {
	t.Helper()
	kp, err := cloudauth.GenerateKeyPair()
	require.NoError(t, err)
	ac, err := cloudauth.NewAuthClient(serverURL, kp)
	require.NoError(t, err)
	return ac
}

// buildSegmentFile creates a .log file on disk using lbd.Writer and returns its path.
func buildSegmentFile(t *testing.T, dir, label string, blockSize uint32, entries []lbd.Entry) string {
	t.Helper()
	path := filepath.Join(dir, fmt.Sprintf("disk.%s.log", label))

	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()

	w, err := lbd.NewWriter(f, lbd.Header{
		Version:      2,
		BlockSize:    blockSize,
		SegmentLabel: label,
		DeviceSize:   1024 * 1024,
	})
	require.NoError(t, err)

	for i := range entries {
		require.NoError(t, w.WriteEntry(&entries[i]))
	}
	return path
}

// makeWriteEntry creates an lbd.Entry for a write operation with correct CRC.
func makeWriteEntry(block uint64, data []byte) lbd.Entry {
	return lbd.Entry{
		Op:       "W",
		Block:    block,
		Length:   uint32(len(data)),
		Checksum: crc32.ChecksumIEEE(data),
		Data:     data,
	}
}

// --- integration tests ---

func TestCloudIntegrationFullRoundTrip(t *testing.T) {
	mock := newMockCloudServer(t)
	authClient := newTestAuthClient(t, mock.URL())

	volumeID := "vol-roundtrip"
	blockSize := uint32(4096)
	tmpDir := t.TempDir()

	// Create 3 segment files with distinct data
	dataA := bytes.Repeat([]byte{0xAA}, 4096)
	dataB := bytes.Repeat([]byte{0xBB}, 4096)
	dataC := bytes.Repeat([]byte{0xCC}, 4096)

	labels := []string{
		"400000000000000100000001",
		"400000000000000200000002",
		"400000000000000300000003",
	}

	segDir := filepath.Join(tmpDir, "segments")
	require.NoError(t, os.MkdirAll(segDir, 0755))

	seg1 := buildSegmentFile(t, segDir, labels[0], blockSize, []lbd.Entry{
		makeWriteEntry(0, dataA),
	})
	seg2 := buildSegmentFile(t, segDir, labels[1], blockSize, []lbd.Entry{
		makeWriteEntry(1, dataB),
	})
	seg3 := buildSegmentFile(t, segDir, labels[2], blockSize, []lbd.Entry{
		makeWriteEntry(2, dataC),
	})

	// Upload all 3 segments via the real CloudSegmentUploader
	uploader := NewCloudSegmentUploader(slog.Default(), mock.URL(), authClient, nil)

	for _, path := range []string{seg1, seg2, seg3} {
		segID, err := uploader.UploadSegment(context.Background(), volumeID, path)
		require.NoError(t, err)
		assert.NotEmpty(t, segID)
	}

	// Verify mock stored 3 completed segments
	completed := mock.completedSegments(volumeID)
	assert.Len(t, completed, 3, "should have 3 completed segments in mock")

	// Verify labels survived round-trip
	foundLabels := make(map[string]bool)
	for _, seg := range completed {
		foundLabels[seg.Label] = true
	}
	for _, label := range labels {
		assert.True(t, foundLabels[label], "label %s should be present in stored segments", label)
	}

	// Create a fresh disk image for replay
	volDir := filepath.Join(tmpDir, "replay-vol")
	require.NoError(t, os.MkdirAll(volDir, 0755))
	diskPath := filepath.Join(volDir, "disk.img")
	f, err := os.Create(diskPath)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(1024*1024))
	f.Close()

	// Replay using the real cloudDiskClient → mock server
	cloudClient := NewCloudDiskClient(slog.Default(), mock.URL(), authClient)
	state := NewState()
	mc := NewDiskMountController(slog.Default(), tmpDir, compute.NewNodeId("node-1"), state, newMockDiskMountOps())
	mc.SetCloudClient(cloudClient)

	err = mc.replayMissingSegments(context.Background(), &VolumeState{
		VolumeId: volumeID,
		DiskPath: volDir,
	})
	require.NoError(t, err)

	// Read back disk and verify data at expected offsets
	result, err := os.ReadFile(diskPath)
	require.NoError(t, err)

	assert.Equal(t, dataA, result[0:4096], "block 0 should have segment 1 data")
	assert.Equal(t, dataB, result[4096:8192], "block 1 should have segment 2 data")
	assert.Equal(t, dataC, result[8192:12288], "block 2 should have segment 3 data")

	// Verify horizon was updated to the last label
	horizon, err := readLogHorizon(volDir)
	require.NoError(t, err)
	assert.Equal(t, labels[2], horizon)
}

func TestCloudIntegrationLogWatcherUpload(t *testing.T) {
	mock := newMockCloudServer(t)
	authClient := newTestAuthClient(t, mock.URL())

	volumeID := "vol-watcher"
	blockSize := uint32(4096)
	tmpDir := t.TempDir()

	// Create volume dir with logs subdir
	volDir := filepath.Join(tmpDir, "vol-watcher")
	logDir := filepath.Join(volDir, "logs")
	require.NoError(t, os.MkdirAll(logDir, 0755))

	data := bytes.Repeat([]byte{0xDD}, 4096)
	labels := []string{
		"400000000000000100000001",
		"400000000000000200000002",
	}

	// Create completed .log files in the volume's log dir
	for _, label := range labels {
		buildSegmentFile(t, logDir, label, blockSize, []lbd.Entry{
			makeWriteEntry(0, data),
		})
	}

	// Create an in-progress .log.tmp file (should be skipped)
	tmpFile := filepath.Join(logDir, "disk.400000000000000300000003.log.tmp")
	require.NoError(t, os.WriteFile(tmpFile, []byte("partial"), 0644))

	state := NewState()
	state.SetVolume("disk_volume/"+volumeID, &VolumeState{
		EntityId: "disk_volume/" + volumeID,
		VolumeId: volumeID,
		DiskPath: volDir,
		Mode:     storage_v1alpha.VM_ACCELERATOR,
	})

	uploader := NewCloudSegmentUploader(slog.Default(), mock.URL(), authClient, state)
	watcher := NewLogWatcher(slog.Default(), state, uploader, time.Second)

	// Run one scan cycle
	watcher.scanAndUpload(context.Background())

	// Verify mock received 2 completed segments
	completed := mock.completedSegments(volumeID)
	assert.Len(t, completed, 2, "should have 2 completed segments")

	// Verify .log files were removed
	for _, label := range labels {
		logFile := filepath.Join(logDir, fmt.Sprintf("disk.%s.log", label))
		_, err := os.Stat(logFile)
		assert.True(t, os.IsNotExist(err), "log file %s should have been removed", logFile)
	}

	// Verify .log.tmp file was not touched
	_, err := os.Stat(tmpFile)
	assert.NoError(t, err, ".log.tmp file should still exist")

	// Verify horizon was updated
	horizon, err := readLogHorizon(volDir)
	require.NoError(t, err)
	assert.NotEmpty(t, horizon)
}

func TestCloudIntegrationLeaseLifecycle(t *testing.T) {
	mock := newMockCloudServer(t)
	authClient := newTestAuthClient(t, mock.URL())

	volumeID := "vol-lease"
	client := NewCloudDiskClient(slog.Default(), mock.URL(), authClient)

	ctx := context.Background()

	// Acquire lease
	nonce1, err := client.AcquireLease(ctx, volumeID)
	require.NoError(t, err)
	assert.NotEmpty(t, nonce1)

	// Acquiring again should fail with 409
	_, err = client.AcquireLease(ctx, volumeID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already has an active lease")

	// Release lease
	err = client.ReleaseLease(ctx, volumeID, nonce1)
	require.NoError(t, err)

	// Acquire again after release should succeed with a new nonce
	nonce2, err := client.AcquireLease(ctx, volumeID)
	require.NoError(t, err)
	assert.NotEmpty(t, nonce2)
	assert.NotEqual(t, nonce1, nonce2, "new lease should have a different nonce")
}

func TestCloudIntegrationIncrementalReplay(t *testing.T) {
	mock := newMockCloudServer(t)
	authClient := newTestAuthClient(t, mock.URL())

	volumeID := "vol-incremental"
	blockSize := uint32(4096)
	tmpDir := t.TempDir()

	data1 := bytes.Repeat([]byte{0x11}, 4096)
	data2 := bytes.Repeat([]byte{0x22}, 4096)
	data3 := bytes.Repeat([]byte{0x33}, 4096)

	labels := []string{
		"400000000000000100000001",
		"400000000000000200000002",
		"400000000000000300000003",
	}

	// Upload 3 segments
	segDir := filepath.Join(tmpDir, "segments")
	require.NoError(t, os.MkdirAll(segDir, 0755))

	uploader := NewCloudSegmentUploader(slog.Default(), mock.URL(), authClient, nil)

	for i, entry := range []lbd.Entry{
		makeWriteEntry(0, data1),
		makeWriteEntry(1, data2),
		makeWriteEntry(2, data3),
	} {
		path := buildSegmentFile(t, segDir, labels[i], blockSize, []lbd.Entry{entry})
		_, err := uploader.UploadSegment(context.Background(), volumeID, path)
		require.NoError(t, err)
	}

	// Create disk image and set horizon to label[1] (segment 2 already applied)
	volDir := filepath.Join(tmpDir, "replay-vol")
	require.NoError(t, os.MkdirAll(volDir, 0755))
	diskPath := filepath.Join(volDir, "disk.img")
	f, err := os.Create(diskPath)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(1024*1024))
	f.Close()

	require.NoError(t, writeLogHorizon(volDir, labels[1]))

	// Replay
	cloudClient := NewCloudDiskClient(slog.Default(), mock.URL(), authClient)
	state := NewState()
	mc := NewDiskMountController(slog.Default(), tmpDir, compute.NewNodeId("node-1"), state, newMockDiskMountOps())
	mc.SetCloudClient(cloudClient)

	err = mc.replayMissingSegments(context.Background(), &VolumeState{
		VolumeId: volumeID,
		DiskPath: volDir,
	})
	require.NoError(t, err)

	// Only segment 3 should have been applied (block 2)
	result, err := os.ReadFile(diskPath)
	require.NoError(t, err)

	assert.Equal(t, make([]byte, 4096), result[0:4096], "block 0 should be zeros (segment 1 skipped)")
	assert.Equal(t, make([]byte, 4096), result[4096:8192], "block 1 should be zeros (segment 2 skipped)")
	assert.Equal(t, data3, result[8192:12288], "block 2 should have segment 3 data")

	// Horizon should have advanced to segment 3
	horizon, err := readLogHorizon(volDir)
	require.NoError(t, err)
	assert.Equal(t, labels[2], horizon)
}

func TestCloudIntegrationOverwriteSemantics(t *testing.T) {
	mock := newMockCloudServer(t)
	authClient := newTestAuthClient(t, mock.URL())

	volumeID := "vol-overwrite"
	blockSize := uint32(4096)
	tmpDir := t.TempDir()

	dataFirst := bytes.Repeat([]byte{0xAA}, 4096)
	dataSecond := bytes.Repeat([]byte{0xFF}, 4096)

	labels := []string{
		"400000000000000100000001",
		"400000000000000200000002",
	}

	// Upload 2 segments both writing to block 0
	segDir := filepath.Join(tmpDir, "segments")
	require.NoError(t, os.MkdirAll(segDir, 0755))

	uploader := NewCloudSegmentUploader(slog.Default(), mock.URL(), authClient, nil)

	path1 := buildSegmentFile(t, segDir, labels[0], blockSize, []lbd.Entry{
		makeWriteEntry(0, dataFirst),
	})
	_, err := uploader.UploadSegment(context.Background(), volumeID, path1)
	require.NoError(t, err)

	path2 := buildSegmentFile(t, segDir, labels[1], blockSize, []lbd.Entry{
		makeWriteEntry(0, dataSecond),
	})
	_, err = uploader.UploadSegment(context.Background(), volumeID, path2)
	require.NoError(t, err)

	// Create disk image and replay
	volDir := filepath.Join(tmpDir, "replay-vol")
	require.NoError(t, os.MkdirAll(volDir, 0755))
	diskPath := filepath.Join(volDir, "disk.img")
	f, err := os.Create(diskPath)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(1024*1024))
	f.Close()

	cloudClient := NewCloudDiskClient(slog.Default(), mock.URL(), authClient)
	state := NewState()
	mc := NewDiskMountController(slog.Default(), tmpDir, compute.NewNodeId("node-1"), state, newMockDiskMountOps())
	mc.SetCloudClient(cloudClient)

	err = mc.replayMissingSegments(context.Background(), &VolumeState{
		VolumeId: volumeID,
		DiskPath: volDir,
	})
	require.NoError(t, err)

	// Block 0 should have the second segment's data (last write wins)
	result, err := os.ReadFile(diskPath)
	require.NoError(t, err)
	assert.Equal(t, dataSecond, result[0:4096], "block 0 should have the second segment's data")

	// Horizon should be at label 2
	horizon, err := readLogHorizon(volDir)
	require.NoError(t, err)
	assert.Equal(t, labels[1], horizon)
}
