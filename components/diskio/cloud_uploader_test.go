package diskio

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/lbd"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/cloudauth"
)

// testAPIHandler wraps a mutable handler that also serves auth endpoints.
type testAPIHandler struct {
	handler http.HandlerFunc
}

func (h *testAPIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/auth/service-account/") {
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
	if h.handler != nil {
		h.handler(w, r)
	}
}

func newTestUploaderServer(t *testing.T) (*httptest.Server, *testAPIHandler, *cloudauth.AuthClient) {
	t.Helper()
	h := &testAPIHandler{}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	kp, err := cloudauth.GenerateKeyPair()
	require.NoError(t, err)
	authClient, err := cloudauth.NewAuthClient(ts.URL, kp)
	require.NoError(t, err)

	return ts, h, authClient
}

// fakeUpdatesClient records what the uploader asked for. The transport itself
// is covered in cloud_updates_client_test.go; these tests are about what the
// uploader decides to send.
type fakeUpdatesClient struct {
	uploads   []fakeUpload
	uploadErr error
	nextID    string
}

type fakeUpload struct {
	VolumeID string
	Request  UploadRequest
	Body     []byte
}

func (f *fakeUpdatesClient) Upload(ctx context.Context, volumeID string, req UploadRequest, body io.Reader, size int64) (string, error) {
	if f.uploadErr != nil {
		return "", f.uploadErr
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	if int64(len(data)) != size {
		return "", fmt.Errorf("declared size %d but body is %d bytes", size, len(data))
	}
	f.uploads = append(f.uploads, fakeUpload{VolumeID: volumeID, Request: req, Body: data})
	if f.nextID != "" {
		return f.nextID, nil
	}
	return "volup-fake", nil
}

func (f *fakeUpdatesClient) List(ctx context.Context, volumeID string, opts ListOptions) ([]UpdateInfo, error) {
	return nil, nil
}

func (f *fakeUpdatesClient) Download(ctx context.Context, volumeID, updateID string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("not implemented")
}

func writeTestSegment(t *testing.T, label string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "disk."+label+".log")
	require.NoError(t, os.WriteFile(path, content, 0644))
	return path
}

func TestCloudSegmentUploaderUploadsAsLBDLog(t *testing.T) {
	const label = "400000000000000100000001"
	fake := &fakeUpdatesClient{nextID: "volup-abc"}
	uploader := NewCloudSegmentUploaderWithClient(slog.Default(), fake, nil)

	content := []byte("lbd log segment bytes")
	segID, err := uploader.UploadSegment(context.Background(), "vol-123", writeTestSegment(t, label, content))
	require.NoError(t, err)
	assert.Equal(t, "volup-abc", segID)

	require.Len(t, fake.uploads, 1)
	up := fake.uploads[0]
	assert.Equal(t, "vol-123", up.VolumeID)
	assert.Equal(t, KindLBDLog, up.Request.Kind)
	// The TAI64N label is the ordering key replay sorts on
	assert.Equal(t, label, up.Request.OrderingKey)
	assert.Equal(t, content, up.Body)
}

// A segment whose filename carries no TAI64N label has no ordering key, so it
// is rejected locally rather than as a 400 from the cloud.
func TestCloudSegmentUploaderRejectsUnlabelledSegment(t *testing.T) {
	fake := &fakeUpdatesClient{}
	uploader := NewCloudSegmentUploaderWithClient(slog.Default(), fake, nil)

	path := filepath.Join(t.TempDir(), "not-a-segment.log")
	require.NoError(t, os.WriteFile(path, []byte("data"), 0644))

	_, err := uploader.UploadSegment(context.Background(), "vol-123", path)
	require.ErrorContains(t, err, "no usable TAI64N label")
	assert.Empty(t, fake.uploads, "nothing should be sent for an unlabelled segment")
}

