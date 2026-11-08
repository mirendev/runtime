package diskio

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/lbd"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	_ "modernc.org/sqlite"
)

// Only the not-yet-implemented cloud catalog is simulated. These tests use the
// real HTTP clients, upload claims, snapshot format, LBD decoder and disk I/O.
type recoveryCloud struct {
	*mockCloudServer
	mu              sync.Mutex
	manifest        RecoveryManifest
	publicationFail bool
	losePublication bool
	uploadFail      bool
	// stallUpload holds checkpoint PUTs open until the client gives up.
	stallUpload   bool
	downloadDelay time.Duration
	renewFail     bool
	// stallRenewal holds the next renewal open until its request is cancelled.
	stallRenewal    bool
	renewals        int
	sessionExpiry   time.Time
	publications    []CheckpointPublication
	claims          map[string]beginUploadRequestJSON
	selection       RecoveryManifest
	pageFault       string
	checkpointFault string
	oldCloud        bool
	// completeOnlyStreamCheck models a cloud that refuses a stream only at
	// completion, after the payload is uploaded.
	completeOnlyStreamCheck bool
	begins                  int
	// source is the history of another volume, "src", that a new volume is
	// restored from.
	source *RecoveryManifest
	// takeovers records take_over_before of each lease takeover request.
	takeovers []time.Time
}

func newRecoveryCloud(t *testing.T) (*recoveryCloud, *DiskMountController, *VolumeState) {
	t.Helper()
	cloud := &recoveryCloud{mockCloudServer: newMockCloudServer(t), manifest: RecoveryManifest{
		Version: 1, VolumeID: "vol", StreamID: "stream", Status: "never_backed_up",
		SessionID: "session", ExpiresAt: time.Now().Add(time.Minute), Complete: true, DeviceSize: 1024 * 1024,
	}}
	cloud.server.Config.Handler = http.HandlerFunc(cloud.serveHTTP)
	auth := newTestAuthClient(t, cloud.URL())
	dir := t.TempDir()
	state, err := LoadState(dir)
	require.NoError(t, err)
	v := &VolumeState{EntityId: "volume", VolumeId: "volume", CloudVolumeId: "vol", DiskPath: dir,
		Mode: storage_v1alpha.VM_ACCELERATOR, SizeBytes: 1024 * 1024, Filesystem: "ext4"}
	require.NoError(t, state.SetVolumeAndSave(v.EntityId, v))
	c := NewDiskMountController(slog.Default(), dir, compute.NewNodeId("fresh-node"), state, newMockDiskMountOps())
	updates := NewCloudUpdatesClient(slog.Default(), cloud.URL(), auth)
	c.SetUpdatesClient(updates)
	c.SetCloudClient(NewCloudDiskClientWithUpdates(slog.Default(), cloud.URL(), auth, updates))
	return cloud, c, v
}

