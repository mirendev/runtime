package ocireg

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/entity/testutils"
)

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// newTestRegistry stands up the registry behind a real http.ServeMux and
// httptest server. Routing through the mux matters for these tests: ServeMux
// runs cleanPath against the *escaped* path, so an unencoded "../" is
// redirected away before we ever see it while "%2e%2e%2f" arrives fully
// decoded in r.URL.Path. Calling handlers directly would miss that entirely.
func newTestRegistry(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	base := t.TempDir()
	storageRoot := filepath.Join(base, "registry")
	require.NoError(t, os.MkdirAll(storageRoot, 0755))

	entServer, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	handler := NewRegistryHandler(storageRoot, testutils.TestLogger(t), entServer.Client)

	srv := httptest.NewServer(newMux(handler))
	t.Cleanup(srv.Close)

	return srv, base
}

// startUpload runs the POST/PATCH half of a blob push and returns the upload
// UUID, leaving the upload staged and ready to be completed.
func startUpload(t *testing.T, srv *httptest.Server, name string, payload []byte) string {
	t.Helper()

	resp, err := http.Post(srv.URL+"/v2/"+name+"/blobs/uploads/", "", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	uuid := resp.Header.Get("Docker-Upload-UUID")
	require.NotEmpty(t, uuid)

	req, err := http.NewRequest(http.MethodPatch,
		fmt.Sprintf("%s/v2/%s/blobs/uploads/%s", srv.URL, name, uuid),
		bytes.NewReader(payload))
	require.NoError(t, err)

	patchResp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer patchResp.Body.Close()
	require.Equal(t, http.StatusAccepted, patchResp.StatusCode)

	return uuid
}

// TestCompleteBlobUpload_RejectsTraversalDigest covers the sink from MIR-1474.
// The digest arrives on the query string rather than the URL path, so nothing
// about the route constrains it: a sandbox could complete an upload with
// ?digest=../../pwned and land arbitrary content at an arbitrary path as root.
func TestCompleteBlobUpload_RejectsTraversalDigest(t *testing.T) {
	srv, base := newTestRegistry(t)

	payload := []byte("#!/bin/sh\nowned\n")
	uuid := startUpload(t, srv, "victim", payload)

	// blobs live at <base>/registry/blobs, so this escapes to <base>/pwned.
	req, err := http.NewRequest(http.MethodPut,
		fmt.Sprintf("%s/v2/victim/blobs/uploads/%s?digest=%s",
			srv.URL, uuid, "..%2F..%2Fpwned"), nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	escaped := filepath.Join(base, "pwned")
	_, statErr := os.Stat(escaped)
	assert.True(t, os.IsNotExist(statErr),
		"upload escaped the storage root and wrote %s", escaped)

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode,
		"a digest that isn't a digest should be rejected outright")
}

// TestCompleteBlobUpload_RejectsMismatchedDigest guards blob integrity. Without
// this check any sandbox can overwrite another app's layer, since os.Rename
// happily clobbers whatever already sits at the destination.
func TestCompleteBlobUpload_RejectsMismatchedDigest(t *testing.T) {
	srv, base := newTestRegistry(t)

	uuid := startUpload(t, srv, "victim", []byte("not what the digest says"))

	// A well-formed digest that simply doesn't match the uploaded bytes.
	claimed := "sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	req, err := http.NewRequest(http.MethodPut,
		fmt.Sprintf("%s/v2/victim/blobs/uploads/%s?digest=%s", srv.URL, uuid, claimed), nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode,
		"content that doesn't hash to the claimed digest should be rejected")

	blobPath := filepath.Join(base, "registry", "blobs", claimed)
	_, statErr := os.Stat(blobPath)
	assert.True(t, os.IsNotExist(statErr), "mismatched blob should not be stored")
}

// TestBlobUpload_RoundTrip is the happy path, here to keep the validation from
// quietly breaking real pushes from buildkit.
func TestBlobUpload_RoundTrip(t *testing.T) {
	srv, _ := newTestRegistry(t)

	payload := []byte("layer contents")
	digest := digestOf(payload)

	uuid := startUpload(t, srv, "myapp", payload)

	req, err := http.NewRequest(http.MethodPut,
		fmt.Sprintf("%s/v2/%s/blobs/uploads/%s?digest=%s", srv.URL, "myapp", uuid, digest), nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Equal(t, digest, resp.Header.Get("Docker-Content-Digest"))

	getResp, err := http.Get(fmt.Sprintf("%s/v2/%s/blobs/%s", srv.URL, "myapp", digest))
	require.NoError(t, err)
	defer getResp.Body.Close()
	require.Equal(t, http.StatusOK, getResp.StatusCode)

	body, err := io.ReadAll(getResp.Body)
	require.NoError(t, err)
	assert.Equal(t, payload, body)
}

// TestNestedRepositoryName_RoundTrip pins that names with slashes still work.
// The OCI grammar allows them and our routing joins everything before
// "blobs"/"manifests" back together, so validation has to permit them while
// still rejecting ".." segments.
func TestNestedRepositoryName_RoundTrip(t *testing.T) {
	srv, _ := newTestRegistry(t)

	payload := []byte("nested layer")
	digest := digestOf(payload)

	uuid := startUpload(t, srv, "team/myapp", payload)

	req, err := http.NewRequest(http.MethodPut,
		fmt.Sprintf("%s/v2/team/myapp/blobs/uploads/%s?digest=%s", srv.URL, uuid, digest), nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
}

// TestEncodedTraversal_Rejected sweeps the routes that are only safe today by
// accident. Decoded slashes shift the route markers out of position so these
// currently fall through to 404, but that's a property of the suffix matching
// rather than a deliberate check. Pin the behavior so a routing change can't
// silently reopen them.
func TestEncodedTraversal_Rejected(t *testing.T) {
	srv, base := newTestRegistry(t)

	sentinel := filepath.Join(base, "secret.txt")
	require.NoError(t, os.WriteFile(sentinel, []byte("classified"), 0600))

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"blob get escapes root", http.MethodGet,
			"/v2/app/blobs/%2e%2e%2f%2e%2e%2fsecret.txt"},
		{"blob head escapes root", http.MethodHead,
			"/v2/app/blobs/%2e%2e%2f%2e%2e%2fsecret.txt"},
		{"manifest get escapes root", http.MethodGet,
			"/v2/%2e%2e%2f%2e%2e%2f%2e%2e%2fetc/manifests/passwd"},
		{"upload uuid escapes root", http.MethodPatch,
			"/v2/app/blobs/uploads/%2e%2e%2f%2e%2e%2fsecret.txt"},
		{"upload complete uuid escapes root", http.MethodPut,
			"/v2/app/blobs/uploads/%2e%2e%2f%2e%2e%2fsecret.txt?digest=sha256:" +
				"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		{"dot dot repository name", http.MethodGet,
			"/v2/..%2f..%2fetc/blobs/sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, srv.URL+tc.path, nil)
			require.NoError(t, err)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)
			assert.NotContains(t, string(body), "classified",
				"traversal leaked file contents")
			assert.GreaterOrEqual(t, resp.StatusCode, 400,
				"traversal attempt should be refused, got %d", resp.StatusCode)
		})
	}

	// Nothing outside the storage root should have been created either.
	entries, err := os.ReadDir(base)
	require.NoError(t, err)
	for _, e := range entries {
		assert.Contains(t, []string{"registry", "secret.txt"}, e.Name(),
			"unexpected file created outside the storage root")
	}
}