func TestCloudSegmentUploaderIncludesLeaseNonce(t *testing.T) {
	fake := &fakeUpdatesClient{}
	state := NewState()
	state.SetPath(t.TempDir())
	// The local entity id and the cloud id are deliberately different: the
	// uploader is handed the cloud id, but mount state keys off the local
	// entity id, so a direct comparison would omit the nonce (MIR-1634).
	state.SetVolume("disk_volume/abc", &VolumeState{
		EntityId:      "disk_volume/abc",
		VolumeId:      "abc",
		CloudVolumeId: "vol-cloud-xyz",
	})
	state.SetMount("mount-1", &MountState{
		EntityId:   "mount-1",
		VolumeId:   "disk_volume/abc",
		Mounted:    true,
		LeaseNonce: "nonce-abc",
	})

	uploader := NewCloudSegmentUploaderWithClient(slog.Default(), fake, state)
	_, err := uploader.UploadSegment(context.Background(), "vol-cloud-xyz",
		writeTestSegment(t, "400000000000000100000001", []byte("data")))
	require.NoError(t, err)

	require.Len(t, fake.uploads, 1)
	assert.Equal(t, "nonce-abc", fake.uploads[0].Request.LeaseNonce)
}

func TestCloudSegmentUploaderNoLeaseNonceWithoutState(t *testing.T) {
	fake := &fakeUpdatesClient{}
	uploader := NewCloudSegmentUploaderWithClient(slog.Default(), fake, nil)

	_, err := uploader.UploadSegment(context.Background(), "vol-123",
		writeTestSegment(t, "400000000000000100000001", []byte("data")))
	require.NoError(t, err)

	require.Len(t, fake.uploads, 1)
	assert.Empty(t, fake.uploads[0].Request.LeaseNonce)
}

func TestCloudSegmentUploaderSkipsEmptyFile(t *testing.T) {
	fake := &fakeUpdatesClient{}
	uploader := NewCloudSegmentUploaderWithClient(slog.Default(), fake, nil)

	segID, err := uploader.UploadSegment(context.Background(), "vol-123",
		writeTestSegment(t, "400000000000000100000001", nil))
	require.NoError(t, err)
	assert.Empty(t, segID)
	assert.Empty(t, fake.uploads, "an empty segment is not worth uploading")
}

func TestCloudSegmentUploaderFileNotFound(t *testing.T) {
	fake := &fakeUpdatesClient{}
	uploader := NewCloudSegmentUploaderWithClient(slog.Default(), fake, nil)

	_, err := uploader.UploadSegment(context.Background(), "vol-123", "/nonexistent/disk.400000000000000100000001.log")
	require.Error(t, err)
	assert.Empty(t, fake.uploads)
}

func TestCloudSegmentUploaderPropagatesUploadFailure(t *testing.T) {
	fake := &fakeUpdatesClient{uploadErr: errors.New("cloud unavailable")}
	uploader := NewCloudSegmentUploaderWithClient(slog.Default(), fake, nil)

	_, err := uploader.UploadSegment(context.Background(), "vol-123",
		writeTestSegment(t, "400000000000000100000001", []byte("data")))
	require.ErrorContains(t, err, "cloud unavailable")
}

// A protocol fixture, not the cloud implementation. Wire names and checksum
// calculation are independent of the client DTOs. Cloud service/storage tests
// must separately prove these server guarantees against the database and GCS.
type uploadProtocolServer struct {
	mu                             sync.Mutex
	url                            string
	rows                           map[string]*protocolUpdate
	order                          []string
	failures                       map[string]int // stage:ordering-key, each injected failure consumed once
	prepareLatency                 time.Duration
	activeTransfers, peakTransfers int
}

type protocolUpdate struct {
	kind, key, md5, crc string
	size                int64
	data                []byte
	prepared, active    bool
	accepted            bool
}

func (s *uploadProtocolServer) fail(stage, key string) bool {
	k := stage + ":" + key
	if s.failures[k] == 0 {
		return false
	}
	s.failures[k]--
	return true
}

