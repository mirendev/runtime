package diskio

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestListLogSegmentsSuccess(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	var receivedVolumeID string

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		receivedVolumeID = r.URL.Query().Get("volume_id")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(listLogSegmentsResponse{
			Segments: []logSegmentInfoJSON{
				{SegmentID: "seg-1", Label: "400002b3a1c5f2b400000001"},
				{SegmentID: "seg-2", Label: "400002b3a1c5f2b400000002"},
				{SegmentID: "seg-3", Label: "400002b3a1c5f2b400000003"},
			},
		})
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	segments, err := client.ListLogSegments(context.Background(), "vol-1", "")

	require.NoError(t, err)
	assert.Equal(t, "vol-1", receivedVolumeID)
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
		json.NewEncoder(w).Encode(listLogSegmentsResponse{
			Segments: []logSegmentInfoJSON{},
		})
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

func TestListLogSegmentsEscapesVolumeID(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	var receivedVolumeID string

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		receivedVolumeID = r.URL.Query().Get("volume_id")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(listLogSegmentsResponse{})
	}

	client := NewCloudDiskClient(slog.Default(), ts.URL, authClient)
	_, err := client.ListLogSegments(context.Background(), "vol/with spaces&special", "")

	require.NoError(t, err)
	assert.Equal(t, "vol/with spaces&special", receivedVolumeID)
}

func TestDownloadLogSegmentSuccess(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	segmentData := []byte("this is the segment data content")

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/download"):
			// Verify the path includes the segment ID
			assert.Contains(t, r.URL.Path, "/api/v1/disk/log-segments/seg-abc/download")
			assert.True(t, strings.HasPrefix(r.Header.Get("Authorization"), "Bearer "))

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(logSegmentDownloadResponse{
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
			json.NewEncoder(w).Encode(logSegmentDownloadResponse{
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
		json.NewEncoder(w).Encode(listLogSegmentsResponse{})
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
			json.NewEncoder(w).Encode(logSegmentDownloadResponse{
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
			json.NewEncoder(w).Encode(logSegmentDownloadResponse{
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

	assert.Equal(t, "/api/v1/disk/log-segments/my-seg-id/download", receivedPath)
}
