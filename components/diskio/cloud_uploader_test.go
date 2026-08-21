package diskio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