func (s *uploadProtocolServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind    string `json:"kind"`
		Key     string `json:"ordering_key"`
		MD5     string `json:"md5"`
		CRC     string `json:"crc32c"`
		Size    int64  `json:"size"`
		Prepare bool   `json:"prepare_only"`
	}
	if r.Method == "POST" {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
	}
	if r.Method != "PUT" && strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get("Authorization") != "Bearer test-jwt-token" {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	if body.Prepare && s.prepareLatency > 0 {
		s.mu.Lock()
		s.activeTransfers++
		s.peakTransfers = max(s.peakTransfers, s.activeTransfers)
		s.mu.Unlock()
		time.Sleep(s.prepareLatency)
		s.mu.Lock()
		s.activeTransfers--
		s.mu.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.HasSuffix(r.URL.Path, "/updates/upload") {
		row := s.rows[body.Key]
		if row == nil {
			row = &protocolUpdate{kind: body.Kind, key: body.Key, md5: body.MD5, crc: body.CRC, size: body.Size}
			s.rows[body.Key] = row
		} else if row.kind != body.Kind || row.md5 != body.MD5 || row.crc != body.CRC || row.size != body.Size {
			http.Error(w, "payload conflict", http.StatusConflict)
			return
		}
		if row.accepted && !row.active {
			// Model accepted work publishing after the waiter timed out, before
			// the next begin. This is not a real PG/bgtask worker fixture.
			row.active = true
			s.order = append(s.order, body.Key)
		}
		if s.fail("begin", body.Key) {
			http.Error(w, "lost begin response", 500)
			return
		}
		response := map[string]any{
			"update_id":        "volup-" + body.Key,
			"complete_url":     strings.TrimSuffix(r.URL.Path, "/upload") + "/volup-" + body.Key + "/complete",
			"supports_prepare": true, "already_active": row.active, "kind": row.kind,
			"prepared":     row.prepared && !row.active,
			"ordering_key": row.key, "md5": row.md5, "crc32c": row.crc, "size": row.size,
		}
		// Cloud services/disk/updates.go returns immutable prepared identities
		// without an upload URL: staging was consumed during preparation.
		if !row.prepared && !row.active {
			response["upload_url"] = s.url + "/put/" + body.Key
		}
		json.NewEncoder(w).Encode(response)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/updates") {
		var updates []map[string]any
		for _, key := range s.order {
			if key > r.URL.Query().Get("after") {
				updates = append(updates, map[string]any{"update_id": "volup-" + key, "kind": "lbd_log", "ordering_key": key, "size": s.rows[key].size})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"updates": updates})
		return
	}
	key := strings.TrimPrefix(filepath.Base(r.URL.Path), "volup-")
	if strings.HasSuffix(r.URL.Path, "/complete") || strings.HasSuffix(r.URL.Path, "/prepare") || strings.HasSuffix(r.URL.Path, "/download") {
		key = strings.TrimPrefix(filepath.Base(filepath.Dir(r.URL.Path)), "volup-")
	}
	row := s.rows[key]
	if row == nil {
		http.Error(w, "missing", 404)
		return
	}
	if r.Method == "PUT" {
		if row.prepared || row.active {
			http.Error(w, "prepared payload is immutable", http.StatusConflict)
			return
		}
		if s.fail("put", key) {
			http.Error(w, "PUT failed", http.StatusServiceUnavailable)
			return
		}
		data, _ := io.ReadAll(r.Body)
		sum := md5.Sum(data)
		crc := crc32.New(crc32.MakeTable(crc32.Castagnoli))
		crc.Write(data)
		if int64(len(data)) != row.size || base64.StdEncoding.EncodeToString(sum[:]) != row.md5 || base64.StdEncoding.EncodeToString(crc.Sum(nil)) != row.crc {
			http.Error(w, "checksum mismatch", 400)
			return
		}
		row.data = data
		return
	}
	if strings.HasSuffix(r.URL.Path, "/complete") || strings.HasSuffix(r.URL.Path, "/prepare") {
		if row.md5 != body.MD5 || row.crc != body.CRC || row.size != body.Size || row.data == nil {
			http.Error(w, "completion mismatch", http.StatusConflict)
			return
		}
		stage := "complete"
		if body.Prepare {
			stage = "prepare"
		}
		if s.fail(stage, key) {
			http.Error(w, stage+" failed", 500)
			return
		}
		row.prepared = true
		if !body.Prepare && s.fail("accepted", key) {
			row.accepted = true
			http.Error(w, "accepted work still running after wait timeout", http.StatusInternalServerError)
			return
		}
		if !body.Prepare && !row.active {
			row.active = true
			s.order = append(s.order, key)
			if s.fail("lost", key) {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					conn.Close()
				}
				return
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"update_id": "volup-" + key, "prepared": body.Prepare})
		return
	}
	if strings.HasSuffix(r.URL.Path, "/download") {
		if !row.active {
			http.Error(w, "not active", 404)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"download_url": s.url + "/blob/" + key})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/blob/") && row.active {
		w.Write(row.data)
		return
	}
	http.Error(w, "unknown", 404)
}

