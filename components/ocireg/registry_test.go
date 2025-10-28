package ocireg

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestPutManifest_PreventsDuplicateArtifacts(t *testing.T) {
	// Setup in-memory entity server
	entServer, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	log := testutils.TestLogger(t)

	// Create an app entity first
	var app core_v1alpha.App
	appID, err := entServer.Client.Create(context.Background(), "test-app", &app)
	require.NoError(t, err)

	// Create registry handler
	tmpDir := t.TempDir()
	handler := NewRegistryHandler(tmpDir, log, entServer.Client)

	// Create a test manifest
	manifest := map[string]interface{}{
		"schemaVersion": 2,
		"config": map[string]interface{}{
			"digest": "sha256:abcd1234",
			"size":   1024,
		},
	}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)

	// Calculate expected digest
	sum := sha256.Sum256(manifestData)
	expectedDigest := "sha256:" + hex.EncodeToString(sum[:])

	// First PUT - should create the artifact
	req1 := httptest.NewRequest(http.MethodPut, "/v2/test-app/manifests/artifact-1", bytes.NewReader(manifestData))
	rec1 := httptest.NewRecorder()

	handler.putManifest(rec1, req1, "test-app", "artifact-1")

	assert.Equal(t, http.StatusCreated, rec1.Code, "First PUT should succeed")
	assert.Equal(t, expectedDigest, rec1.Header().Get("Docker-Content-Digest"))

	// Verify artifact was created
	var artifact1 core_v1alpha.Artifact
	err = entServer.Client.Get(context.Background(), "artifact-1", &artifact1)
	require.NoError(t, err, "First artifact should exist")
	assert.Equal(t, expectedDigest, artifact1.ManifestDigest)
	assert.Equal(t, string(manifestData), artifact1.Manifest)
	assert.Equal(t, appID, artifact1.App)

	// Second PUT with same manifest but different reference - should NOT create duplicate
	req2 := httptest.NewRequest(http.MethodPut, "/v2/test-app/manifests/artifact-2", bytes.NewReader(manifestData))
	rec2 := httptest.NewRecorder()

	handler.putManifest(rec2, req2, "test-app", "artifact-2")

	assert.Equal(t, http.StatusCreated, rec2.Code, "Second PUT should succeed")
	assert.Equal(t, expectedDigest, rec2.Header().Get("Docker-Content-Digest"))

	// Verify that artifact-2 was NOT created (would return not found)
	var artifact2 core_v1alpha.Artifact
	err = entServer.Client.Get(context.Background(), "artifact-2", &artifact2)
	assert.Error(t, err, "Second artifact should not exist as a separate entity")

	// Verify only one artifact exists with this digest
	var foundArtifact core_v1alpha.Artifact
	err = entServer.Client.OneAtIndex(context.Background(), entity.String(core_v1alpha.ArtifactManifestDigestId, expectedDigest), &foundArtifact)
	require.NoError(t, err, "Should find exactly one artifact by digest")
	assert.Equal(t, artifact1.ID, foundArtifact.ID, "Should be the first artifact created")
}

func TestPutManifest_DifferentManifestsCreateSeparateArtifacts(t *testing.T) {
	// Setup in-memory entity server
	entServer, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	log := testutils.TestLogger(t)

	// Create an app entity
	var app core_v1alpha.App
	_, err := entServer.Client.Create(context.Background(), "test-app", &app)
	require.NoError(t, err)

	// Create registry handler
	tmpDir := t.TempDir()
	handler := NewRegistryHandler(tmpDir, log, entServer.Client)

	// Create two different manifests
	manifest1 := map[string]interface{}{
		"schemaVersion": 2,
		"config": map[string]interface{}{
			"digest": "sha256:abcd1234",
			"size":   1024,
		},
	}
	manifestData1, err := json.Marshal(manifest1)
	require.NoError(t, err)

	manifest2 := map[string]interface{}{
		"schemaVersion": 2,
		"config": map[string]interface{}{
			"digest": "sha256:efgh5678",
			"size":   2048,
		},
	}
	manifestData2, err := json.Marshal(manifest2)
	require.NoError(t, err)

	// PUT first manifest
	req1 := httptest.NewRequest(http.MethodPut, "/v2/test-app/manifests/artifact-1", bytes.NewReader(manifestData1))
	rec1 := httptest.NewRecorder()
	handler.putManifest(rec1, req1, "test-app", "artifact-1")
	assert.Equal(t, http.StatusCreated, rec1.Code)

	// PUT second manifest
	req2 := httptest.NewRequest(http.MethodPut, "/v2/test-app/manifests/artifact-2", bytes.NewReader(manifestData2))
	rec2 := httptest.NewRecorder()
	handler.putManifest(rec2, req2, "test-app", "artifact-2")
	assert.Equal(t, http.StatusCreated, rec2.Code)

	// Verify both artifacts exist
	var artifact1 core_v1alpha.Artifact
	err = entServer.Client.Get(context.Background(), "artifact-1", &artifact1)
	require.NoError(t, err, "First artifact should exist")

	var artifact2 core_v1alpha.Artifact
	err = entServer.Client.Get(context.Background(), "artifact-2", &artifact2)
	require.NoError(t, err, "Second artifact should exist")

	// Verify they have different digests
	assert.NotEqual(t, artifact1.ManifestDigest, artifact2.ManifestDigest, "Different manifests should have different digests")
}

