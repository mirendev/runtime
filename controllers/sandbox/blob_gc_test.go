package sandbox

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	entitytestutils "miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/idgen"
)

func makeManifest(t *testing.T, configDigest string, layerDigests ...string) string {
	t.Helper()
	m := ociManifest{
		Config: ociDescriptor{Digest: configDigest},
	}
	for _, d := range layerDigests {
		m.Layers = append(m.Layers, ociDescriptor{Digest: d})
	}
	b, err := json.Marshal(m)
	require.NoError(t, err)
	return string(b)
}

func createArtifact(t *testing.T, eac *entityserver_v1alpha.EntityAccessClient, status core_v1alpha.ArtifactStatus, manifest string) entity.Id {
	t.Helper()
	name := idgen.GenNS("a")
	artID := entity.Id("artifact/" + name)
	art := &core_v1alpha.Artifact{
		ID:       artID,
		Manifest: manifest,
		Status:   status,
	}
	var rpcE entityserver_v1alpha.Entity
	rpcE.SetId(artID.String())
	rpcE.SetAttrs(entity.New(entity.DBId, artID, art.Encode).Attrs())
	_, err := eac.Put(context.Background(), &rpcE)
	require.NoError(t, err)
	return artID
}

func createAppVersion(t *testing.T, eac *entityserver_v1alpha.EntityAccessClient, staticArtifact string) entity.Id {
	t.Helper()
	name := idgen.GenNS("v")
	versionID := entity.Id("app_version/" + name)
	version := &core_v1alpha.AppVersion{
		ID:             versionID,
		Version:        name,
		StaticArtifact: staticArtifact,
	}
	var rpcE entityserver_v1alpha.Entity
	rpcE.SetId(versionID.String())
	rpcE.SetAttrs(entity.New(entity.DBId, versionID, version.Encode).Attrs())
	_, err := eac.Put(context.Background(), &rpcE)
	require.NoError(t, err)
	return versionID
}

func createBlobFile(t *testing.T, blobsDir, digest string, modTime time.Time) {
	t.Helper()
	path := filepath.Join(blobsDir, digest)
	require.NoError(t, os.WriteFile(path, []byte("blob-content"), 0644))
	require.NoError(t, os.Chtimes(path, modTime, modTime))
}