func TestUploadPipelineFailureRecoveryAndReplay(t *testing.T) {
	for _, stage := range []string{"begin", "put", "prepare", "complete", "lost", "accepted"} {
		t.Run(stage, func(t *testing.T) {
			ts, h, auth := newTestUploaderServer(t)
			remote := &uploadProtocolServer{url: ts.URL, rows: map[string]*protocolUpdate{}, failures: map[string]int{}, prepareLatency: time.Millisecond}
			h.handler = remote.ServeHTTP
			diskPath := t.TempDir()
			logDir := filepath.Join(diskPath, "logs")
			require.NoError(t, os.Mkdir(logDir, 0755))
			state := NewState()
			state.SetPath(diskPath)
			require.NoError(t, state.SetVolumeAndSave("disk_volume/test", &VolumeState{
				VolumeId: "test", CloudVolumeId: "vol-test", DiskPath: diskPath, Mode: storage_v1alpha.VM_ACCELERATOR,
			}))
			const count = 27 // multiple batches, overlapping writes make misordering observable
			expected := make([]byte, 4*4096)
			var keys []string
			for i := 0; i < count; i++ {
				key := fmt.Sprintf("4000000000000001%08x", i+1)
				keys = append(keys, key)
				data := bytes.Repeat([]byte{byte(i + 1)}, 4096)
				block := (i * 3) % 4
				copy(expected[block*4096:], data)
				segment := buildSegmentData(t, 4096, key, []lbd.Entry{{Op: "W", Block: uint64(block), Length: 4096, Checksum: crc32.ChecksumIEEE(data), Data: data}})
				require.NoError(t, os.WriteFile(filepath.Join(logDir, "disk."+key+".log"), segment, 0644))
			}
			remote.failures[stage+":"+keys[1]] = 1
			client := NewCloudUpdatesClient(slog.New(slog.NewTextHandler(io.Discard, nil)), ts.URL, auth)
			watch := NewLogWatcher(slog.Default(), state, NewCloudSegmentUploaderWithClient(slog.Default(), client, state), time.Second)
			watch.scanAndUpload(context.Background())
			horizon, err := readLogHorizon(diskPath)
			require.NoError(t, err)
			require.Empty(t, horizon, "a failed batch cannot retire any local prefix")
			require.True(t, state.HasPendingCloudUpload("vol-test"))
			for _, key := range keys {
				require.FileExists(t, filepath.Join(logDir, "disk."+key+".log"))
			}
			remote.mu.Lock()
			prefix := append([]string{}, remote.order...)
			if stage == "put" {
				assert.Equal(t, keys[:1], prefix)
				assert.False(t, remote.rows[keys[1]].prepared)
				for _, key := range keys[2:uploadConcurrency] {
					assert.True(t, remote.rows[key].prepared, "later concurrent work survives the earlier PUT failure")
					assert.False(t, remote.rows[key].active)
				}
			}
			remote.mu.Unlock()
			require.Equal(t, keys[:len(prefix)], prefix, "replay must never see a hole")
			// Rebuild state, client and watcher as after a process restart.
			state, err = LoadState(diskPath)
			require.NoError(t, err)
			client = NewCloudUpdatesClient(slog.New(slog.NewTextHandler(io.Discard, nil)), ts.URL, auth)
			NewLogWatcher(slog.Default(), state, NewCloudSegmentUploaderWithClient(slog.Default(), client, state), time.Second).scanAndUpload(context.Background())
			require.False(t, state.HasPendingCloudUpload("vol-test"))
			entries, err := os.ReadDir(logDir)
			require.NoError(t, err)
			require.Empty(t, entries)
			horizon, err = readLogHorizon(diskPath)
			require.NoError(t, err)
			require.Equal(t, keys[len(keys)-1], horizon)
			require.Equal(t, keys, remote.order)
			require.Len(t, remote.rows, count, "retries must reuse remote identities")
			require.LessOrEqual(t, remote.peakTransfers, 8)
			require.Greater(t, remote.peakTransfers, 1)
			// Restore into a distinct empty image through authenticated list/download
			// and the real LBD replay parser, not from the upload fixture's memory.
			restore := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(restore, "disk.img"), make([]byte, len(expected)), 0644))
			mc := NewDiskMountController(slog.Default(), restore, compute.NewNodeId("replay"), NewState(), newMockDiskMountOps())
			mc.SetCloudClient(NewCloudDiskClient(slog.Default(), ts.URL, auth))
			require.NoError(t, mc.replayMissingSegments(context.Background(), &VolumeState{DiskPath: restore, CloudVolumeId: "vol-test"}))
			actual, err := os.ReadFile(filepath.Join(restore, "disk.img"))
			require.NoError(t, err)
			require.Equal(t, expected, actual)
		})
	}
}

