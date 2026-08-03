package release

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type downloadTarEntry struct {
	header  tar.Header
	content []byte
}

func writeDownloadTar(t *testing.T, entries []downloadTarEntry) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "artifact.tar.gz")
	file, err := os.Create(path)
	require.NoError(t, err)

	gw := gzip.NewWriter(file)
	tw := tar.NewWriter(gw)
	for _, entry := range entries {
		header := entry.header
		header.Size = int64(len(entry.content))
		require.NoError(t, tw.WriteHeader(&header))
		if len(entry.content) > 0 {
			_, err := tw.Write(entry.content)
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	require.NoError(t, file.Close())

	return path
}

func TestExtractTarGzRejectsEscapingEntries(t *testing.T) {
	tests := []struct {
		name       string
		headerName func(parent string) string
	}{
		{
			name:       "parent traversal",
			headerName: func(string) string { return "../outside" },
		},
		{
			name:       "embedded traversal",
			headerName: func(string) string { return "nested/../../outside" },
		},
		{
			name: "absolute path",
			headerName: func(parent string) string {
				return filepath.Join(parent, "outside")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := t.TempDir()
			target := filepath.Join(parent, "target")
			require.NoError(t, os.Mkdir(target, 0755))

			archive := writeDownloadTar(t, []downloadTarEntry{
				{
					header: tar.Header{
						Name:     tt.headerName(parent),
						Typeflag: tar.TypeReg,
						Mode:     0644,
					},
					content: []byte("escaped"),
				},
			})

			d := &assetDownloader{}
			_, err := d.extractTarGz(archive, target)
			require.ErrorContains(t, err, "invalid tar entry")

			// The traversal targets sit one level above the extraction temp dir,
			// so check both the target dir and its parent.
			_, err = os.Stat(filepath.Join(parent, "outside"))
			require.ErrorIs(t, err, os.ErrNotExist)
			_, err = os.Stat(filepath.Join(target, "outside"))
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

func TestExtractTarGzFindsBinary(t *testing.T) {
	target := t.TempDir()
	archive := writeDownloadTar(t, []downloadTarEntry{
		{header: tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0755}},
		{header: tar.Header{Name: "bin", Typeflag: tar.TypeDir, Mode: 0755}},
		{
			header:  tar.Header{Name: "miren", Typeflag: tar.TypeReg, Mode: 0755},
			content: []byte("binary"),
		},
	})

	d := &assetDownloader{}
	path, err := d.extractTarGz(archive, target)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(target, "miren.new"), path)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, []byte("binary"), content)
}