func TestRunBlobGC(t *testing.T) {
	entServer, cleanup := entitytestutils.NewInMemEntityServer(t)
	defer cleanup()

	eac := entServer.EAC
	log := entitytestutils.TestLogger(t)
	oldTime := time.Now().Add(-2 * time.Hour)

	t.Run("deletes unreferenced blobs", func(t *testing.T) {
		r := require.New(t)
		tmpDir := t.TempDir()
		blobsDir := filepath.Join(tmpDir, "registry", "blobs")
		r.NoError(os.MkdirAll(blobsDir, 0755))

		createBlobFile(t, blobsDir, "sha256:orphan1", oldTime)
		createBlobFile(t, blobsDir, "sha256:orphan2", oldTime)

		watchdog := &ImageWatchdog{
			Log:      log,
			EAC:      eac,
			DataPath: tmpDir,
			Config:   DefaultImageGCConfig(),
		}

		result, err := watchdog.RunBlobGC(context.Background())
		r.NoError(err)
		r.ElementsMatch([]string{"sha256:orphan1", "sha256:orphan2"}, result.DeletedBlobs)
		r.Equal(0, result.RetainedBlobs)
		r.Equal(2, result.TotalBlobs)
	})

	t.Run("keeps blobs referenced by active artifacts", func(t *testing.T) {
		r := require.New(t)
		tmpDir := t.TempDir()
		blobsDir := filepath.Join(tmpDir, "registry", "blobs")
		r.NoError(os.MkdirAll(blobsDir, 0755))

		manifest := makeManifest(t, "sha256:config1", "sha256:layer1", "sha256:layer2")
		createArtifact(t, eac, core_v1alpha.ACTIVE, manifest)

		createBlobFile(t, blobsDir, "sha256:config1", oldTime)
		createBlobFile(t, blobsDir, "sha256:layer1", oldTime)
		createBlobFile(t, blobsDir, "sha256:layer2", oldTime)
		createBlobFile(t, blobsDir, "sha256:orphan", oldTime)

		watchdog := &ImageWatchdog{
			Log:      log,
			EAC:      eac,
			DataPath: tmpDir,
			Config:   DefaultImageGCConfig(),
		}

		result, err := watchdog.RunBlobGC(context.Background())
		r.NoError(err)
		r.Equal([]string{"sha256:orphan"}, result.DeletedBlobs)
		r.Equal(3, result.RetainedBlobs)
	})

	t.Run("keeps static artifacts referenced by app versions", func(t *testing.T) {
		r := require.New(t)
		tmpDir := t.TempDir()
		blobsDir := filepath.Join(tmpDir, "registry", "blobs")
		r.NoError(os.MkdirAll(blobsDir, 0755))

		staticDigest := "sha256:static"
		versionID := createAppVersion(t, eac, staticDigest)
		createBlobFile(t, blobsDir, staticDigest, oldTime)
		createBlobFile(t, blobsDir, "sha256:orphan-static", oldTime)

		watchdog := &ImageWatchdog{
			Log:      log,
			EAC:      eac,
			DataPath: tmpDir,
			Config:   DefaultImageGCConfig(),
		}

		result, err := watchdog.RunBlobGC(context.Background())
		r.NoError(err)
		r.Equal([]string{"sha256:orphan-static"}, result.DeletedBlobs)
		r.Equal(1, result.RetainedBlobs)

		_, err = eac.Delete(context.Background(), versionID.String())
		r.NoError(err)
		result, err = watchdog.RunBlobGC(context.Background())
		r.NoError(err)
		r.Equal([]string{staticDigest}, result.DeletedBlobs)
	})

	t.Run("keeps blobs referenced by legacy (empty status) artifacts", func(t *testing.T) {
		r := require.New(t)
		tmpDir := t.TempDir()
		blobsDir := filepath.Join(tmpDir, "registry", "blobs")
		r.NoError(os.MkdirAll(blobsDir, 0755))

		manifest := makeManifest(t, "sha256:legacyconfig", "sha256:legacylayer")
		createArtifact(t, eac, "", manifest) // Empty status = legacy

		createBlobFile(t, blobsDir, "sha256:legacyconfig", oldTime)
		createBlobFile(t, blobsDir, "sha256:legacylayer", oldTime)

		watchdog := &ImageWatchdog{
			Log:      log,
			EAC:      eac,
			DataPath: tmpDir,
			Config:   DefaultImageGCConfig(),
		}

		result, err := watchdog.RunBlobGC(context.Background())
		r.NoError(err)
		r.Empty(result.DeletedBlobs)
		r.Equal(2, result.RetainedBlobs)
	})

	t.Run("deletes blobs only referenced by archived artifacts", func(t *testing.T) {
		r := require.New(t)
		tmpDir := t.TempDir()
		blobsDir := filepath.Join(tmpDir, "registry", "blobs")
		r.NoError(os.MkdirAll(blobsDir, 0755))

		manifest := makeManifest(t, "sha256:archivedconfig", "sha256:archivedlayer")
		createArtifact(t, eac, core_v1alpha.ARCHIVED, manifest)

		createBlobFile(t, blobsDir, "sha256:archivedconfig", oldTime)
		createBlobFile(t, blobsDir, "sha256:archivedlayer", oldTime)

		watchdog := &ImageWatchdog{
			Log:      log,
			EAC:      eac,
			DataPath: tmpDir,
			Config:   DefaultImageGCConfig(),
		}

		result, err := watchdog.RunBlobGC(context.Background())
		r.NoError(err)
		r.ElementsMatch([]string{"sha256:archivedconfig", "sha256:archivedlayer"}, result.DeletedBlobs)
	})

	t.Run("shared blobs retained if any active reference exists", func(t *testing.T) {
		r := require.New(t)
		tmpDir := t.TempDir()
		blobsDir := filepath.Join(tmpDir, "registry", "blobs")
		r.NoError(os.MkdirAll(blobsDir, 0755))

		sharedLayer := "sha256:sharedlayer"

		// Active artifact references the shared layer
		activeManifest := makeManifest(t, "sha256:activeconf", sharedLayer)
		createArtifact(t, eac, core_v1alpha.ACTIVE, activeManifest)

		// Archived artifact also references the shared layer
		archivedManifest := makeManifest(t, "sha256:archconf", sharedLayer)
		createArtifact(t, eac, core_v1alpha.ARCHIVED, archivedManifest)

		createBlobFile(t, blobsDir, "sha256:activeconf", oldTime)
		createBlobFile(t, blobsDir, sharedLayer, oldTime)
		createBlobFile(t, blobsDir, "sha256:archconf", oldTime)

		watchdog := &ImageWatchdog{
			Log:      log,
			EAC:      eac,
			DataPath: tmpDir,
			Config:   DefaultImageGCConfig(),
		}

		result, err := watchdog.RunBlobGC(context.Background())
		r.NoError(err)
		// Shared layer retained because active artifact references it
		r.NotContains(result.DeletedBlobs, sharedLayer)
		// Active artifact's config retained
		r.NotContains(result.DeletedBlobs, "sha256:activeconf")
		// Archived artifact's exclusive config deleted
		r.Contains(result.DeletedBlobs, "sha256:archconf")
	})

	t.Run("handles empty blobs directory", func(t *testing.T) {
		r := require.New(t)
		tmpDir := t.TempDir()
		blobsDir := filepath.Join(tmpDir, "registry", "blobs")
		r.NoError(os.MkdirAll(blobsDir, 0755))

		watchdog := &ImageWatchdog{
			Log:      log,
			EAC:      eac,
			DataPath: tmpDir,
			Config:   DefaultImageGCConfig(),
		}

		result, err := watchdog.RunBlobGC(context.Background())
		r.NoError(err)
		r.Empty(result.DeletedBlobs)
		r.Equal(0, result.TotalBlobs)
	})

	t.Run("handles missing blobs directory", func(t *testing.T) {
		r := require.New(t)
		tmpDir := t.TempDir()

		watchdog := &ImageWatchdog{
			Log:      log,
			EAC:      eac,
			DataPath: tmpDir,
			Config:   DefaultImageGCConfig(),
		}

		result, err := watchdog.RunBlobGC(context.Background())
		r.NoError(err)
		r.Empty(result.DeletedBlobs)
		r.Equal(0, result.TotalBlobs)
	})

	t.Run("handles malformed manifest", func(t *testing.T) {
		r := require.New(t)
		tmpDir := t.TempDir()
		blobsDir := filepath.Join(tmpDir, "registry", "blobs")
		r.NoError(os.MkdirAll(blobsDir, 0755))

		// Create artifact with invalid manifest - since we can't parse what it
		// references, its blobs may be deleted if not referenced elsewhere
		createArtifact(t, eac, core_v1alpha.ACTIVE, "not-valid-json{{{")

		createBlobFile(t, blobsDir, "sha256:someblob", oldTime)

		watchdog := &ImageWatchdog{
			Log:      log,
			EAC:      eac,
			DataPath: tmpDir,
			Config:   DefaultImageGCConfig(),
		}

		result, err := watchdog.RunBlobGC(context.Background())
		r.NoError(err)
		// The orphan blob is deleted because we couldn't parse the manifest
		// (malformed manifest is skipped, not treated as referencing all blobs)
		r.Contains(result.DeletedBlobs, "sha256:someblob")
	})

	t.Run("skips recently modified blobs", func(t *testing.T) {
		r := require.New(t)
		tmpDir := t.TempDir()
		blobsDir := filepath.Join(tmpDir, "registry", "blobs")
		r.NoError(os.MkdirAll(blobsDir, 0755))

		// Recent blob (within 1 hour) should be kept even if unreferenced
		recentTime := time.Now().Add(-30 * time.Minute)
		createBlobFile(t, blobsDir, "sha256:recent", recentTime)
		createBlobFile(t, blobsDir, "sha256:old", oldTime)

		watchdog := &ImageWatchdog{
			Log:      log,
			EAC:      eac,
			DataPath: tmpDir,
			Config:   DefaultImageGCConfig(),
		}

		result, err := watchdog.RunBlobGC(context.Background())
		r.NoError(err)
		r.NotContains(result.DeletedBlobs, "sha256:recent")
		r.Contains(result.DeletedBlobs, "sha256:old")
		r.Equal(1, result.RetainedBlobs)
	})
}