func TestLogWatcherNewVolumeProgressesDuringBlockedCompletion(t *testing.T) {
	ts, h, auth := newTestUploaderServer(t)
	remote := &uploadProtocolServer{url: ts.URL, rows: map[string]*protocolUpdate{}, failures: map[string]int{}}
	const oldKey = "400000000000000100000001"
	const newKey = "400000000000000200000001"
	blocked := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	h.handler = func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/volup-"+oldKey+"/complete") {
			data, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			var body struct {
				Prepare bool `json:"prepare_only"`
			}
			if err := json.Unmarshal(data, &body); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(data))
			if !body.Prepare {
				once.Do(func() { close(blocked) })
				<-release
			}
		}
		remote.ServeHTTP(w, r)
	}
	state, err := LoadState(t.TempDir())
	require.NoError(t, err)
	addVolume := func(id, key string, count int) string {
		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, "logs"), 0755))
		for i := 0; i < count; i++ {
			label := key[:16] + fmt.Sprintf("%08x", i+1)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "logs", "disk."+label+".log"), []byte(label), 0600))
		}
		require.NoError(t, state.SetVolumeAndSave(id, &VolumeState{
			EntityId: id, VolumeId: id, CloudVolumeId: id, DiskPath: dir, Mode: storage_v1alpha.VM_ACCELERATOR,
		}))
		return dir
	}
	oldDir := addVolume("vol-old", oldKey, uploadConcurrency+1)
	require.NoError(t, state.SetMountAndSave("mount", &MountState{
		EntityId: "mount", VolumeId: "vol-old", CloudVolumeId: "vol-old", LeaseNonce: "nonce", LeaseOwned: true, Mode: storage_v1alpha.VM_ACCELERATOR,
	}))
	ctx, cancel := context.WithCancel(t.Context())
	watch := NewLogWatcher(slog.Default(), state, NewCloudSegmentUploader(slog.Default(), ts.URL, auth, state), time.Millisecond)
	runErr := make(chan error, 1)
	go func() { runErr <- watch.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		close(release)
		watch.Wait()
	})
	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("old volume did not reach completion")
	}
	// Discover a disk after the old scan started, not just in its snapshot.
	newDir := addVolume("vol-new", newKey, 2)
	require.Eventually(t, func() bool {
		horizon, err := readLogHorizon(newDir)
		return err == nil && horizon == "400000000000000200000002"
	}, 5*time.Second, time.Millisecond)
	remote.mu.Lock()
	order := append([]string(nil), remote.order...)
	remote.mu.Unlock()
	require.Equal(t, []string{newKey, "400000000000000200000002"}, order)
	// Neither deletion nor cancellation may discard the unresolved batch.
	vc := NewDiskVolumeController(slog.Default(), t.TempDir(), compute.NewNodeId("node"), state, NewRealDiskVolumeOps(slog.Default()), newMockDiskMountOps())
	deleteCtx, deleteCancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer deleteCancel()
	require.ErrorIs(t, vc.Reconcile(deleteCtx, &storage_v1alpha.DiskVolume{
		ID: "vol-old", DesiredState: storage_v1alpha.DV_ABSENT, ActualState: storage_v1alpha.DV_READY,
	}, nil), context.DeadlineExceeded)
	cancel()
	select {
	case err := <-runErr:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("watcher did not join cancelled uploads")
	}
	watch.Wait()
	require.Equal(t, "nonce", state.LeaseNonceForCloudVolume("vol-old"))
	require.True(t, state.HasPendingCloudUpload("vol-old"))
	horizon, err := readLogHorizon(oldDir)
	require.NoError(t, err)
	require.Empty(t, horizon)
	files, err := os.ReadDir(filepath.Join(oldDir, "logs"))
	require.NoError(t, err)
	require.Len(t, files, uploadConcurrency+1)
}

