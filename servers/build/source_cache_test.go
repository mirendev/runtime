package build

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
	"miren.dev/runtime/pkg/tarx"
)

func TestPrepareUploadRetainedSourceRoundtrip(t *testing.T) {
	ctx := t.Context()
	h := newDeploySagaHarness(t)
	h.builder.DataPath = t.TempDir()
	h.builder.ingressClient = ingress.NewClient(h.builder.Log, h.inmem.EAC.Client)
	s := &SagaBuilder{inner: h.builder, executor: h.executor, streams: h.streams, statuses: h.statuses, log: h.builder.Log}
	client := build_v1alpha.NewBuilderClient(rpc.LocalClient(build_v1alpha.AdaptBuilder(s)))
	status := stream.Callback(func(*build_v1alpha.Status) error { return nil })
	files := dockerfileTarball(t)
	files["lib/keep.txt"] = "unchanged"
	files["changed.txt"] = "old"
	files["removed.txt"] = "deleted locally"
	_, err := client.BuildFromTar(ctx, "demo", stream.ServeReader(ctx, makeTar(t, files)), status, nil, "", "", nil)
	require.NoError(t, err)
	var app core_v1alpha.App
	require.NoError(t, h.builder.ec.Get(ctx, "demo", &app))
	originalID := app.ActiveVersion

	// A newer preview must not displace the active source used for uploads.
	files["lib/keep.txt"] = "preview"
	_, err = client.BuildFromTar(ctx, "demo", stream.ServeReader(ctx, makeTar(t, files)), status, nil, "preview", "24h", nil)
	require.NoError(t, err)
	files["lib/keep.txt"] = "unchanged"
	files["changed.txt"] = "new" // Same size, different hash.
	files["added.txt"] = "brand new"
	delete(files, "removed.txt")
	manifest := make([]*build_v1alpha.FileManifestEntry, 0, len(files))
	for path, body := range files {
		manifest = append(manifest, sourceManifestEntry(path, body, 0755))
	}
	prepared, err := client.PrepareUpload(ctx, "demo", manifest)
	require.NoError(t, err)
	result := prepared.Result()
	require.Equal(t, int32(len(files)-2), result.CachedCount())
	require.ElementsMatch(t, []string{"changed.txt", "added.txt"}, *result.NeededPaths())

	_, err = client.BuildFromPrepared(ctx, result.SessionId(), stream.ServeReader(ctx, makeTar(t, map[string]string{
		"changed.txt": "new", "added.txt": "brand new",
	})), status, nil, "", "", nil)
	require.NoError(t, err)
	require.NoError(t, h.builder.ec.Get(ctx, "demo", &app))
	var version core_v1alpha.AppVersion
	require.NoError(t, h.builder.ec.GetById(ctx, app.ActiveVersion, &version))
	archive, err := h.builder.openSource(version.SourceArchive)
	require.NoError(t, err)
	defer archive.Close()
	gz, err := gzip.NewReader(archive)
	require.NoError(t, err)
	defer gz.Close()
	contents, err := tarx.TarToMap(gz)
	require.NoError(t, err)
	want := make(map[string][]byte, len(files))
	for path, body := range files {
		want[path] = []byte(body)
	}
	require.Equal(t, want, contents)
	require.NoDirExists(t, filepath.Join(h.builder.DataPath, "source-code"))

	// Following a rollback, the original archive becomes the upload source.
	app.ActiveVersion = originalID
	require.NoError(t, h.builder.ec.Update(ctx, &app))
	prepared, err = client.PrepareUpload(ctx, "demo", []*build_v1alpha.FileManifestEntry{sourceManifestEntry("changed.txt", "old", 0700)})
	require.NoError(t, err)
	require.Equal(t, int32(1), prepared.Result().CachedCount())
	require.Empty(t, *prepared.Result().NeededPaths())
	val, ok := h.builder.sessions.LoadAndDelete(prepared.Result().SessionId())
	require.True(t, ok)
	sess := val.(*buildSession)
	sess.cancelFunc()
	info, err := os.Stat(filepath.Join(sess.dir, "changed.txt"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0700), info.Mode().Perm(), "mode-only changes reuse bytes with client permissions")
}

func TestPrepareUploadUnavailableSource(t *testing.T) {
	for _, kind := range []string{"new app", "no active version", "legacy", "missing", "corrupt header", "corrupt checksum"} {
		t.Run(kind, func(t *testing.T) {
			ctx := t.Context()
			h := newDeploySagaHarness(t)
			h.builder.DataPath = t.TempDir()
			if kind != "new app" {
				var app core_v1alpha.App
				if kind != "no active version" {
					var version core_v1alpha.AppVersion
					if kind != "legacy" {
						dir := t.TempDir()
						require.NoError(t, os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("cached bytes"), 0644))
						archive, err := h.builder.retainSource(dir)
						require.NoError(t, err)
						version.SourceArchive = archive
						path := filepath.Join(h.builder.DataPath, "build-sources", archive+".tar.gz")
						switch kind {
						case "missing":
							require.NoError(t, os.Remove(path))
						case "corrupt header":
							require.NoError(t, os.WriteFile(path, []byte("not gzip"), 0600))
						case "corrupt checksum":
							data, err := os.ReadFile(path)
							require.NoError(t, err)
							data[len(data)-8] ^= 0xff // Fail after a matching file has been extracted.
							require.NoError(t, os.WriteFile(path, data, 0600))
						}
					}
					versionID, err := h.builder.ec.Create(ctx, "deployed", &version)
					require.NoError(t, err)
					app.ActiveVersion = versionID
				}
				_, err := h.builder.ec.Create(ctx, "demo", &app)
				require.NoError(t, err)
			}
			client := build_v1alpha.NewBuilderClient(rpc.LocalClient(build_v1alpha.AdaptBuilder(&SagaBuilder{inner: h.builder})))
			prepared, err := client.PrepareUpload(ctx, "demo", []*build_v1alpha.FileManifestEntry{
				sourceManifestEntry("keep.txt", "cached bytes", 0644), sourceManifestEntry("new.txt", "new bytes", 0644),
			})
			require.NoError(t, err)
			result := prepared.Result()
			require.Zero(t, result.CachedCount())
			require.ElementsMatch(t, []string{"keep.txt", "new.txt"}, *result.NeededPaths())
			val, ok := h.builder.sessions.LoadAndDelete(result.SessionId())
			require.True(t, ok)
			sess := val.(*buildSession)
			sess.cancelFunc()
			entries, err := os.ReadDir(sess.dir)
			require.NoError(t, err)
			require.Empty(t, entries, "fallback must discard partial extraction")
		})
	}
}

func sourceManifestEntry(path, body string, mode int32) *build_v1alpha.FileManifestEntry {
	hash := sha256.Sum256([]byte(body))
	entry := &build_v1alpha.FileManifestEntry{}
	entry.SetPath(path)
	entry.SetHash(hex.EncodeToString(hash[:]))
	entry.SetSize(int64(len(body)))
	entry.SetMode(mode)
	return entry
}
