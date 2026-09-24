package build

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportStaticSource(t *testing.T) {
	source := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(source, ".miren"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(source, ".miren", "app.toml"), []byte("secret config"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(source, "docs"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "index.html"), []byte("home"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(source, "docs", "guide.txt"), []byte("guide"), 0644))
	require.NoError(t, os.Symlink("/etc/passwd", filepath.Join(source, "escape")))

	destination := filepath.Join(t.TempDir(), "static.tar")
	require.NoError(t, exportStaticSource(source, "/app", destination))

	archive, err := os.Open(destination)
	require.NoError(t, err)
	defer archive.Close()
	files := make(map[string]string)
	reader := tar.NewReader(archive)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if header.Typeflag != tar.TypeReg {
			continue
		}
		contents, err := io.ReadAll(reader)
		require.NoError(t, err)
		files[header.Name] = string(contents)
	}
	assert.Equal(t, map[string]string{
		"docs/guide.txt": "guide",
		"index.html":     "home",
	}, files)
}

func TestCanonicalizeStaticArchiveRemovesVolatileMetadata(t *testing.T) {
	writeArchive := func(t *testing.T, modTime time.Time, uid int) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "input.tar")
		file, err := os.Create(path)
		require.NoError(t, err)
		archive := tar.NewWriter(file)
		body := []byte("generated output")
		require.NoError(t, archive.WriteHeader(&tar.Header{
			Name:     "index.html",
			Mode:     0644,
			Size:     int64(len(body)),
			Typeflag: tar.TypeReg,
			ModTime:  modTime,
			Uid:      uid,
			Uname:    "builder",
		}))
		_, err = archive.Write(body)
		require.NoError(t, err)
		require.NoError(t, archive.Close())
		require.NoError(t, file.Close())
		return path
	}

	first := writeArchive(t, time.Unix(100, 0), 1000)
	second := writeArchive(t, time.Unix(200, 0), 2000)
	firstCanonical := filepath.Join(t.TempDir(), "canonical.tar")
	secondCanonical := filepath.Join(t.TempDir(), "canonical.tar")
	require.NoError(t, canonicalizeStaticArchive(first, firstCanonical, ""))
	require.NoError(t, canonicalizeStaticArchive(second, secondCanonical, ""))
	firstBytes, err := os.ReadFile(firstCanonical)
	require.NoError(t, err)
	secondBytes, err := os.ReadFile(secondCanonical)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(firstBytes, secondBytes), "canonical archives should be byte-identical")
}

func TestStaticErrorPageMustBeInExport(t *testing.T) {
	source := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(source, "errors"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "errors", "page.html"), []byte("<h1>{{.Title}}</h1>"), 0644))
	archive := filepath.Join(t.TempDir(), "static.tar")
	require.NoError(t, exportStaticSource(source, "/app", archive))
	canonical := filepath.Join(t.TempDir(), "canonical.tar")
	require.NoError(t, canonicalizeStaticArchive(archive, canonical, "errors/page.html"))
	assert.ErrorContains(t, canonicalizeStaticArchive(archive, canonical, "errors/missing.html"), "not found in static.dir")

	require.NoError(t, os.WriteFile(filepath.Join(source, "errors", "page.html"), bytes.Repeat([]byte("x"), (128<<10)+1), 0644))
	require.NoError(t, exportStaticSource(source, "/app", archive))
	assert.ErrorContains(t, canonicalizeStaticArchive(archive, canonical, "errors/page.html"), "exceeds 128 KiB")
}

func TestExportStaticSourceRejectsPathsOutsideSourceRoot(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "static.tar")

	err := exportStaticSource(t.TempDir(), "/site", destination)
	require.ErrorContains(t, err, "must be within /app")
}