func TestDiskMountReleaseDrainsFinalSegmentBeforeLease(t *testing.T) {
	ts, h, auth := newTestUploaderServer(t)
	remote := &uploadProtocolServer{url: ts.URL, rows: map[string]*protocolUpdate{}, failures: map[string]int{}}
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	require.NoError(t, os.Mkdir(logDir, 0755))
	state, err := LoadState(dir)
	require.NoError(t, err)
	require.NoError(t, state.SetVolumeAndSave("volume", &VolumeState{EntityId: "volume", DiskPath: dir, CloudVolumeId: "vol-test", Mode: storage_v1alpha.VM_ACCELERATOR}))
	require.NoError(t, state.SetMountAndSave("mount", &MountState{
		EntityId: "mount", VolumeId: "volume", CloudVolumeId: "vol-test", BackingPath: filepath.Join(dir, "disk.img"),
		DevicePath: "/dev/lbd0", MountPath: "/mnt/volume", Mounted: true, Mode: storage_v1alpha.VM_ACCELERATOR,
		LeaseNonce: "nonce",
	}))
	h.handler = func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			data, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			var request struct {
				Nonce string `json:"lease_nonce"`
			}
			if err := json.Unmarshal(data, &request); err != nil || request.Nonce != "nonce" || state.LeaseNonceForCloudVolume("vol-test") != "nonce" {
				http.Error(w, "lease lost before tail publication", http.StatusForbidden)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(data))
		}
		remote.ServeHTTP(w, r)
	}
	var keys []string
	expected := make([]byte, 4096)
	const count = logScanWindow + 1
	write := func(i int) {
		key := fmt.Sprintf("4000000000000001%08x", i+1)
		keys = append(keys, key)
		data := bytes.Repeat([]byte{byte(i + 1)}, 4096)
		copy(expected, data)
		segment := buildSegmentData(t, 4096, key, []lbd.Entry{{Op: "W", Block: 0, Length: 4096, Checksum: crc32.ChecksumIEEE(data), Data: data}})
		require.NoError(t, os.WriteFile(filepath.Join(logDir, "disk."+key+".log"), segment, 0600))
	}
	for i := 0; i < count-1; i++ {
		write(i)
	}
	ops := newMockDiskMountOps()
	ops.lbdBackings["/dev/lbd0"] = filepath.Join(dir, "disk.img")
	// Kernel unmount succeeded before the process could persist LeaseOwned.
	ops.onLbdDetach = func() {
		write(count - 1)
		volumeController := NewDiskVolumeController(slog.Default(), dir, compute.NewNodeId("node"), state, NewRealDiskVolumeOps(slog.Default()), ops)
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		require.ErrorIs(t, volumeController.Reconcile(ctx, &storage_v1alpha.DiskVolume{
			ID: "volume", DesiredState: storage_v1alpha.DV_ABSENT, ActualState: storage_v1alpha.DV_READY,
		}, nil), context.DeadlineExceeded, "deletion between detach and drain must wait for publication")
	}
	remote.failures["complete:"+keys[1]] = 1
	cloud := &mockCloudDiskClient{}
	client := NewCloudUpdatesClient(slog.Default(), ts.URL, auth)
	controller := newTestDiskMountController(slog.Default(), dir, "node", nil, state, ops)
	controller.SetCloudClient(cloud)
	controller.SetUpdatesClient(client)
	mount := &storage_v1alpha.DiskMount{ID: "mount", DesiredState: storage_v1alpha.DM_WANT_UNMOUNTED}
	require.Error(t, controller.Reconcile(t.Context(), mount, nil))
	require.Empty(t, cloud.releaseLeaseCalls)
	require.Len(t, keys, count, "detach closes the final segment before drain")
	for _, key := range keys {
		require.FileExists(t, filepath.Join(logDir, "disk."+key+".log"))
	}
	state, err = LoadState(dir)
	require.NoError(t, err)
	require.True(t, state.HasPendingCloudUpload("vol-test"))
	require.Equal(t, "nonce", state.LeaseNonceForCloudVolume("vol-test"))
	volumeController := NewDiskVolumeController(slog.Default(), dir, compute.NewNodeId("node"), state, NewRealDiskVolumeOps(slog.Default()), ops)
	require.Error(t, volumeController.Reconcile(t.Context(), &storage_v1alpha.DiskVolume{
		ID: "volume", DesiredState: storage_v1alpha.DV_ABSENT, ActualState: storage_v1alpha.DV_READY,
	}, nil), "deletion after failed final drain must preserve its original directory")
	require.NotNil(t, state.GetVolume("volume"))
	for _, key := range keys {
		require.FileExists(t, filepath.Join(logDir, "disk."+key+".log"))
	}
	state.DeleteVolume("volume") // orphan retry must use persisted backing and intent
	controller = newTestDiskMountController(slog.Default(), dir, "node", nil, state, ops)
	controller.SetCloudClient(cloud)
	controller.SetUpdatesClient(client)
	require.NoError(t, controller.Reconcile(t.Context(), mount, nil))
	require.Len(t, cloud.releaseLeaseCalls, 1)
	require.Nil(t, state.GetMount("mount"))
	require.False(t, state.HasPendingCloudUpload("vol-test"))
	entries, err := os.ReadDir(logDir)
	require.NoError(t, err)
	require.Empty(t, entries)
	require.Equal(t, keys, remote.order)
	restore := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(restore, "disk.img"), make([]byte, 4096), 0600))
	controller = NewDiskMountController(slog.Default(), restore, compute.NewNodeId("replay"), NewState(), newMockDiskMountOps())
	controller.SetCloudClient(NewCloudDiskClient(slog.Default(), ts.URL, auth))
	require.NoError(t, controller.replayMissingSegments(t.Context(), &VolumeState{DiskPath: restore, CloudVolumeId: "vol-test"}))
	actual, err := os.ReadFile(filepath.Join(restore, "disk.img"))
	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