func TestGetManifest_ByDigest(t *testing.T) {
	// Setup in-memory entity server
	entServer, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	log := testutils.TestLogger(t)

	// Create registry handler
	tmpDir := t.TempDir()
	handler := NewRegistryHandler(tmpDir, log, entServer.Client)

	// Create a test manifest
	manifest := map[string]interface{}{
		"schemaVersion": 2,
		"config": map[string]interface{}{
			"digest": "sha256:test1234",
			"size":   1024,
		},
	}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)

	// Calculate digest
	sum := sha256.Sum256(manifestData)
	digest := "sha256:" + hex.EncodeToString(sum[:])

	// Create artifact directly in entity store
	var artifact core_v1alpha.Artifact
	artifact.Manifest = string(manifestData)
	artifact.ManifestDigest = digest
	_, err = entServer.Client.Create(context.Background(), "test-artifact", &artifact)
	require.NoError(t, err)

	// GET manifest by digest
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v2/test-app/manifests/%s", digest), nil)
	rec := httptest.NewRecorder()

	handler.getManifest(rec, req, "test-app", digest)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/vnd.oci.image.manifest.v1+json", rec.Header().Get("Content-Type"))
	assert.Equal(t, digest, rec.Header().Get("Docker-Content-Digest"))

	// Verify response body matches original manifest
	body, err := io.ReadAll(rec.Body)
	require.NoError(t, err)
	assert.Equal(t, manifestData, body)
}

func TestGetManifest_ByReference(t *testing.T) {
	// Setup in-memory entity server
	entServer, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	log := testutils.TestLogger(t)

	// Create registry handler
	tmpDir := t.TempDir()
	handler := NewRegistryHandler(tmpDir, log, entServer.Client)

	// Create a test manifest
	manifest := map[string]interface{}{
		"schemaVersion": 2,
		"config": map[string]interface{}{
			"digest": "sha256:ref1234",
			"size":   512,
		},
	}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)

	// Calculate digest
	sum := sha256.Sum256(manifestData)
	digest := "sha256:" + hex.EncodeToString(sum[:])

	// Create artifact with a specific reference
	var artifact core_v1alpha.Artifact
	artifact.Manifest = string(manifestData)
	artifact.ManifestDigest = digest
	_, err = entServer.Client.Create(context.Background(), "my-artifact-ref", &artifact)
	require.NoError(t, err)

	// GET manifest by reference (not digest)
	req := httptest.NewRequest(http.MethodGet, "/v2/test-app/manifests/my-artifact-ref", nil)
	rec := httptest.NewRecorder()

	handler.getManifest(rec, req, "test-app", "my-artifact-ref")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, digest, rec.Header().Get("Docker-Content-Digest"))

	// Verify response body
	body, err := io.ReadAll(rec.Body)
	require.NoError(t, err)
	assert.Equal(t, manifestData, body)
}

func TestGetManifest_NotFound(t *testing.T) {
	// Setup in-memory entity server
	entServer, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	log := testutils.TestLogger(t)

	// Create registry handler
	tmpDir := t.TempDir()
	handler := NewRegistryHandler(tmpDir, log, entServer.Client)

	// Try to GET a non-existent manifest
	req := httptest.NewRequest(http.MethodGet, "/v2/test-app/manifests/does-not-exist", nil)
	rec := httptest.NewRecorder()

	handler.getManifest(rec, req, "test-app", "does-not-exist")

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
