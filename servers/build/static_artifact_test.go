package build

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"testing"

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

func TestExportStaticSourceRejectsPathsOutsideSourceRoot(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "static.tar")

	err := exportStaticSource(t.TempDir(), "/site", destination)
	require.ErrorContains(t, err, "must be within /app")
}