func TestUploadPipelineRechecksReplayHorizon(t *testing.T) {
	ts, h, auth := newTestUploaderServer(t)
	remote := &uploadProtocolServer{url: ts.URL, rows: map[string]*protocolUpdate{}, failures: map[string]int{}}
	h.handler = remote.ServeHTTP
	diskPath := t.TempDir()
	state := NewState()
	state.SetPath(diskPath)
	require.NoError(t, os.Mkdir(filepath.Join(diskPath, "logs"), 0755))
	keys := []string{"400000000000000100000001", "400000000000000100000002"}
	var paths []string
	for _, key := range keys {
		path := filepath.Join(diskPath, "logs", "disk."+key+".log")
		require.NoError(t, os.WriteFile(path, []byte(key), 0644))
		paths = append(paths, path)
	}
	unlock, err := state.LockCloudVolume(t.Context(), "vol-test")
	require.NoError(t, err)
	u := NewCloudSegmentUploader(slog.Default(), ts.URL, auth, state)
	done := make(chan error, 1)
	go func() {
		done <- u.UploadSegments(t.Context(), "vol-test", diskPath, paths, func(path string) error {
			if err := updateLogHorizonFromPath(diskPath, path); err != nil {
				return err
			}
			return os.Remove(path)
		})
	}()
	// Replay covers the first selected segment before upload acquires the lock.
	require.NoError(t, writeLogHorizon(diskPath, keys[0]))
	unlock()
	require.NoError(t, <-done)
	require.Equal(t, keys[1:], remote.order)
	require.Len(t, remote.rows, 1, "replay-covered payload must never begin a remote update")
	for _, path := range paths {
		require.NoFileExists(t, path)
	}
	horizon, err := readLogHorizon(diskPath)
	require.NoError(t, err)
	require.Equal(t, keys[1], horizon)
}

func TestUploadPipelineCoveredBatchPreservesExistingIntent(t *testing.T) {
	diskPath := t.TempDir()
	state := NewState()
	state.SetPath(diskPath)
	require.NoError(t, state.SetPendingCloudUpload("vol-test", "earlier-upload"))
	key := "400000000000000100000001"
	require.NoError(t, writeLogHorizon(diskPath, key))
	path := filepath.Join(diskPath, "disk."+key+".log")
	require.NoError(t, os.WriteFile(path, []byte(key), 0644))
	u := NewCloudSegmentUploaderWithClient(slog.Default(), nil, state)
	require.NoError(t, u.UploadSegments(t.Context(), "vol-test", diskPath, []string{path}, os.Remove))
	state, err := LoadState(diskPath)
	require.NoError(t, err)
	require.True(t, state.HasPendingCloudUpload("vol-test"), "covered work cannot reconcile an earlier intent")
}

