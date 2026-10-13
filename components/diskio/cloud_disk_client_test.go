package diskio

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
)

func TestAcquireLeaseSuccess(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	var (
		receivedMethod      string
		receivedPath        string
		receivedContentType string
		receivedAuth        string
	)

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		receivedContentType = r.Header.Get("Content-Type")
		receivedAuth = r.Header.Get("Authorization")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(leaseResponse{
			LeaseID:  "lease-abc",
			VolumeID: "vol-1",
			Nonce:    "nonce-xyz",
		})
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	nonce, err := client.AcquireLease(context.Background(), "vol-1")

	require.NoError(t, err)
	assert.Equal(t, "nonce-xyz", nonce)
	assert.Equal(t, "POST", receivedMethod)
	assert.Equal(t, "/api/v1/disk/volumes/vol-1/lease", receivedPath)
	assert.Equal(t, "application/json", receivedContentType)
	assert.True(t, strings.HasPrefix(receivedAuth, "Bearer "), "should have Bearer token")
}

func TestAcquireLeaseConflict(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte("already leased"))
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	_, err := client.AcquireLease(context.Background(), "vol-1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "already has an active lease")
}

func TestAcquireLeaseServerError(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	_, err := client.AcquireLease(context.Background(), "vol-1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
	assert.Contains(t, err.Error(), "internal error")
}

func TestReleaseLeaseSuccess(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	var (
		receivedMethod string
		receivedPath   string
		receivedNonce  string
		receivedAuth   string
	)

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		receivedNonce = r.Header.Get("X-Lease-Nonce")
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	err := client.ReleaseLease(context.Background(), "vol-1", "nonce-abc")

	require.NoError(t, err)
	assert.Equal(t, "DELETE", receivedMethod)
	assert.Equal(t, "/api/v1/disk/volumes/vol-1/lease", receivedPath)
	assert.Equal(t, "nonce-abc", receivedNonce)
	assert.True(t, strings.HasPrefix(receivedAuth, "Bearer "))
}

func TestReleaseLeaseError(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("lease not found"))
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	err := client.ReleaseLease(context.Background(), "vol-1", "bad-nonce")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
	assert.Contains(t, err.Error(), "lease not found")
}

