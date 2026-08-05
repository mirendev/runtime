package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/registration"
)

// newUnregisterCloud stands up a stub cloud that answers the service account
// handshake and hands DELETE /api/v1/self/cluster to deleteStatus.
func newUnregisterCloud(t *testing.T, deleteStatus int, deleted *bool) *httptest.Server {
	t.Helper()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/service-account/begin":
			json.NewEncoder(w).Encode(cloudauth.BeginAuthResponse{
				Envelope:  "test-envelope",
				Challenge: "dGVzdC1jaGFsbGVuZ2U=",
			})
		case "/auth/service-account/complete":
			json.NewEncoder(w).Encode(cloudauth.CompleteAuthResponse{
				Token:     "test-jwt-token",
				ExpiresIn: 3600,
			})
		case "/api/v1/self/cluster":
			if deleted != nil {
				*deleted = true
			}
			w.WriteHeader(deleteStatus)
		default:
			t.Errorf("unexpected request to %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(ts.Close)

	return ts
}

// seedRegistration writes a registration with the given status pointing at
// cloudURL, and returns the directory holding it.
func seedRegistration(t *testing.T, cloudURL, status string) string {
	t.Helper()

	dir := t.TempDir()

	privateKey, _, err := registration.GenerateKeyPair()
	require.NoError(t, err)

	require.NoError(t, registration.SaveRegistration(dir, &registration.StoredRegistration{
		ClusterID:   "cluster-123",
		ClusterName: "prod",
		PrivateKey:  privateKey,
		CloudURL:    cloudURL,
		Status:      status,
	}))

	return dir
}

func testUnregisterContext() (*Context, *bytes.Buffer) {
	var buf bytes.Buffer
	return &Context{Context: context.Background(), Stdout: &buf}, &buf
}

func requireRegistrationCleared(t *testing.T, dir string) {
	t.Helper()
	for _, name := range registration.RegistrationFiles {
		require.NoFileExists(t, filepath.Join(dir, name))
	}
}

func requireRegistrationIntact(t *testing.T, dir string) {
	t.Helper()
	for _, name := range registration.RegistrationFiles {
		require.FileExists(t, filepath.Join(dir, name))
	}
}

func TestUnregisterRemovesCloudEntryThenLocalState(t *testing.T) {
	var deleted bool
	ts := newUnregisterCloud(t, http.StatusNoContent, &deleted)
	dir := seedRegistration(t, ts.URL, "approved")

	ctx, _ := testUnregisterContext()
	require.NoError(t, Unregister(ctx, UnregisterOptions{Force: true, OutputDir: dir}))

	require.True(t, deleted, "cloud should have been asked to remove the cluster")
	requireRegistrationCleared(t, dir)
}

// The service account key is the only credential that can authorize the cloud
// side, and it lives in the files the local half deletes. If the cloud call
// fails, the local state has to survive so the operator can retry from this
// machine instead of stranding an unreachable cluster in the organization.
func TestUnregisterKeepsLocalStateWhenCloudFails(t *testing.T) {
	ts := newUnregisterCloud(t, http.StatusInternalServerError, nil)
	dir := seedRegistration(t, ts.URL, "approved")

	ctx, _ := testUnregisterContext()
	err := Unregister(ctx, UnregisterOptions{Force: true, OutputDir: dir})

	require.Error(t, err)
	require.Contains(t, err.Error(), "--local-only")
	requireRegistrationIntact(t, dir)
}

// A cluster that cloud has already forgotten should still finish its local
// cleanup, so a retried unregister converges instead of wedging.
func TestUnregisterCompletesWhenCloudAlreadyForgotCluster(t *testing.T) {
	ts := newUnregisterCloud(t, http.StatusNotFound, nil)
	dir := seedRegistration(t, ts.URL, "approved")

	ctx, _ := testUnregisterContext()
	require.NoError(t, Unregister(ctx, UnregisterOptions{Force: true, OutputDir: dir}))

	requireRegistrationCleared(t, dir)
}

// --local-only is the orphan recovery path: clear local state without talking
// to cloud at all, for when the cloud entry is already gone or unreachable.
func TestUnregisterLocalOnlySkipsCloud(t *testing.T) {
	// Any request to this server fails the test, which is the assertion.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("--local-only contacted cloud at %s", r.URL.Path)
	}))
	defer ts.Close()

	dir := seedRegistration(t, ts.URL, "approved")

	ctx, buf := testUnregisterContext()
	require.NoError(t, Unregister(ctx, UnregisterOptions{Force: true, LocalOnly: true, OutputDir: dir}))

	requireRegistrationCleared(t, dir)
	require.Contains(t, buf.String(), "left in place")
}

// A registration that was never approved has no cluster in the organization to
// remove, so there is nothing to ask cloud for.
func TestUnregisterPendingRegistrationSkipsCloud(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("pending registration contacted cloud at %s", r.URL.Path)
	}))
	defer ts.Close()

	dir := seedRegistration(t, ts.URL, "pending")

	ctx, _ := testUnregisterContext()
	require.NoError(t, Unregister(ctx, UnregisterOptions{Force: true, OutputDir: dir}))

	requireRegistrationCleared(t, dir)
}

func TestUnregisterWithoutRegistrationIsANoOp(t *testing.T) {
	dir := t.TempDir()

	ctx, buf := testUnregisterContext()
	require.NoError(t, Unregister(ctx, UnregisterOptions{Force: true, OutputDir: dir}))

	require.Contains(t, buf.String(), "No cluster registration found")
}

// Unregister must not touch anything in the server directory that is not cloud
// registration material. The TLS pair secures CLI-to-cluster auth, the OIDC key
// signs end-user sessions, and the workload identity key anchors the cluster's
// own service identities — none of them have anything to do with the cloud.
func TestUnregisterLeavesUnrelatedServerFilesAlone(t *testing.T) {
	ts := newUnregisterCloud(t, http.StatusNoContent, nil)
	dir := seedRegistration(t, ts.URL, "approved")

	bystanders := []string{"ca.crt", "ca.key", "api.crt", "api.key", "oidc-signing.key", "workload-identity.key"}
	for _, name := range bystanders {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("keep me"), 0600))
	}

	ctx, _ := testUnregisterContext()
	require.NoError(t, Unregister(ctx, UnregisterOptions{Force: true, OutputDir: dir}))

	requireRegistrationCleared(t, dir)
	for _, name := range bystanders {
		require.FileExists(t, filepath.Join(dir, name))
	}
}