func TestUploadPipelineCrashBoundaries(t *testing.T) {
	if phase := os.Getenv("LBD_UPLOAD_CRASH_PHASE"); phase != "" {
		diskPath, apiURL := os.Getenv("LBD_UPLOAD_CRASH_DIR"), os.Getenv("LBD_UPLOAD_CRASH_URL")
		state, err := LoadState(diskPath)
		require.NoError(t, err)
		auth := newTestAuthClient(t, apiURL)
		u := NewCloudSegmentUploader(slog.Default(), apiURL, auth, state)
		paths := []string{
			filepath.Join(diskPath, "logs/disk.400000000000000100000001.log"),
			filepath.Join(diskPath, "logs/disk.400000000000000100000002.log"),
		}
		if phase == "preflight-error" {
			require.NoError(t, os.Mkdir(filepath.Join(diskPath, logHorizonFile), 0755))
		}
		err = u.UploadSegments(context.Background(), "vol-test", diskPath, paths, func(path string) error {
			if phase == "after-horizon" {
				require.NoError(t, updateLogHorizonFromPath(diskPath, path))
			}
			os.Exit(42) // abrupt process exit, bypassing all cleanup and unlock defers
			return nil
		})
		if phase == "preflight-error" {
			require.Error(t, err)
			os.Exit(42)
		}
		require.NoError(t, err)
		t.Fatal("did not reach crash boundary")
	}
	for _, phase := range []string{"preflight-error", "after-completion", "after-horizon"} {
		t.Run(phase, func(t *testing.T) {
			ts, h, auth := newTestUploaderServer(t)
			remote := &uploadProtocolServer{url: ts.URL, rows: map[string]*protocolUpdate{}, failures: map[string]int{}}
			h.handler = remote.ServeHTTP
			diskPath := t.TempDir()
			logDir := filepath.Join(diskPath, "logs")
			require.NoError(t, os.Mkdir(logDir, 0755))
			state := NewState()
			state.SetPath(diskPath)
			require.NoError(t, state.SetVolumeAndSave("disk_volume/test", &VolumeState{DiskPath: diskPath, CloudVolumeId: "vol-test", Mode: storage_v1alpha.VM_ACCELERATOR}))
			for _, key := range []string{"400000000000000100000001", "400000000000000100000002"} {
				require.NoError(t, os.WriteFile(filepath.Join(logDir, "disk."+key+".log"), []byte(key), 0644))
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestUploadPipelineCrashBoundaries$")
			cmd.Env = append(os.Environ(), "LBD_UPLOAD_CRASH_PHASE="+phase, "LBD_UPLOAD_CRASH_DIR="+diskPath, "LBD_UPLOAD_CRASH_URL="+ts.URL)
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			require.ErrorAs(t, err, &exit, string(output))
			require.Equal(t, 42, exit.ExitCode(), string(output))
			state, err = LoadState(diskPath)
			require.NoError(t, err)
			require.False(t, state.HasPendingCloudUpload("vol-test"), "no stranded gate before remote work or after confirmation")
			entries, err := os.ReadDir(logDir)
			require.NoError(t, err)
			require.Len(t, entries, 2, "crash must leave local payloads recoverable")
			remoteCount := 2
			if phase == "preflight-error" {
				remoteCount = 0
				require.NoError(t, os.Remove(filepath.Join(diskPath, logHorizonFile)))
				require.NoError(t, writeLogHorizon(diskPath, "400000000000000100000002"))
			}
			remote.mu.Lock()
			require.Len(t, remote.order, remoteCount)
			remote.mu.Unlock()
			NewLogWatcher(slog.Default(), state, NewCloudSegmentUploader(slog.Default(), ts.URL, auth, state), time.Second).scanAndUpload(context.Background())
			entries, err = os.ReadDir(logDir)
			require.NoError(t, err)
			require.Empty(t, entries)
			horizon, err := readLogHorizon(diskPath)
			require.NoError(t, err)
			require.Equal(t, "400000000000000100000002", horizon)
			remote.mu.Lock()
			require.Len(t, remote.rows, remoteCount)
			require.Len(t, remote.order, remoteCount, "restart must not republish")
			remote.mu.Unlock()
		})
	}
}