func (s *recoveryCloud) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/lease/acquire") || strings.HasSuffix(r.URL.Path, "/lease/takeover") {
		var request acquireLeaseRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if takeover := strings.HasSuffix(r.URL.Path, "/lease/takeover"); takeover != (request.TakeOverBefore != nil) {
			w.WriteHeader(http.StatusBadRequest)
			return
		} else if takeover {
			s.mu.Lock()
			s.takeovers = append(s.takeovers, *request.TakeOverBefore)
			s.mu.Unlock()
		}
		s.mockCloudServer.mu.Lock()
		s.leases["vol"] = &activeLease{Nonce: "nonce"}
		s.mockCloudServer.mu.Unlock()
		json.NewEncoder(w).Encode(leaseResponse{VolumeID: "vol", Nonce: "nonce", AcquisitionID: request.AcquisitionID})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.oldCloud {
		s.ServeHTTP(w, r)
		return
	}
	if strings.Contains(r.URL.Path, "/updates") && r.Header.Get("Authorization") != "Bearer test-jwt-token" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch {
	case strings.HasSuffix(r.URL.Path, "/updates/upload"):
		var claim beginUploadRequestJSON
		require.NoError(s.t, json.NewDecoder(r.Body).Decode(&claim))
		encoded, err := json.Marshal(claim)
		require.NoError(s.t, err)
		r.Body = io.NopCloser(bytes.NewReader(encoded))
		if claim.Kind != string(KindLBDCheckpoint) {
			s.ServeHTTP(w, r)
			return
		}
		s.begins++
		if claim.StreamID == "" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		if !s.completeOnlyStreamCheck && s.manifest.Status == "ready" && claim.StreamID != s.manifest.StreamID {
			// Matches MIR-2185: a plain 409 with no code, before any upload URL.
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error":"volume has a different accelerator stream"}`))
			return
		}
		var response beginUploadResponseJSON
		for id, previous := range s.claims {
			if previous.OrderingKey == claim.OrderingKey {
				// Like MIR-2185, the existing-key check ignores the nonce, so a
				// retry under a later lease resumes the same update.
				previous.LeaseNonce = claim.LeaseNonce
				if previous != claim {
					w.WriteHeader(http.StatusUnprocessableEntity)
					return
				}
				s.mockCloudServer.mu.Lock()
				response = beginUploadResponseJSON{UpdateID: id, UploadURL: s.URL() + "/upload/" + id,
					AlreadyActive: s.segments[id].Complete, Kind: claim.Kind, OrderingKey: claim.OrderingKey,
					MD5: claim.MD5, CRC32C: claim.CRC32C, Size: claim.Size}
				s.mockCloudServer.mu.Unlock()
				break
			}
		}
		if response.UpdateID == "" {
			recorder := httptest.NewRecorder()
			s.handleUploadRequest(recorder, r)
			if recorder.Code != http.StatusOK {
				w.WriteHeader(recorder.Code)
				w.Write(recorder.Body.Bytes())
				return
			}
			require.NoError(s.t, json.Unmarshal(recorder.Body.Bytes(), &response))
			if s.claims == nil {
				s.claims = make(map[string]beginUploadRequestJSON)
			}
			s.claims[response.UpdateID] = claim
		}
		if s.checkpointFault != "unsupported" {
			response.RecoveryVersion = AcceleratorRecoveryVersion
		}
		json.NewEncoder(w).Encode(response)
	case strings.HasSuffix(r.URL.Path, "/updates/restore-sessions"):
		var request struct {
			Until string `json:"until"`
		}
		require.NoError(s.t, json.NewDecoder(r.Body).Decode(&request))
		s.selection = s.manifest
		if strings.Contains(r.URL.Path, "/volumes/src/") {
			s.selection = *s.source
		}
		s.selection.Segments = append([]RecoverySegment(nil), s.selection.Segments...)
		if request.Until != "" {
			// Like the cloud: the newest base at or before until, then the
			// tail through until. These tests keep one base.
			kept := s.selection.Segments[:0]
			for _, segment := range s.selection.Segments {
				if segment.Label <= request.Until {
					kept = append(kept, segment)
					s.selection.Head = segment.Label
				}
			}
			s.selection.Segments, s.selection.SegmentCount = kept, len(kept)
		}
		if s.selection.VolumeID == "vol" && (s.manifest.StreamID == "" || s.manifest.Status == "never_backed_up") {
			// No acknowledged stream: cloud retains the whole log, or has
			// nothing at all, and names no stream either way.
			status := "never_backed_up"
			s.mockCloudServer.mu.Lock()
			for _, object := range s.segments {
				if object.Complete && object.Kind == string(KindLBDLog) && object.VolumeID == "vol" {
					status = "legacy"
				}
			}
			s.mockCloudServer.mu.Unlock()
			s.selection = RecoveryManifest{Version: 1, VolumeID: "vol", Status: status, SessionID: "session",
				ExpiresAt: time.Now().Add(time.Minute), Complete: true}
		}
		s.sessionExpiry = s.selection.ExpiresAt
		json.NewEncoder(w).Encode(restoreSession{Version: 1, VolumeID: s.selection.VolumeID, SessionID: "session", ExpiresAt: s.selection.ExpiresAt})
	case strings.Contains(r.URL.Path, "/restore-sessions/") && strings.HasSuffix(r.URL.Path, "/renew"):
		if s.stallRenewal {
			s.stallRenewal = false
			s.mu.Unlock()
			// The server notices a client hang-up only after the body is read.
			_, _ = io.Copy(io.Discard, r.Body)
			<-r.Context().Done()
			s.mu.Lock()
			return
		}
		// An expired session cannot be revived, so only timely renewal succeeds.
		if s.renewFail || time.Now().After(s.sessionExpiry) {
			w.WriteHeader(http.StatusGone)
			return
		}
		s.renewals++
		s.sessionExpiry = time.Now().Add(time.Minute)
		json.NewEncoder(w).Encode(map[string]any{"version": 1, "volume_id": s.selection.VolumeID, "session_id": "session", "expires_at": s.sessionExpiry})
	case strings.HasSuffix(r.URL.Path, "/updates") && r.URL.Query().Get("restore_session") != "":
		start, err := strconv.Atoi(r.URL.Query().Get("cursor"))
		if r.URL.Query().Get("cursor") == "" {
			start, err = 0, nil
		}
		if err != nil || start > len(s.selection.Segments) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		end := min(start+100, len(s.selection.Segments))
		manifest := s.selection
		page := listUpdatesResponseJSON{Recovery: &manifest}
		for _, segment := range s.selection.Segments[start:end] {
			page.Updates = append(page.Updates, UpdateInfo{UpdateID: segment.UpdateID, Kind: string(KindLBDLog),
				OrderingKey: segment.Label, Size: segment.Size, SHA256: segment.SHA256,
				PreviousLabel: segment.PreviousLabel, FirstSequence: segment.FirstSequence, EntryCount: segment.EntryCount})
		}
		if end < len(s.selection.Segments) {
			page.NextCursor = strconv.Itoa(end)
		}
		if s.pageFault == "legacy-list" {
			page.Recovery = nil
		}
		if start > 0 {
			switch s.pageFault {
			case "changed-head":
				manifest.Head = "400000000000000000000001"
			case "wrong-session":
				manifest.SessionID = "another-session"
			case "missing-metadata":
				page.Recovery = nil
			case "early-eof":
				page.Updates, page.NextCursor = nil, ""
			case "repeated-cursor":
				page.NextCursor = strconv.Itoa(start)
			}
		}
		json.NewEncoder(w).Encode(page)
	case strings.HasSuffix(r.URL.Path, "/complete"):
		var completion completeUploadRequestJSON
		if err := json.NewDecoder(r.Body).Decode(&completion); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		encoded, err := json.Marshal(completion)
		require.NoError(s.t, err)
		r.Body = io.NopCloser(bytes.NewReader(encoded))
		if completion.Checkpoint == nil {
			s.ServeHTTP(w, r)
			return
		}
		p := *completion.Checkpoint
		s.mockCloudServer.mu.Lock()
		head := s.manifest.Head
		for _, object := range s.segments {
			if object.Complete && object.Kind == string(KindLBDLog) && object.VolumeID == "vol" && object.Label > head {
				head = object.Label
			}
		}
		base := s.segments[p.Base.UpdateID]
		valid := base != nil && base.Kind == string(KindLBDCheckpoint) && recoveryObject(p.Base.UpdateID, base.Data) == p.Base.RecoveryObject
		if lease := s.leases["vol"]; lease != nil && lease.Nonce != completion.LeaseNonce {
			valid = false
		}
		s.mockCloudServer.mu.Unlock()
		if s.manifest.Status != "ready" {
			s.manifest.StreamID = p.StreamID
		}
		if valid && p.StreamID != s.manifest.StreamID {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"code":"stream_conflict"}`))
			return
		}
		// A replayed image may stop at any committed log above the newest
		// checkpoint; any other image must reach the head.
		boundary := p.Base.ThroughLabel == head
		if p.Basis == checkpointBasisReplayed {
			committed := false
			s.mockCloudServer.mu.Lock()
			for _, object := range s.segments {
				committed = committed || (object.Complete && object.Kind == string(KindLBDLog) && object.Label == p.Base.ThroughLabel)
			}
			s.mockCloudServer.mu.Unlock()
			boundary = committed && (s.manifest.Checkpoint == nil || p.Base.ThroughLabel > s.manifest.Checkpoint.ThroughLabel)
		}
		if !valid || p.VolumeID != s.manifest.VolumeID || !boundary {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		s.publications = append(s.publications, p)
		if s.publicationFail {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		recorder := httptest.NewRecorder()
		s.handleComplete(recorder, r)
		if recorder.Code != http.StatusOK {
			w.WriteHeader(recorder.Code)
			w.Write(recorder.Body.Bytes())
			return
		}
		s.manifest.Status = "ready"
		s.manifest.Checkpoint = &p.Base
		s.manifest.CoverageFloor = p.Base.ThroughLabel
		s.manifest.Segments = slices.DeleteFunc(s.manifest.Segments, func(seg RecoverySegment) bool { return seg.Label <= p.Base.ThroughLabel })
		s.manifest.SegmentCount = len(s.manifest.Segments)
		if len(s.manifest.Segments) == 0 {
			s.manifest.Head = p.Base.ThroughLabel
		}
		if s.losePublication {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		switch s.checkpointFault {
		case "missing-ack":
			json.NewEncoder(w).Encode(map[string]any{"update_id": p.Base.UpdateID})
		case "wrong-ack":
			p.StreamID = "wrong-stream"
			fallthrough
		default:
			json.NewEncoder(w).Encode(map[string]any{"update_id": p.Base.UpdateID, "checkpoint": p})
		}
	default:
		if (s.uploadFail || s.stallUpload) && r.Method == http.MethodPut {
			s.mockCloudServer.mu.Lock()
			object := s.segments[strings.TrimPrefix(r.URL.Path, "/upload/")]
			checkpoint := object != nil && object.Kind == string(KindLBDCheckpoint)
			s.mockCloudServer.mu.Unlock()
			if checkpoint && s.stallUpload {
				s.mu.Unlock()
				_, _ = io.Copy(io.Discard, r.Body)
				<-r.Context().Done()
				s.mu.Lock()
				return
			}
			if checkpoint {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
		}
		if s.downloadDelay > 0 && strings.HasPrefix(r.URL.Path, "/data/") {
			delay := s.downloadDelay
			s.mu.Unlock()
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
			}
			s.mu.Lock()
		}
		s.ServeHTTP(w, r)
	}
}

func recoveryObject(id string, b []byte) RecoveryObject {
	h := sha256.Sum256(b)
	return RecoveryObject{UpdateID: id, Size: int64(len(b)), SHA256: hex.EncodeToString(h[:])}
}

func (s *recoveryCloud) addTail(t *testing.T, entries []lbd.Entry) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	label := fmt.Sprintf("4000000000000000%08x", len(s.manifest.Segments)+2)
	var payload bytes.Buffer
	writer, err := lbd.NewWriter(&payload, lbd.Header{Version: 2, BlockSize: 4096, SegmentLabel: label, DeviceSize: s.manifest.DeviceSize})
	require.NoError(t, err)
	for i := range entries {
		require.NoError(t, writer.WriteEntry(&entries[i]))
	}
	data := payload.Bytes()
	id := "tail-" + label
	s.mockCloudServer.mu.Lock()
	s.segments[id] = &storedSegment{VolumeID: "vol", Data: data, Label: label, Kind: string(KindLBDLog), Complete: true}
	s.mockCloudServer.mu.Unlock()
	first := uint64(0)
	if len(entries) > 0 {
		first = entries[0].Sequence
	}
	s.manifest.Segments = append(s.manifest.Segments, RecoverySegment{
		RecoveryObject: recoveryObject(id, data), Label: label, PreviousLabel: s.manifest.Head,
		FirstSequence: first, EntryCount: uint64(len(entries)),
	})
	s.manifest.SegmentCount++
	s.manifest.Head = label
	return id
}

func seedRecoveryCheckpoint(t *testing.T, c *DiskMountController, v *VolumeState) []byte {
	t.Helper()
	image := make([]byte, 1024*1024)
	copy(image, bytes.Repeat([]byte{0xA7}, 4096)) // written only in retired history
	copy(image[4096:], bytes.Repeat([]byte{0xB2}, 4096))
	require.NoError(t, os.WriteFile(filepath.Join(v.DiskPath, "disk.img"), image, 0600))
	old := buildSegmentData(t, 4096, "400000000000000000000001", []lbd.Entry{makeWriteEntry(0, image[:4096]), makeWriteEntry(1, image[4096:8192])})
	_, err := c.updatesClient.Upload(t.Context(), "vol", UploadRequest{Kind: KindLBDLog, OrderingKey: "400000000000000000000001"}, bytes.NewReader(old), int64(len(old)))
	require.NoError(t, err)
	require.NoError(t, writeLogHorizon(v.DiskPath, "400000000000000000000001"))
	require.NoError(t, writeRecoveryFile(v.DiskPath, acceleratorStreamFile, []byte("stream")))
	require.NoError(t, c.publishAcceleratorCheckpoint(t.Context(), v, "nonce"))
	return image
}

func TestAcceleratorCheckpointFreshNodeRecovery(t *testing.T) {
	cloud, c, v := newRecoveryCloud(t)
	expected := seedRecoveryCheckpoint(t, c, v)
	data := bytes.Repeat([]byte{0xC3}, 4096)
	cloud.addTail(t, []lbd.Entry{
		{Op: "T", Sequence: 41, Block: 1, Length: 4096},
		{Op: "W", Sequence: 42, Block: 2, Length: 4096, Data: data, Checksum: crc32.ChecksumIEEE(data)},
	})
	clear(expected[4096:8192])
	copy(expected[8192:], data)
	// Physically discard all pre-checkpoint log objects and all local files.
	cloud.mockCloudServer.mu.Lock()
	retired := 0
	for id, object := range cloud.segments {
		if object.Kind == string(KindLBDLog) && object.Label <= cloud.manifest.Checkpoint.ThroughLabel {
			delete(cloud.segments, id)
			retired++
		}
	}
	cloud.mockCloudServer.mu.Unlock()
	require.Positive(t, retired)
	v.DiskPath = streamDir(t)
	require.NoError(t, c.recoverAccelerator(t.Context(), v, "nonce"))
	actual, err := os.ReadFile(filepath.Join(v.DiskPath, "disk.img"))
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	horizon, err := readLogHorizon(v.DiskPath)
	require.NoError(t, err)
	require.Equal(t, cloud.manifest.Head, horizon)
	require.Positive(t, cloud.renewals)
	// A stale backing/horizon below the retained floor must bootstrap too.
	require.NoError(t, os.WriteFile(filepath.Join(v.DiskPath, "disk.img"), make([]byte, 1024*1024), 0600))
	require.NoError(t, writeLogHorizon(v.DiskPath, "400000000000000000000000"))
	require.NoError(t, c.recoverAccelerator(t.Context(), v, "nonce"))
	actual, err = os.ReadFile(filepath.Join(v.DiskPath, "disk.img"))
	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

// A node that has never applied anything of the volume (a new cluster
// adopting it, or a host that lost its copy) has no stream file, horizon or
// image. Mounting must rebuild it from the protected checkpoint and tail and
// adopt the cloud's stream, not replay a retired log onto a blank image.
func TestAcceleratorMountAdoptsCloudStreamOnFreshNode(t *testing.T) {
	cloud, c, v := newRecoveryCloud(t)
	expected := seedRecoveryCheckpoint(t, c, v)
	data := bytes.Repeat([]byte{0x5D}, 4096)
	cloud.addTail(t, []lbd.Entry{{Op: "W", Sequence: 41, Block: 3, Length: 4096, Data: data, Checksum: crc32.ChecksumIEEE(data)}})
	copy(expected[3*4096:], data)
	cloud.mockCloudServer.mu.Lock()
	for id, object := range cloud.segments {
		if object.Kind == string(KindLBDLog) && object.Label <= cloud.manifest.Checkpoint.ThroughLabel {
			delete(cloud.segments, id)
		}
	}
	cloud.mockCloudServer.mu.Unlock()

	v.DiskPath = t.TempDir()
	mountAndDetach(t, c, v)
	actual, err := os.ReadFile(filepath.Join(v.DiskPath, "disk.img"))
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	stream, err := os.ReadFile(filepath.Join(v.DiskPath, acceleratorStreamFile))
	require.NoError(t, err)
	require.Equal(t, "stream", string(stream))
	horizon, err := readLogHorizon(v.DiskPath)
	require.NoError(t, err)
	require.Equal(t, cloud.manifest.Head, horizon)
	require.NoFileExists(t, filepath.Join(v.DiskPath, checkpointConflictFile))
}

// A brand-new volume: the cloud has no history and names no stream.
func TestAcceleratorNewVolumeMountsWithoutHistory(t *testing.T) {
	_, c, v := newRecoveryCloud(t)
	v.DiskPath = t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(v.DiskPath, "disk.img"), make([]byte, v.SizeBytes), 0600))
	mountAndDetach(t, c, v)
}

// A legacy volume (no checkpoint stream) whose image is gone replays its
// whole retained log onto a new blank image instead of failing every mount.
func TestAcceleratorLegacyVolumeRebuildsMissingImage(t *testing.T) {
	_, c, v := newRecoveryCloud(t)
	v.DiskPath = t.TempDir()
	data := bytes.Repeat([]byte{0x6E}, 4096)
	const label = "400000000000000000000001"
	segment := buildSegmentData(t, 4096, label, []lbd.Entry{makeWriteEntry(2, data)})
	_, err := c.updatesClient.Upload(t.Context(), "vol", UploadRequest{Kind: KindLBDLog, OrderingKey: label}, bytes.NewReader(segment), int64(len(segment)))
	require.NoError(t, err)

	mountAndDetach(t, c, v)
	image, err := os.ReadFile(filepath.Join(v.DiskPath, "disk.img"))
	require.NoError(t, err)
	require.Len(t, image, int(v.SizeBytes))
	require.Equal(t, data, image[2*4096:3*4096])
}

func TestAcceleratorMountRequiresCompleteRecovery(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(fmt.Sprintf("valid-%t", valid), func(t *testing.T) {
			cloud, c, v := newRecoveryCloud(t)
			expected := seedRecoveryCheckpoint(t, c, v)
			cloud.addTail(t, nil)
			if !valid {
				cloud.mu.Lock()
				cloud.manifest.Segments = nil
				cloud.manifest.SegmentCount = 0 // missing suffix, despite terminal page
				cloud.mu.Unlock()
			}
			v.DiskPath = streamDir(t)
			require.NoError(t, c.state.SetVolumeAndSave(v.EntityId, v))
			mount := &storage_v1alpha.DiskMount{ID: "mount", VolumeId: "volume", MountPath: filepath.Join(v.DiskPath, "mnt"),
				DesiredState: storage_v1alpha.DM_WANT_MOUNTED, ActualState: storage_v1alpha.DM_PENDING}
			err := c.Reconcile(t.Context(), mount, nil)
			if valid {
				require.NoError(t, err)
				require.True(t, c.state.GetMount("mount").Mounted)
				actual, err := os.ReadFile(filepath.Join(v.DiskPath, "disk.img"))
				require.NoError(t, err)
				require.Equal(t, expected, actual)
			} else {
				require.Error(t, err)
				require.Nil(t, c.state.GetMount("mount"))
				require.NoFileExists(t, filepath.Join(v.DiskPath, "disk.img"))
			}
		})
	}
}

func TestAcceleratorRecoveryFailsClosed(t *testing.T) {
	for _, scenario := range []string{"empty", "middle", "suffix", "truncated-manifest", "floor", "expired", "stream-reset", "corrupt", "truncate", "entry-boundary-truncate", "crc", "sequence", "header", "renew", "offline", "old-cloud"} {
		t.Run(scenario, func(t *testing.T) {
			cloud, c, v := newRecoveryCloud(t)
			seedRecoveryCheckpoint(t, c, v)
			data := bytes.Repeat([]byte{0xD4}, 4096)
			entry := lbd.Entry{Op: "W", Sequence: 1, Block: 2, Length: 4096, Data: data, Checksum: crc32.ChecksumIEEE(data)}
			cloud.addTail(t, []lbd.Entry{entry})
			id := cloud.addTail(t, []lbd.Entry{entry})
			v.DiskPath = streamDir(t)
			sentinel := bytes.Repeat([]byte{0xE5}, 64*1024)
			require.NoError(t, os.WriteFile(filepath.Join(v.DiskPath, "disk.img"), sentinel, 0600))
			require.NoError(t, writeLogHorizon(v.DiskPath, "old-local-horizon"))
			switch scenario {
			case "empty":
				cloud.manifest.Segments = nil
				cloud.manifest.SegmentCount = 0
			case "middle":
				cloud.manifest.Segments = cloud.manifest.Segments[1:]
				cloud.manifest.SegmentCount--
			case "suffix":
				cloud.manifest.Segments = cloud.manifest.Segments[:1]
				cloud.manifest.SegmentCount--
			case "truncated-manifest":
				cloud.manifest.Complete = false
			case "floor":
				cloud.manifest.CoverageFloor = cloud.manifest.Head
			case "expired":
				cloud.manifest.Status = "expired"
			case "stream-reset":
				require.NoError(t, writeRecoveryFile(v.DiskPath, acceleratorStreamFile, []byte("other-stream")))
			case "corrupt", "truncate":
				cloud.mockCloudServer.mu.Lock()
				if scenario == "corrupt" {
					cloud.segments[id].Data[0] ^= 0xff
				} else {
					cloud.segments[id].Data = cloud.segments[id].Data[:10]
				}
				cloud.mockCloudServer.mu.Unlock()
			case "entry-boundary-truncate", "crc", "sequence", "header":
				entries := []lbd.Entry{entry}
				label := cloud.manifest.Segments[1].Label
				switch scenario {
				case "entry-boundary-truncate":
					entries = nil
				case "crc":
					entries[0].Checksum++
				case "sequence":
					entries[0].Sequence++
				default:
					label = "wrong-header-label"
				}
				payload := buildSegmentData(t, 4096, label, entries)
				cloud.segments[id].Data = payload
				// Even a valid transport digest must not bypass LBD validation.
				cloud.manifest.Segments[1].RecoveryObject = recoveryObject(id, payload)
			case "renew":
				cloud.renewFail = true
			case "offline":
				cloud.server.Close()
			case "old-cloud":
				cloud.oldCloud = true
			}
			require.Error(t, c.recoverAccelerator(t.Context(), v, "nonce"))
			actual, err := os.ReadFile(filepath.Join(v.DiskPath, "disk.img"))
			require.NoError(t, err)
			require.Equal(t, sentinel, actual, "invalid coverage must not replace the local image")
			horizon, err := readLogHorizon(v.DiskPath)
			require.NoError(t, err)
			require.Equal(t, "old-local-horizon", horizon)
		})
	}
}

func TestAcceleratorCheckpointPublicationRetry(t *testing.T) {
	for _, failure := range []string{"upload", "publication", "lost-response", "missing-ack", "wrong-ack"} {
		t.Run(failure, func(t *testing.T) {
			cloud, c, v := newRecoveryCloud(t)
			require.NoError(t, os.WriteFile(filepath.Join(v.DiskPath, "disk.img"), make([]byte, 1024*1024), 0600))
			require.NoError(t, writeRecoveryFile(v.DiskPath, acceleratorStreamFile, []byte("stream")))
			cloud.uploadFail = failure == "upload"
			cloud.publicationFail = failure == "publication"
			cloud.losePublication = failure == "lost-response"
			cloud.checkpointFault = failure
			require.Error(t, c.publishAcceleratorCheckpoint(t.Context(), v, "nonce"))
			cloud.mu.Lock()
			require.Len(t, cloud.claims, 1)
			var originalID string
			for id := range cloud.claims {
				originalID = id
			}
			cloud.mu.Unlock()
			staged, err := os.ReadFile(filepath.Join(v.DiskPath, checkpointPayloadFile))
			require.NoError(t, err)
			// Process restart resumes the same staged bytes under the old lease.
			state, err := LoadState(v.DiskPath)
			require.NoError(t, err)
			c.state = state
			cloud.mu.Lock()
			cloud.uploadFail, cloud.publicationFail, cloud.losePublication = false, false, false
			cloud.checkpointFault = ""
			cloud.mu.Unlock()
			require.NoError(t, c.publishAcceleratorCheckpoint(t.Context(), v, "nonce"))
			require.Equal(t, recoveryObject("", staged).SHA256, cloud.manifest.Checkpoint.SHA256)
			require.Equal(t, originalID, cloud.manifest.Checkpoint.UpdateID, "retry must reuse the checksum-bound update")
			require.Len(t, cloud.claims, 1)
			_, err = os.Stat(filepath.Join(v.DiskPath, checkpointIntentFile))
			require.True(t, os.IsNotExist(err))
		})
	}
}

func TestAcceleratorRestoreProtectionDuringDownload(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("renewal-fails-%t", fail), func(t *testing.T) {
			cloud, c, v := newRecoveryCloud(t)
			expected := seedRecoveryCheckpoint(t, c, v)
			cloud.manifest.ExpiresAt = time.Now().Add(90 * time.Millisecond)
			cloud.renewFail = fail
			cloud.downloadDelay = 200 * time.Millisecond
			v.DiskPath = streamDir(t)
			err := c.recoverAccelerator(t.Context(), v, "nonce")
			if fail {
				require.Error(t, err)
				require.NoFileExists(t, filepath.Join(v.DiskPath, "disk.img"))
			} else {
				require.NoError(t, err)
				actual, err := os.ReadFile(filepath.Join(v.DiskPath, "disk.img"))
				require.NoError(t, err)
				require.Equal(t, expected, actual)
				// The session expires mid-download, so only background renewal succeeds.
				cloud.mu.Lock()
				require.Positive(t, cloud.renewals)
				cloud.mu.Unlock()
			}
		})
	}
}

func TestAcceleratorInterruptedInstallRebuilds(t *testing.T) {
	cloud, c, v := newRecoveryCloud(t)
	expected := seedRecoveryCheckpoint(t, c, v)
	v.DiskPath = streamDir(t)
	// Fail after image rename but before horizon installation.
	require.NoError(t, os.Mkdir(filepath.Join(v.DiskPath, logHorizonFile+".tmp"), 0700))
	require.Error(t, c.recoverAccelerator(t.Context(), v, "nonce"))
	require.FileExists(t, filepath.Join(v.DiskPath, acceleratorInstallFile))
	require.NoError(t, os.Remove(filepath.Join(v.DiskPath, logHorizonFile+".tmp")))
	require.NoError(t, c.recoverAccelerator(t.Context(), v, "nonce"))
	actual, err := os.ReadFile(filepath.Join(v.DiskPath, "disk.img"))
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	horizon, err := readLogHorizon(v.DiskPath)
	require.NoError(t, err)
	require.Equal(t, cloud.manifest.Head, horizon)
	require.NoFileExists(t, filepath.Join(v.DiskPath, acceleratorInstallFile))
}

func TestAcceleratorStaleCheckpointIntentIsRestaged(t *testing.T) {
	cloud, c, v := newRecoveryCloud(t)
	seedRecoveryCheckpoint(t, c, v)
	advance := func(label string) {
		segment := buildSegmentData(t, 4096, label, nil)
		_, err := c.updatesClient.Upload(t.Context(), "vol", UploadRequest{Kind: KindLBDLog, OrderingKey: label}, bytes.NewReader(segment), int64(len(segment)))
		require.NoError(t, err)
		require.NoError(t, writeLogHorizon(v.DiskPath, label))
	}
	advance("400000000000000000000002")
	cloud.mu.Lock()
	cloud.uploadFail = true
	cloud.mu.Unlock()
	require.Error(t, c.publishAcceleratorCheckpoint(t.Context(), v, "nonce"))
	require.FileExists(t, filepath.Join(v.DiskPath, checkpointIntentFile))
	// The log advanced past the staged boundary before the retry.
	const next = "400000000000000000000003"
	advance(next)
	cloud.mu.Lock()
	cloud.uploadFail = false
	cloud.mu.Unlock()
	require.NoError(t, c.publishAcceleratorCheckpoint(t.Context(), v, "nonce"))
	cloud.mu.Lock()
	require.Equal(t, next, cloud.manifest.Checkpoint.ThroughLabel)
	cloud.mu.Unlock()
}

func TestAcceleratorManifestBeyondTenThousand(t *testing.T) {
	cloud, c, v := newRecoveryCloud(t)
	seedRecoveryCheckpoint(t, c, v)
	for range 10001 {
		cloud.addTail(t, nil)
	}
	v.DiskPath = streamDir(t)
	require.NoError(t, c.recoverAccelerator(t.Context(), v, "nonce"))
	horizon, err := readLogHorizon(v.DiskPath)
	require.NoError(t, err)
	require.Equal(t, cloud.manifest.Head, horizon)
}

func TestAcceleratorRecoveryRejectsUnboundPages(t *testing.T) {
	for _, fault := range []string{"legacy-list", "changed-head", "wrong-session", "missing-metadata", "early-eof", "repeated-cursor"} {
		t.Run(fault, func(t *testing.T) {
			cloud, c, v := newRecoveryCloud(t)
			seedRecoveryCheckpoint(t, c, v)
			for range 201 {
				cloud.addTail(t, nil)
			}
			cloud.pageFault = fault
			v.DiskPath = streamDir(t)
			require.Error(t, c.recoverAccelerator(t.Context(), v, "nonce"))
			require.NoFileExists(t, filepath.Join(v.DiskPath, "disk.img"))
			require.NoFileExists(t, filepath.Join(v.DiskPath, logHorizonFile))
		})
	}
}

func TestAcceleratorFreshFilesystemAndDatabase(t *testing.T) {
	for _, tool := range []string{"mkfs.ext4", "debugfs", "e2fsck"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s required; run through hack/it", tool)
		}
	}
	run := func(name string, args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(t.Context(), name, args...).CombinedOutput()
		require.NoError(t, err, "%s: %s", name, out)
		return string(out)
	}
	cloud, c, v := newRecoveryCloud(t)
	v.SizeBytes = 16 * 1024 * 1024
	cloud.manifest.DeviceSize = uint64(v.SizeBytes)
	database := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", database)
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE records (id INTEGER PRIMARY KEY, value TEXT); INSERT INTO records VALUES (1, 'retired-only'), (2, 'before-boundary'), (3, 'deleted-after-boundary')")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	imagePath := filepath.Join(v.DiskPath, "disk.img")
	image, err := os.Create(imagePath)
	require.NoError(t, err)
	require.NoError(t, image.Truncate(v.SizeBytes))
	require.NoError(t, image.Close())
	run("mkfs.ext4", "-q", "-F", imagePath)
	run("debugfs", "-w", "-R", "write "+database+" /state.db", imagePath)
	before, err := os.ReadFile(imagePath)
	require.NoError(t, err)
	require.NoError(t, writeRecoveryFile(v.DiskPath, acceleratorStreamFile, []byte("stream")))
	require.NoError(t, writeLogHorizon(v.DiskPath, "400000000000000000000001"))
	// Upload the actual prefix that created the filesystem/database, then
	// checkpoint it and physically remove its only log object below.
	var prefix bytes.Buffer
	writer, err := lbd.NewWriter(&prefix, lbd.Header{Version: 2, BlockSize: 4096, DeviceSize: uint64(v.SizeBytes), SegmentLabel: "400000000000000000000001"})
	require.NoError(t, err)
	for offset := 0; offset < len(before); offset += 4096 {
		entry := makeWriteEntry(uint64(offset/4096), before[offset:offset+4096])
		entry.Sequence = uint64(offset / 4096)
		require.NoError(t, writer.WriteEntry(&entry))
	}
	prefixID, err := c.updatesClient.Upload(t.Context(), "vol", UploadRequest{Kind: KindLBDLog, OrderingKey: "400000000000000000000001"}, bytes.NewReader(prefix.Bytes()), int64(prefix.Len()))
	require.NoError(t, err)
	require.NoError(t, c.publishAcceleratorCheckpoint(t.Context(), v, "nonce"))
	db, err = sql.Open("sqlite", database)
	require.NoError(t, err)
	_, err = db.Exec("UPDATE records SET value = 'after-boundary' WHERE id = 2; DELETE FROM records WHERE id = 3; INSERT INTO records VALUES (4, 'tail-only')")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	run("debugfs", "-w", "-R", "rm /state.db", imagePath)
	run("debugfs", "-w", "-R", "write "+database+" /state.db", imagePath)
	after, err := os.ReadFile(imagePath)
	require.NoError(t, err)
	var tail []lbd.Entry
	for offset := 0; offset < len(after); offset += 4096 {
		if !bytes.Equal(before[offset:offset+4096], after[offset:offset+4096]) {
			entry := makeWriteEntry(uint64(offset/4096), after[offset:offset+4096])
			entry.Sequence = uint64(len(tail))
			tail = append(tail, entry)
		}
	}
	require.NotEmpty(t, tail)
	cloud.addTail(t, tail)
	cloud.mockCloudServer.mu.Lock()
	delete(cloud.segments, prefixID)
	cloud.mockCloudServer.mu.Unlock()
	// A different directory has neither an image, a horizon, nor stream state.
	v.DiskPath = streamDir(t)
	require.NoError(t, c.recoverAccelerator(t.Context(), v, "nonce"))
	restored := filepath.Join(v.DiskPath, "disk.img")
	actual, err := os.ReadFile(restored)
	require.NoError(t, err)
	require.Equal(t, sha256.Sum256(after), sha256.Sum256(actual))
	fsck := run("e2fsck", "-fn", restored)
	t.Logf("fresh filesystem integrity: %s", strings.TrimSpace(fsck))
	extracted := filepath.Join(v.DiskPath, "restored.db")
	run("debugfs", "-R", "dump /state.db "+extracted, restored)
	db, err = sql.Open("sqlite", extracted)
	require.NoError(t, err)
	defer db.Close()
	var integrity string
	require.NoError(t, db.QueryRow("PRAGMA integrity_check").Scan(&integrity))
	require.Equal(t, "ok", integrity)
	var contents string
	require.NoError(t, db.QueryRow("SELECT group_concat(id || ':' || value, ',') FROM (SELECT * FROM records ORDER BY id)").Scan(&contents))
	require.Equal(t, "1:retired-only,2:after-boundary,4:tail-only", contents)
	t.Logf("fresh database integrity=%s contents=%s; covered prefix object removed", integrity, contents)
}

// streamDir models a node that has already established the volume's stream.
func streamDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, writeRecoveryFile(dir, acceleratorStreamFile, []byte("stream")))
	return dir
}

// mountAndDetach drives the public reconcile path through one attachment.
func mountAndDetach(t *testing.T, c *DiskMountController, v *VolumeState) {
	t.Helper()
	require.NoError(t, c.state.SetVolumeAndSave(v.EntityId, v))
	mount := &storage_v1alpha.DiskMount{ID: "mount", VolumeId: "volume", MountPath: filepath.Join(v.DiskPath, "mnt"),
		DesiredState: storage_v1alpha.DM_WANT_MOUNTED, ActualState: storage_v1alpha.DM_PENDING}
	require.NoError(t, c.Reconcile(t.Context(), mount, nil))
	require.True(t, c.state.GetMount("mount").Mounted)
	mount.DesiredState, mount.ActualState = storage_v1alpha.DM_WANT_UNMOUNTED, storage_v1alpha.DM_MOUNTED
	require.NoError(t, c.Reconcile(t.Context(), mount, nil))
	require.Nil(t, c.state.GetMount("mount"))
	require.False(t, c.state.HasPendingCloudUpload("vol"), "a checkpoint must never gate later attaches")
}

func checkpointClaims(cloud *recoveryCloud) int {
	cloud.mockCloudServer.mu.Lock()
	defer cloud.mockCloudServer.mu.Unlock()
	n := 0
	for _, object := range cloud.segments {
		if object.Kind == string(KindLBDCheckpoint) {
			n++
		}
	}
	return n
}

func TestAcceleratorMountAndDetachWithoutV1Cloud(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream-%t", stream), func(t *testing.T) {
			cloud, c, v := newRecoveryCloud(t)
			cloud.oldCloud = true
			v.DiskPath = t.TempDir()
			if stream {
				v.DiskPath = streamDir(t)
			}
			require.NoError(t, os.WriteFile(filepath.Join(v.DiskPath, "disk.img"), make([]byte, 1024*1024), 0600))
			data := bytes.Repeat([]byte{0x5A}, 4096)
			const label = "400000000000000000000001"
			segment := buildSegmentData(t, 4096, label, []lbd.Entry{makeWriteEntry(2, data)})
			_, err := c.updatesClient.Upload(t.Context(), "vol", UploadRequest{Kind: KindLBDLog, OrderingKey: label}, bytes.NewReader(segment), int64(len(segment)))
			require.NoError(t, err)

			mountAndDetach(t, c, v)
			image, err := os.ReadFile(filepath.Join(v.DiskPath, "disk.img"))
			require.NoError(t, err)
			require.Equal(t, data, image[2*4096:3*4096], "legacy replay must apply the cloud log")
			horizon, err := readLogHorizon(v.DiskPath)
			require.NoError(t, err)
			require.Equal(t, label, horizon)
			cloud.mockCloudServer.mu.Lock()
			require.Empty(t, cloud.leases, "an unsupported checkpoint must not hold the lease")
			cloud.mockCloudServer.mu.Unlock()
			require.True(t, c.checkpointsUnsupported.Load())
			require.NoFileExists(t, filepath.Join(v.DiskPath, checkpointIntentFile))
			require.NoFileExists(t, filepath.Join(v.DiskPath, checkpointPayloadFile))
			if stream {
				require.FileExists(t, filepath.Join(v.DiskPath, acceleratorStreamFile))
			} else {
				require.NoFileExists(t, filepath.Join(v.DiskPath, acceleratorStreamFile), "an old cloud must not start a stream")
			}
			// A second cycle neither fails nor stages another doomed checkpoint.
			mountAndDetach(t, c, v)
			require.NoFileExists(t, filepath.Join(v.DiskPath, checkpointPayloadFile))
		})
	}
}

func TestAcceleratorLegacyVolumeMigratesAtDetach(t *testing.T) {
	cloud, c, v := newRecoveryCloud(t)
	v.DiskPath = t.TempDir()
	expected := make([]byte, 1024*1024)
	copy(expected[4096:], bytes.Repeat([]byte{0x3C}, 4096))
	require.NoError(t, os.WriteFile(filepath.Join(v.DiskPath, "disk.img"), make([]byte, len(expected)), 0600))
	const label = "400000000000000000000001"
	segment := buildSegmentData(t, 4096, label, []lbd.Entry{makeWriteEntry(1, expected[4096:8192])})
	_, err := c.updatesClient.Upload(t.Context(), "vol", UploadRequest{Kind: KindLBDLog, OrderingKey: label}, bytes.NewReader(segment), int64(len(segment)))
	require.NoError(t, err)

	// Legacy volume on a v1 cloud: legacy replay on mount, first checkpoint at detach.
	mountAndDetach(t, c, v)
	stream, err := os.ReadFile(filepath.Join(v.DiskPath, acceleratorStreamFile))
	require.NoError(t, err)
	cloud.mu.Lock()
	require.Equal(t, string(stream), cloud.manifest.StreamID, "the acknowledged checkpoint starts the stream")
	require.Equal(t, label, cloud.manifest.Checkpoint.ThroughLabel)
	cloud.mu.Unlock()
	require.Equal(t, 1, checkpointClaims(cloud))

	// Now a stream volume: mount rebuilds from the checkpoint, and an
	// unchanged detach does not upload the same image again.
	require.NoError(t, os.WriteFile(filepath.Join(v.DiskPath, "disk.img"), make([]byte, len(expected)), 0600))
	mountAndDetach(t, c, v)
	actual, err := os.ReadFile(filepath.Join(v.DiskPath, "disk.img"))
	require.NoError(t, err)
	require.Equal(t, expected, actual, "protected recovery must rebuild from the checkpoint")
	require.Equal(t, 1, checkpointClaims(cloud))
}

func TestAcceleratorDetachDoesNotWaitForCheckpoint(t *testing.T) {
	cloud, c, v := newRecoveryCloud(t)
	seedRecoveryCheckpoint(t, c, v)
	imagePath := filepath.Join(v.DiskPath, "disk.img")
	logDir := filepath.Join(v.DiskPath, "logs")
	require.NoError(t, os.Mkdir(logDir, 0700))
	const finalLabel = "400000000000000000000002"
	data := bytes.Repeat([]byte{0xF6}, 4096)
	segment := buildSegmentData(t, 4096, finalLabel, []lbd.Entry{makeWriteEntry(3, data)})
	require.NoError(t, os.WriteFile(filepath.Join(logDir, "disk."+finalLabel+".log"), segment, 0600))
	c.ops.(*mockDiskMountOps).lbdBackings["/dev/lbd0"] = imagePath
	require.NoError(t, c.state.SetMountAndSave("mount", &MountState{
		EntityId: "mount", VolumeId: "volume", CloudVolumeId: "vol", Mode: storage_v1alpha.VM_ACCELERATOR,
		BackingPath: imagePath, DevicePath: "/dev/lbd0", LeaseNonce: "nonce", LeaseOwned: true,
	}))
	cloud.mockCloudServer.mu.Lock()
	cloud.leases["vol"] = &activeLease{Nonce: "nonce"}
	cloud.mockCloudServer.mu.Unlock()
	cloud.mu.Lock()
	cloud.uploadFail = true
	cloud.mu.Unlock()
	mount := &storage_v1alpha.DiskMount{ID: "mount", VolumeId: "volume", DesiredState: storage_v1alpha.DM_WANT_UNMOUNTED,
		ActualState: storage_v1alpha.DM_MOUNTED}
	require.NoError(t, c.Reconcile(t.Context(), mount, nil))
	require.Nil(t, c.state.GetMount("mount"))
	require.False(t, c.state.HasPendingCloudUpload("vol"))
	horizon, err := readLogHorizon(v.DiskPath)
	require.NoError(t, err)
	require.Equal(t, finalLabel, horizon, "the final log is drained even when the checkpoint fails")
	cloud.mockCloudServer.mu.Lock()
	require.Empty(t, cloud.leases)
	cloud.mockCloudServer.mu.Unlock()
	cloud.mu.Lock()
	require.Equal(t, "400000000000000000000001", cloud.manifest.Checkpoint.ThroughLabel)
	cloud.mu.Unlock()
	require.FileExists(t, filepath.Join(v.DiskPath, checkpointIntentFile), "the staged checkpoint is kept for the next detach")
}

func TestAcceleratorInstallSurvivesInterruptedBackgroundRenewal(t *testing.T) {
	cloud, c, v := newRecoveryCloud(t)
	expected := seedRecoveryCheckpoint(t, c, v)
	// The background renewal starts at a third of the expiry and is still in
	// flight when the install, which renews on its own, commits.
	cloud.manifest.ExpiresAt = time.Now().Add(600 * time.Millisecond)
	cloud.stallRenewal = true
	cloud.downloadDelay = 300 * time.Millisecond
	v.DiskPath = streamDir(t)
	require.NoError(t, c.recoverAccelerator(t.Context(), v, "nonce"))
	actual, err := os.ReadFile(filepath.Join(v.DiskPath, "disk.img"))
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	cloud.mu.Lock()
	require.False(t, cloud.stallRenewal, "the background renewal must have been in flight")
	cloud.mu.Unlock()
}

func TestAcceleratorCheckpointSkippedUnderShortDeadline(t *testing.T) {
	cloud, c, v := newRecoveryCloud(t)
	seedRecoveryCheckpoint(t, c, v)
	segment := buildSegmentData(t, 4096, "400000000000000000000002", nil)
	_, err := c.updatesClient.Upload(t.Context(), "vol", UploadRequest{Kind: KindLBDLog, OrderingKey: "400000000000000000000002"}, bytes.NewReader(segment), int64(len(segment)))
	require.NoError(t, err)
	require.NoError(t, writeLogHorizon(v.DiskPath, "400000000000000000000002"))
	mount := &MountState{EntityId: "mount", VolumeId: "volume", CloudVolumeId: "vol", Mode: storage_v1alpha.VM_ACCELERATOR,
		BackingPath: filepath.Join(v.DiskPath, "disk.img"), LeaseNonce: "nonce", LeaseOwned: true}
	// Shutdown releases leases under a 30s context; checkpointing must not spend it.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	c.checkpointDetachedMount(ctx, mount)
	require.Equal(t, 1, checkpointClaims(cloud))
	c.checkpointDetachedMount(t.Context(), mount)
	require.Equal(t, 2, checkpointClaims(cloud))
}

func TestAcceleratorFailedInstallReportsItsOwnError(t *testing.T) {
	cloud, c, v := newRecoveryCloud(t)
	seedRecoveryCheckpoint(t, c, v)
	id := cloud.addTail(t, nil)
	cloud.mockCloudServer.mu.Lock()
	cloud.segments[id].Data[0] ^= 0xff
	cloud.mockCloudServer.mu.Unlock()
	// A background renewal is in flight (from 300ms) when the corrupt
	// segment fails the install (about 600ms), well before expiry.
	cloud.manifest.ExpiresAt = time.Now().Add(900 * time.Millisecond)
	cloud.stallRenewal = true
	cloud.downloadDelay = 300 * time.Millisecond
	v.DiskPath = streamDir(t)
	err := c.recoverAccelerator(t.Context(), v, "nonce")
	require.ErrorContains(t, err, "digest mismatch")
	cloud.mu.Lock()
	require.False(t, cloud.stallRenewal, "the background renewal must have been in flight")
	cloud.mu.Unlock()
}

func TestAcceleratorNoCheckpointWithoutLbdDetach(t *testing.T) {
	cloud, c, v := newRecoveryCloud(t)
	v.DiskPath = t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(v.DiskPath, "disk.img"), make([]byte, 1024*1024), 0600))
	const label = "400000000000000000000001"
	segment := buildSegmentData(t, 4096, label, []lbd.Entry{makeWriteEntry(1, bytes.Repeat([]byte{0x7E}, 4096))})
	_, err := c.updatesClient.Upload(t.Context(), "vol", UploadRequest{Kind: KindLBDLog, OrderingKey: label}, bytes.NewReader(segment), int64(len(segment)))
	require.NoError(t, err)
	require.NoError(t, c.state.SetVolumeAndSave(v.EntityId, v))
	mount := &storage_v1alpha.DiskMount{ID: "mount", VolumeId: "volume", MountPath: filepath.Join(v.DiskPath, "mnt"),
		DesiredState: storage_v1alpha.DM_WANT_MOUNTED, ActualState: storage_v1alpha.DM_PENDING}
	require.NoError(t, c.Reconcile(t.Context(), mount, nil))
	// Even a clean-detach flag left from an earlier attachment proves nothing
	// about an image whose device was still recorded when it vanished.
	ms := c.state.GetMount("mount")
	ms.CheckpointDue = true
	require.NoError(t, c.state.SetMountAndSave("mount", ms))
	// A reboot: the kernel device is gone, so cleanup never calls LbdDetach.
	ops := c.ops.(*mockDiskMountOps)
	clear(ops.lbdBackings)
	clear(ops.mountDevices)
	mount.DesiredState, mount.ActualState = storage_v1alpha.DM_WANT_UNMOUNTED, storage_v1alpha.DM_MOUNTED
	require.NoError(t, c.Reconcile(t.Context(), mount, nil))
	require.Nil(t, c.state.GetMount("mount"))
	require.Zero(t, checkpointClaims(cloud), "a post-crash image is not a checkpoint")
	require.NoFileExists(t, filepath.Join(v.DiskPath, acceleratorStreamFile))
	cloud.mockCloudServer.mu.Lock()
	require.Empty(t, cloud.leases)
	cloud.mockCloudServer.mu.Unlock()
}

func TestAcceleratorStreamConflictStopsCheckpoints(t *testing.T) {
	// A current cloud refuses the stream at begin; an earlier one only at
	// completion, after the image is uploaded. Both must stop checkpoints.
	for _, refusedAt := range []string{"begin", "complete"} {
		t.Run(refusedAt, func(t *testing.T) {
			cloud, c, v := newRecoveryCloud(t)
			cloud.completeOnlyStreamCheck = refusedAt == "complete"
			seedRecoveryCheckpoint(t, c, v)
			require.Equal(t, 1, checkpointClaims(cloud))
			// A legacy copy of the same volume, which has applied history of
			// its own, proposes its own stream. (A copy with nothing applied
			// adopts the cloud's stream instead.)
			v.DiskPath = t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(v.DiskPath, "disk.img"), make([]byte, 1024*1024), 0600))
			require.NoError(t, writeLogHorizon(v.DiskPath, "400000000000000000000001"))
			mountAndDetach(t, c, v)
			uploaded := 1
			if refusedAt == "complete" {
				uploaded = 2
			}
			require.Equal(t, uploaded, checkpointClaims(cloud), "a begin refusal sends no bytes")
			require.Equal(t, 2, checkpointBegins(cloud))
			require.FileExists(t, filepath.Join(v.DiskPath, checkpointConflictFile))
			require.NoFileExists(t, filepath.Join(v.DiskPath, checkpointIntentFile))
			require.NoFileExists(t, filepath.Join(v.DiskPath, checkpointPayloadFile))

			// New writes would restage a new image. After a restart, the next
			// detach must not even ask the cloud again.
			const label = "400000000000000000000002"
			segment := buildSegmentData(t, 4096, label, []lbd.Entry{makeWriteEntry(2, bytes.Repeat([]byte{0x4D}, 4096))})
			_, err := c.updatesClient.Upload(t.Context(), "vol", UploadRequest{Kind: KindLBDLog, OrderingKey: label}, bytes.NewReader(segment), int64(len(segment)))
			require.NoError(t, err)
			restarted := NewDiskMountController(slog.Default(), c.dataPath, compute.NewNodeId("fresh-node"), c.state, newMockDiskMountOps())
			restarted.SetUpdatesClient(c.updatesClient)
			restarted.SetCloudClient(c.cloudClient)
			mountAndDetach(t, restarted, v)
			horizon, err := readLogHorizon(v.DiskPath)
			require.NoError(t, err)
			require.Equal(t, label, horizon)
			require.Equal(t, uploaded, checkpointClaims(cloud), "a refused stream must not upload again")
			require.Equal(t, 2, checkpointBegins(cloud), "a refused stream must not be staged or claimed again")
			cloud.mockCloudServer.mu.Lock()
			require.Empty(t, cloud.leases)
			cloud.mockCloudServer.mu.Unlock()
		})
	}
}

func checkpointBegins(cloud *recoveryCloud) int {
	cloud.mu.Lock()
	defer cloud.mu.Unlock()
	return cloud.begins
}

func TestAcceleratorStalledCheckpointReleasesLease(t *testing.T) {
	cloud, c, v := newRecoveryCloud(t)
	v.DiskPath = t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(v.DiskPath, "disk.img"), make([]byte, 1024*1024), 0600))
	const label = "400000000000000000000001"
	segment := buildSegmentData(t, 4096, label, []lbd.Entry{makeWriteEntry(1, bytes.Repeat([]byte{0x2B}, 4096))})
	_, err := c.updatesClient.Upload(t.Context(), "vol", UploadRequest{Kind: KindLBDLog, OrderingKey: label}, bytes.NewReader(segment), int64(len(segment)))
	require.NoError(t, err)
	cloud.stallUpload = true
	c.SetCheckpointTimeout(500 * time.Millisecond)
	started := time.Now()
	mountAndDetach(t, c, v)
	require.Less(t, time.Since(started), 10*time.Second, "the lease must not wait on a stalled checkpoint")
	cloud.mockCloudServer.mu.Lock()
	require.Empty(t, cloud.leases)
	cloud.mockCloudServer.mu.Unlock()
	require.NoFileExists(t, filepath.Join(v.DiskPath, checkpointPayloadFile), "abandoned bytes are discarded")
	require.NoFileExists(t, filepath.Join(v.DiskPath, acceleratorStreamFile))

	// The kept intent retries the same proposed stream once the cloud recovers.
	intent, err := os.ReadFile(filepath.Join(v.DiskPath, checkpointIntentFile))
	require.NoError(t, err)
	var staged checkpointIntent
	require.NoError(t, json.Unmarshal(intent, &staged))
	// A restage that fails (here, under an already-expired deadline) must
	// not drop the proposed stream along with it.
	expired, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, c.publishAcceleratorCheckpoint(expired, v, "nonce"), context.Canceled)
	require.FileExists(t, filepath.Join(v.DiskPath, checkpointIntentFile))
	cloud.mu.Lock()
	cloud.stallUpload = false
	cloud.mu.Unlock()
	mountAndDetach(t, c, v)
	stream, err := os.ReadFile(filepath.Join(v.DiskPath, acceleratorStreamFile))
	require.NoError(t, err)
	require.Equal(t, staged.Publication.StreamID, string(stream))
}

func TestAcceleratorCheckpointRetryUnderNewLease(t *testing.T) {
	cloud, c, v := newRecoveryCloud(t)
	seedRecoveryCheckpoint(t, c, v)
	const label = "400000000000000000000002"
	segment := buildSegmentData(t, 4096, label, []lbd.Entry{makeWriteEntry(3, bytes.Repeat([]byte{0x61}, 4096))})
	_, err := c.updatesClient.Upload(t.Context(), "vol", UploadRequest{Kind: KindLBDLog, OrderingKey: label}, bytes.NewReader(segment), int64(len(segment)))
	require.NoError(t, err)
	require.NoError(t, writeLogHorizon(v.DiskPath, label))
	cloud.mu.Lock()
	cloud.uploadFail = true
	cloud.mu.Unlock()
	require.Error(t, c.publishAcceleratorCheckpoint(t.Context(), v, "first-lease"))
	cloud.mu.Lock()
	cloud.uploadFail = false
	cloud.mu.Unlock()
	// The next detach holds a different lease and re-begins the same key.
	require.NoError(t, c.publishAcceleratorCheckpoint(t.Context(), v, "second-lease"))
	require.NoFileExists(t, filepath.Join(v.DiskPath, checkpointConflictFile), "a cross-lease retry is not a stream conflict")
	require.Equal(t, 2, checkpointClaims(cloud), "the retry resumes the same update")
	cloud.mu.Lock()
	require.Equal(t, label, cloud.manifest.Checkpoint.ThroughLabel)
	cloud.mu.Unlock()
}

// A new volume restored from another volume's history as of a point in time:
// a segment opened before the time can hold writes after it, which must not
// appear, and the result must become the new volume's own first checkpoint so
// its later history never replays onto an empty image.
func TestAcceleratorRewindSeedsNewVolumeAtPointInTime(t *testing.T) {
	cloud, c, v := newRecoveryCloud(t)
	expected := seedRecoveryCheckpoint(t, c, v)
	entry := func(ts, seq, block uint64, fill byte) lbd.Entry {
		data := bytes.Repeat([]byte{fill}, 4096)
		return lbd.Entry{Op: "W", TimestampNS: ts, Sequence: seq, Block: block, Length: 4096, Data: data, Checksum: crc32.ChecksumIEEE(data)}
	}
	// Labels are 2^62 + Unix seconds plus nanoseconds, so label ...02 is 2ns.
	cloud.addTail(t, []lbd.Entry{entry(2, 41, 3, 0x11), entry(3, 42, 4, 0x22)})
	cloud.addTail(t, []lbd.Entry{entry(4, 43, 5, 0x33)})
	copy(expected[3*4096:], bytes.Repeat([]byte{0x11}, 4096))

	// That history now belongs to the source volume; "vol" is brand new.
	cloud.mu.Lock()
	source := cloud.manifest
	source.VolumeID = "src"
	cloud.source = &source
	cloud.manifest = RecoveryManifest{Version: 1, VolumeID: "vol", Status: "never_backed_up", SessionID: "session",
		ExpiresAt: time.Now().Add(time.Minute), Complete: true, DeviceSize: 1024 * 1024}
	seeded := len(cloud.publications)
	cloud.mockCloudServer.mu.Lock()
	for _, object := range cloud.segments {
		object.VolumeID = "src"
	}
	cloud.mockCloudServer.mu.Unlock()
	cloud.mu.Unlock()

	v.DiskPath = t.TempDir()
	v.RestoreFrom, v.RestoreUntil = "src", "400000000000000000000002"
	require.NoError(t, c.state.SetVolumeAndSave(v.EntityId, v))
	// A failed earlier attempt left a staged checkpoint of another image, at
	// the same empty boundary. It must not be published over this rebuild.
	require.NoError(t, os.WriteFile(filepath.Join(v.DiskPath, "disk.img"), bytes.Repeat([]byte{0x99}, 1024*1024), 0600))
	_, err := c.stageCheckpoint(t.Context(), v, "proposed", "", filepath.Join(v.DiskPath, checkpointPayloadFile))
	require.NoError(t, err)
	mount := &storage_v1alpha.DiskMount{ID: "mount", VolumeId: "volume", MountPath: filepath.Join(v.DiskPath, "mnt"),
		DesiredState: storage_v1alpha.DM_WANT_MOUNTED, ActualState: storage_v1alpha.DM_PENDING}
	require.NoError(t, c.Reconcile(t.Context(), mount, nil))
	actual, err := os.ReadFile(filepath.Join(v.DiskPath, "disk.img"))
	require.NoError(t, err)
	require.Equal(t, expected, actual)

	// Seeded before anything attaches, not at a detach that may never come.
	require.Len(t, cloud.publications, seeded+1)
	seed := cloud.publications[seeded]
	require.Equal(t, "vol", seed.VolumeID)
	require.Empty(t, seed.Base.ThroughLabel, "the seed precedes all of the new volume's log")
	require.Equal(t, "proposed", seed.StreamID, "a retry keeps the proposed stream")
	digest := sha256.Sum256(expected)
	require.Equal(t, hex.EncodeToString(digest[:]), seed.Base.ImageSHA256)

	mount.DesiredState, mount.ActualState = storage_v1alpha.DM_WANT_UNMOUNTED, storage_v1alpha.DM_MOUNTED
	require.NoError(t, c.Reconcile(t.Context(), mount, nil))

	// Once seeded, the volume recovers from its own history, never the source
	// again: a second rewind would propose another stream and be refused.
	v.DiskPath = t.TempDir()
	mountAndDetach(t, c, v)
	actual, err = os.ReadFile(filepath.Join(v.DiskPath, "disk.img"))
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	stream, err := os.ReadFile(filepath.Join(v.DiskPath, acceleratorStreamFile))
	require.NoError(t, err)
	require.Equal(t, seed.StreamID, string(stream))
}

// An adopted volume marked for takeover acquires its lease through the
// takeover route, carrying the operator's decision time, so a writer that
// died holding the lease no longer blocks the mount.
func TestAcceleratorMountTakesOverLeaseWhenMarked(t *testing.T) {
	cloud, c, v := newRecoveryCloud(t)
	v.DiskPath = t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(v.DiskPath, "disk.img"), make([]byte, v.SizeBytes), 0600))
	decided := time.Date(2026, 11, 7, 6, 0, 0, 123, time.UTC)
	v.TakeOverBefore = decided.Format(time.RFC3339Nano)
	mountAndDetach(t, c, v)
	require.Len(t, cloud.takeovers, 1)
	require.True(t, decided.Equal(cloud.takeovers[0]))
}

// A disk that stays attached gets hourly checkpoints rebuilt from the cloud's
// committed history, never from its live image, which is always ahead of
// what is uploaded. A fresh node then restores from that checkpoint alone.
func TestAcceleratorAttachedCheckpointRebuildsFromCloudHistory(t *testing.T) {
	cloud, c, v := newRecoveryCloud(t)
	expected := seedRecoveryCheckpoint(t, c, v)
	entry := func(seq, block uint64, fill byte) lbd.Entry {
		data := bytes.Repeat([]byte{fill}, 4096)
		return lbd.Entry{Op: "W", Sequence: seq, Block: block, Length: 4096, Data: data, Checksum: crc32.ChecksumIEEE(data)}
	}
	cloud.addTail(t, []lbd.Entry{entry(41, 2, 0xC3)})
	cloud.addTail(t, []lbd.Entry{{Op: "T", Sequence: 42, Block: 1, Length: 4096}, entry(43, 3, 0xD4)})
	copy(expected[2*4096:], bytes.Repeat([]byte{0xC3}, 4096))
	clear(expected[4096 : 2*4096])
	copy(expected[3*4096:], bytes.Repeat([]byte{0xD4}, 4096))
	head := cloud.manifest.Head

	// The live image holds a write whose segment is not uploaded yet.
	live := slices.Clone(expected)
	copy(live[5*4096:], bytes.Repeat([]byte{0xEE}, 4096))
	require.NoError(t, os.WriteFile(filepath.Join(v.DiskPath, "disk.img"), live, 0600))
	require.NoError(t, c.state.SetMountAndSave("mount", &MountState{EntityId: "mount", VolumeId: v.VolumeId, CloudVolumeId: "vol",
		DevicePath: "/dev/lbd0", Mounted: true, Mode: storage_v1alpha.VM_ACCELERATOR, LeaseNonce: "nonce", LeaseOwned: true}))

	published := len(cloud.publications)
	due := map[string]time.Time{}
	now := time.Now()
	c.checkpointAttachedMounts(t.Context(), due, now)
	require.Len(t, cloud.publications, published, "a mount just seen attached from a checkpoint is not due yet")
	c.checkpointAttachedMounts(t.Context(), due, now.Add(DefaultAttachedCheckpointInterval-time.Second))
	require.Len(t, cloud.publications, published)

	c.checkpointAttachedMounts(t.Context(), due, now.Add(DefaultAttachedCheckpointInterval))
	require.Len(t, cloud.publications, published+1)
	p := cloud.publications[published]
	require.Equal(t, checkpointBasisReplayed, p.Basis)
	require.Equal(t, head, p.Base.ThroughLabel, "the boundary is the committed head it was rebuilt through")
	digest := sha256.Sum256(expected)
	require.Equal(t, hex.EncodeToString(digest[:]), p.Base.ImageSHA256, "the checkpoint holds committed history only")
	leftovers, err := filepath.Glob(filepath.Join(v.DiskPath, ".*"))
	require.NoError(t, err)
	require.Empty(t, leftovers, "the rebuilt image and payload are removed")
	actual, err := os.ReadFile(filepath.Join(v.DiskPath, "disk.img"))
	require.NoError(t, err)
	require.Equal(t, live, actual, "the live image is never touched")

	// Nothing committed since: the next interval publishes nothing.
	c.checkpointAttachedMounts(t.Context(), due, now.Add(2*DefaultAttachedCheckpointInterval))
	require.Len(t, cloud.publications, published+1)

	// A fresh node restores from the attached checkpoint with no tail.
	cloud.mockCloudServer.mu.Lock()
	for id, object := range cloud.segments {
		if object.Kind == string(KindLBDLog) {
			delete(cloud.segments, id)
		}
	}
	cloud.mockCloudServer.mu.Unlock()
	v.DiskPath = streamDir(t)
	require.NoError(t, c.recoverAccelerator(t.Context(), v, "nonce"))
	actual, err = os.ReadFile(filepath.Join(v.DiskPath, "disk.img"))
	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

// A new disk that has stayed attached since it was created has no stream.
// Its first attached checkpoint is rebuilt from its whole log and starts
// one, which the volume then keeps.
func TestAcceleratorAttachedCheckpointLeavesNewVolumeToDetach(t *testing.T) {
	cloud, c, v := newRecoveryCloud(t)
	expected := make([]byte, 1024*1024)
	for i, fill := range []byte{0x31, 0x42} {
		label := fmt.Sprintf("4000000000000000000000%02x", i+1)
		data := bytes.Repeat([]byte{fill}, 4096)
		copy(expected[(i+2)*4096:], data)
		segment := buildSegmentData(t, 4096, label, []lbd.Entry{makeWriteEntry(uint64(i+2), data)})
		_, err := c.updatesClient.Upload(t.Context(), "vol", UploadRequest{Kind: KindLBDLog, OrderingKey: label}, bytes.NewReader(segment), int64(len(segment)))
		require.NoError(t, err)
	}
	cloud.mu.Lock()
	cloud.manifest.Status = "legacy"
	cloud.mu.Unlock()
	live := slices.Clone(expected)
	copy(live[7*4096:], bytes.Repeat([]byte{0xEE}, 4096))
	require.NoError(t, os.WriteFile(filepath.Join(v.DiskPath, "disk.img"), live, 0600))
	require.NoError(t, c.state.SetMountAndSave("mount", &MountState{EntityId: "mount", VolumeId: v.VolumeId, CloudVolumeId: "vol",
		DevicePath: "/dev/lbd0", Mounted: true, Mode: storage_v1alpha.VM_ACCELERATOR, LeaseNonce: "nonce", LeaseOwned: true}))

	due := map[string]time.Time{}
	now := time.Now()
	c.checkpointAttachedMounts(t.Context(), due, now)
	c.checkpointAttachedMounts(t.Context(), due, now.Add(DefaultAttachedCheckpointInterval))
	// The legacy listing has nothing to verify a rebuild against, and a first
	// checkpoint would make that log retirable, so only detach starts the stream.
	require.Empty(t, cloud.publications)
	require.NoFileExists(t, filepath.Join(v.DiskPath, acceleratorStreamFile))
}

// The server's shutdown detaches every lbd device cleanly, which proves the
// image as well as a detach during cleanup does. The checkpoint that cleanup
// would have published must still come once the mount is released after the
// restart, even though the device is gone by then.
func TestAcceleratorCheckpointAfterShutdownDetach(t *testing.T) {
	cloud, c, v := newRecoveryCloud(t)
	v.DiskPath = t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(v.DiskPath, "disk.img"), make([]byte, 1024*1024), 0600))
	const label = "400000000000000000000001"
	segment := buildSegmentData(t, 4096, label, []lbd.Entry{makeWriteEntry(1, bytes.Repeat([]byte{0x7E}, 4096))})
	_, err := c.updatesClient.Upload(t.Context(), "vol", UploadRequest{Kind: KindLBDLog, OrderingKey: label}, bytes.NewReader(segment), int64(len(segment)))
	require.NoError(t, err)
	require.NoError(t, c.state.SetVolumeAndSave(v.EntityId, v))
	mount := &storage_v1alpha.DiskMount{ID: "mount", VolumeId: "volume", MountPath: filepath.Join(diskMountBasePath, "hill-shutdown"),
		DesiredState: storage_v1alpha.DM_WANT_MOUNTED, ActualState: storage_v1alpha.DM_PENDING}
	require.NoError(t, c.Reconcile(t.Context(), mount, nil))
	ops := c.ops.(*mockDiskMountOps)
	NewDiskVolumeController(slog.Default(), v.DiskPath, compute.NewNodeId("fresh-node"), c.state, nil, ops).Shutdown()
	require.NotEmpty(t, ops.detachedLbds, "shutdown detached the device")

	restarted, err := LoadState(filepath.Dir(c.state.path))
	require.NoError(t, err)
	c.state = restarted
	mount.DesiredState, mount.ActualState = storage_v1alpha.DM_WANT_UNMOUNTED, storage_v1alpha.DM_ERROR
	require.NoError(t, c.Reconcile(t.Context(), mount, nil))
	require.Nil(t, c.state.GetMount("mount"))
	require.Equal(t, 1, checkpointClaims(cloud), "a clean shutdown detach is a checkpoint")
}

// A server restart cannot detach an lbd device an app container still has
// mounted. The next process must adopt that device under the lease it still
// holds rather than retry the detach forever.
func TestAcceleratorAdoptsLbdThatOutlivedServer(t *testing.T) {
	cloud, c, v := newRecoveryCloud(t)
	v.DiskPath = t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(v.DiskPath, "disk.img"), make([]byte, 1024*1024), 0600))
	require.NoError(t, c.state.SetVolumeAndSave(v.EntityId, v))
	mount := &storage_v1alpha.DiskMount{ID: "mount", VolumeId: "volume", MountPath: filepath.Join(diskMountBasePath, "hill-adopt"),
		DesiredState: storage_v1alpha.DM_WANT_MOUNTED, ActualState: storage_v1alpha.DM_PENDING}
	require.NoError(t, c.Reconcile(t.Context(), mount, nil))
	before := *c.state.GetMount("mount")
	ops := c.ops.(*mockDiskMountOps)
	ops.lbdDetachErr = fmt.Errorf("LBD_CTL_REMOVE failed: Device or resource busy")
	NewDiskVolumeController(slog.Default(), v.DiskPath, compute.NewNodeId("fresh-node"), c.state, nil, ops).Shutdown()
	require.False(t, ops.IsMounted(mount.MountPath))

	restarted, err := LoadState(filepath.Dir(c.state.path))
	require.NoError(t, err)
	c.state = restarted
	mount.ActualState = storage_v1alpha.DM_MOUNTED
	require.NoError(t, c.Reconcile(t.Context(), mount, nil))
	after := c.state.GetMount("mount")
	require.True(t, after.Mounted)
	require.Equal(t, before.DevicePath, after.DevicePath)
	require.Equal(t, before.LeaseNonce, after.LeaseNonce, "the device keeps logging under its lease")
	require.True(t, ops.IsMounted(mount.MountPath))
	require.Len(t, ops.attachedLbds, 1, "no second device for the same image")
	require.Zero(t, checkpointClaims(cloud))
	cloud.mockCloudServer.mu.Lock()
	require.Len(t, cloud.leases, 1)
	cloud.mockCloudServer.mu.Unlock()
}