func TestReleaseLeaseReconcilesOnlyExplicitNotOwner(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		resolved bool
	}{
		{"not-owner", 403, `{"code":"lease_not_owner","volume_id":"vol-1"}`, true},
		{"generic-forbidden", 403, `{"error":"Invalid lease nonce"}`, false},
		{"wrong-volume", 403, `{"code":"lease_not_owner","volume_id":"other"}`, false},
		{"unauthenticated", 401, `{"code":"lease_not_owner","volume_id":"vol-1"}`, false},
		{"server-error", 500, `{"code":"lease_not_owner","volume_id":"vol-1"}`, false},
		{"malformed", 403, `{"code":`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts, h, auth := newTestUploaderServer(t)
			h.handler = func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "stale", r.Header.Get("X-Lease-Nonce"))
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}
			err := NewCloudDiskClient(slog.Default(), ts.URL, auth).ReleaseLease(t.Context(), "vol-1", "stale")
			if tc.resolved {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestLeaseCleanupRecoversCrashAfterCloudDelete(t *testing.T) {
	ts, h, auth := newTestUploaderServer(t)
	var current atomic.Value
	current.Store("old")
	h.handler = func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Lease-Nonce") == current.Load().(string) {
			current.Store("")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"lease_not_owner","volume_id":"cloud"}`))
	}
	dir := t.TempDir()
	state, err := LoadState(dir)
	require.NoError(t, err)
	require.NoError(t, state.SetMountAndSave("old-mount", &MountState{
		EntityId: "old-mount", CloudVolumeId: "cloud", LeaseNonce: "old",
		BackingPath: filepath.Join(dir, "disk.img"),
		DevicePath:  "/dev/lbd0", Mode: storage_v1alpha.VM_ACCELERATOR,
	}))
	client := NewCloudDiskClient(slog.Default(), ts.URL, auth)
	require.NoError(t, client.ReleaseLease(t.Context(), "cloud", "old"))
	// Crash before writing the successful release to disk. A replacement now
	// holds the reused device; recovery must reclaim only the stale record.
	current.Store("replacement")
	state, err = LoadState(dir)
	require.NoError(t, err)
	state.SetMount("live", &MountState{EntityId: "live", CloudVolumeId: "cloud", DevicePath: "/dev/lbd0", LeaseNonce: "replacement", Mounted: true})
	ops := newMockDiskMountOps()
	controller := NewDiskMountController(slog.Default(), dir, compute.NewNodeId("node"), state, ops)
	controller.SetCloudClient(client)
	require.NoError(t, controller.Reconcile(t.Context(), &storage_v1alpha.DiskMount{ID: "old-mount", DesiredState: storage_v1alpha.DM_WANT_UNMOUNTED}, nil))
	require.Equal(t, "replacement", current.Load())
	require.Empty(t, ops.detachedLbds)
	state, err = LoadState(dir)
	require.NoError(t, err)
	require.Nil(t, state.GetMount("old-mount"))
	require.Equal(t, "replacement", state.LeaseNonceForCloudVolume("cloud"))
}

func TestListLogSegmentsSuccess(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	var receivedPath, receivedKind string

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedKind = r.URL.Query().Get("kind")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(listUpdatesResponseJSON{
			Updates: []UpdateInfo{
				{UpdateID: "seg-1", Kind: "lbd_log", OrderingKey: "400002b3a1c5f2b400000001"},
				{UpdateID: "seg-2", Kind: "lbd_log", OrderingKey: "400002b3a1c5f2b400000002"},
				{UpdateID: "seg-3", Kind: "lbd_log", OrderingKey: "400002b3a1c5f2b400000003"},
			},
		})
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	segments, err := client.ListLogSegments(context.Background(), "vol-1", "")

	require.NoError(t, err)
	assert.Equal(t, "/api/v1/disk/volumes/vol-1/updates", receivedPath)
	assert.Equal(t, "lbd_log", receivedKind, "log segments are lbd_log updates")
	require.Len(t, segments, 3)
	assert.Equal(t, "seg-1", segments[0].SegmentID)
	assert.Equal(t, "400002b3a1c5f2b400000001", segments[0].Label)
	assert.Equal(t, "seg-2", segments[1].SegmentID)
	assert.Equal(t, "seg-3", segments[2].SegmentID)
}

func TestListLogSegmentsEmpty(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(listUpdatesResponseJSON{Updates: []UpdateInfo{}})
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	segments, err := client.ListLogSegments(context.Background(), "vol-1", "")

	require.NoError(t, err)
	assert.Empty(t, segments)
}

func TestListLogSegmentsError(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("unauthorized"))
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	_, err := client.ListLogSegments(context.Background(), "vol-1", "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

// The volume ID now travels in the path, so it has to survive path escaping.
func TestListLogSegmentsEscapesVolumeID(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	var receivedPath string

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(listUpdatesResponseJSON{})
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	_, err := client.ListLogSegments(context.Background(), "vol with spaces", "")

	require.NoError(t, err)
	assert.Equal(t, "/api/v1/disk/volumes/vol with spaces/updates", receivedPath)
}

func TestDownloadLogSegmentSuccess(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	segmentData := []byte("this is the segment data content")

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/download"):
			// Verify the path includes the segment ID
			assert.Contains(t, r.URL.Path, "/api/v1/disk/volumes/vol-1/updates/seg-abc/download")
			assert.True(t, strings.HasPrefix(r.Header.Get("Authorization"), "Bearer "))

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(downloadResponseJSON{
				DownloadURL: ts.URL + "/data/seg-abc",
			})

		case r.Method == "GET" && r.URL.Path == "/data/seg-abc":
			// Presigned URL — no auth header expected
			w.Write(segmentData)
		}
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	rc, err := client.DownloadLogSegment(context.Background(), "vol-1", "seg-abc")

	require.NoError(t, err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, segmentData, data)
}

func TestDownloadLogSegmentAPIError(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("segment not found"))
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	_, err := client.DownloadLogSegment(context.Background(), "vol-1", "no-such-seg")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
	assert.Contains(t, err.Error(), "segment not found")
}

func TestDownloadLogSegmentDataFetchError(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/download"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(downloadResponseJSON{
				DownloadURL: ts.URL + "/data/expired",
			})
		case r.URL.Path == "/data/expired":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("presigned URL expired"))
		}
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	_, err := client.DownloadLogSegment(context.Background(), "vol-1", "seg-1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

func TestAcquireLeaseIncludesAuthToken(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	var receivedAuth string

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(leaseResponse{
			LeaseID:  "lease-1",
			VolumeID: "vol-1",
			Nonce:    "nonce-1",
		})
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	_, err := client.AcquireLease(context.Background(), "vol-1")

	require.NoError(t, err)
	assert.Equal(t, "Bearer test-jwt-token", receivedAuth)
}

func TestListLogSegmentsIncludesAuthToken(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	var receivedAuth string

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(listUpdatesResponseJSON{})
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	_, err := client.ListLogSegments(context.Background(), "vol-1", "")

	require.NoError(t, err)
	assert.Equal(t, "Bearer test-jwt-token", receivedAuth)
}

func TestDownloadLogSegmentIncludesAuthToken(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	var apiAuth string

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/download"):
			apiAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(downloadResponseJSON{
				DownloadURL: ts.URL + "/data/seg-1",
			})
		case r.URL.Path == "/data/seg-1":
			w.Write([]byte("data"))
		}
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	rc, err := client.DownloadLogSegment(context.Background(), "vol-1", "seg-1")
	require.NoError(t, err)
	rc.Close()

	assert.Equal(t, "Bearer test-jwt-token", apiAuth)
}

func TestReleaseLeaseIncludesAuthToken(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	var receivedAuth string

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	err := client.ReleaseLease(context.Background(), "vol-1", "nonce-1")

	require.NoError(t, err)
	assert.Equal(t, "Bearer test-jwt-token", receivedAuth)
}

func TestAcquireLeaseInvalidJSON(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not valid json"))
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	_, err := client.AcquireLease(context.Background(), "vol-1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode")
}

func TestListLogSegmentsInvalidJSON(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{malformed"))
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	_, err := client.ListLogSegments(context.Background(), "vol-1", "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode")
}

func TestDownloadLogSegmentInvalidJSON(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("bad json"))
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	_, err := client.DownloadLogSegment(context.Background(), "vol-1", "seg-1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode")
}

func TestAcquireLeaseURLConstruction(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	var receivedPath string

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(leaseResponse{Nonce: "n"})
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	_, err := client.AcquireLease(context.Background(), "my-volume-id")

	require.NoError(t, err)
	assert.Equal(t, "/api/v1/disk/volumes/my-volume-id/lease", receivedPath)
}

func TestReleaseLeaseURLConstruction(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	var receivedPath string

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	err := client.ReleaseLease(context.Background(), "my-volume-id", "nonce")

	require.NoError(t, err)
	assert.Equal(t, "/api/v1/disk/volumes/my-volume-id/lease", receivedPath)
}

func TestDownloadLogSegmentURLConstruction(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	var receivedPath string

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/download"):
			receivedPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(downloadResponseJSON{
				DownloadURL: ts.URL + "/data",
			})
		case r.URL.Path == "/data":
			w.Write([]byte("x"))
		}
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	rc, err := client.DownloadLogSegment(context.Background(), "vol-1", "my-seg-id")
	require.NoError(t, err)
	rc.Close()

	assert.Equal(t, "/api/v1/disk/volumes/vol-1/updates/my-seg-id/download", receivedPath)
}

func TestLeaseAcquisitionIntentRecovery(t *testing.T) {
	for _, failure := range []string{"lost-response", "nonce-save", "expired", "conflict"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "disk.img"), make([]byte, 4096), 0600))
			state, err := LoadState(dir)
			require.NoError(t, err)
			require.NoError(t, state.SetVolumeAndSave("volume", &VolumeState{
				EntityId: "volume", CloudVolumeId: "cloud", DiskPath: dir, Mode: storage_v1alpha.VM_ACCELERATOR,
			}))
			ts, h, auth := newTestUploaderServer(t)
			var firstID atomic.Value
			var calls atomic.Int32
			h.handler = func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					_, _ = w.Write([]byte(`{"updates":[]}`))
					return
				}
				require.Equal(t, "/api/v1/disk/volumes/cloud/lease/acquire", r.URL.Path)
				var request acquireLeaseRequest
				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				persisted, err := LoadState(dir)
				require.NoError(t, err)
				require.Equal(t, request.AcquisitionID, persisted.GetMount("mount").LeaseAcquisitionID, "intent must be durable before RPC")
				require.Empty(t, persisted.GetMount("mount").LeaseNonce)
				if calls.Add(1) == 1 {
					firstID.Store(request.AcquisitionID)
					switch failure {
					case "lost-response":
						connection, _, err := w.(http.Hijacker).Hijack()
						require.NoError(t, err)
						_ = connection.Close()
						return
					case "nonce-save":
						state.SetPath(filepath.Join(dir, "missing-directory"))
					case "expired":
						w.WriteHeader(http.StatusConflict)
						_ = json.NewEncoder(w).Encode(map[string]string{"code": "lease_acquisition_ended", "volume_id": "cloud", "acquisition_id": request.AcquisitionID})
						return
					case "conflict":
						w.WriteHeader(http.StatusConflict)
						_, _ = w.Write([]byte(`{"error":"conflicting owner"}`))
						return
					}
				} else if failure == "expired" {
					require.NotEqual(t, firstID.Load(), request.AcquisitionID, "only confirmed terminal intent may be replaced")
				} else {
					require.Equal(t, firstID.Load(), request.AcquisitionID, "ambiguous failure must retry original grant identity")
				}
				_ = json.NewEncoder(w).Encode(leaseResponse{VolumeID: "cloud", AcquisitionID: request.AcquisitionID, Nonce: "original-grant"})
			}
			controller := NewDiskMountController(slog.Default(), dir, compute.NewNodeId("node"), state, newMockDiskMountOps())
			controller.SetCloudClient(NewCloudDiskClient(slog.Default(), ts.URL, auth))
			mount := &storage_v1alpha.DiskMount{ID: "mount", VolumeId: "volume", MountPath: filepath.Join(dir, "mnt"), DesiredState: storage_v1alpha.DM_WANT_MOUNTED, ActualState: storage_v1alpha.DM_PENDING}
			require.Error(t, controller.Reconcile(t.Context(), mount, nil))
			restarted, err := LoadState(dir)
			require.NoError(t, err)
			if failure == "expired" {
				require.Empty(t, restarted.GetMount("mount").LeaseAcquisitionID)
			} else {
				require.Equal(t, firstID.Load(), restarted.GetMount("mount").LeaseAcquisitionID)
			}
			controller.state = restarted
			mount.ActualState = storage_v1alpha.DM_ATTACHING
			require.NoError(t, controller.Reconcile(t.Context(), mount, nil))
			require.Equal(t, int32(2), calls.Load())
			restarted, err = LoadState(dir)
			require.NoError(t, err)
			require.True(t, restarted.GetMount("mount").Mounted)
			require.Equal(t, "original-grant", restarted.LeaseNonceForCloudVolume("cloud"))
		})
	}
}

func TestLeaseAcquisitionRejectsUnboundResponses(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{
		{404, `{"error":"old cloud"}`},
		{200, `{"volume_id":"cloud","nonce":"n"}`},
		{200, `{"volume_id":"other","acquisition_id":"intent","nonce":"n"}`},
		{409, `{"code":"lease_acquisition_ended","volume_id":"cloud","acquisition_id":"other"}`},
		{403, `{"code":"lease_acquisition_ended","volume_id":"cloud","acquisition_id":"intent"}`},
	} {
		ts, h, auth := newTestUploaderServer(t)
		h.handler = func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}
		_, err := NewCloudDiskClient(slog.Default(), ts.URL, auth).AcquireLeaseWithIdentity(t.Context(), "cloud", "intent")
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrLeaseAcquisitionEnded)
	}
}
