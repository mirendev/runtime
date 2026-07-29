package commands

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type releaseTarEntry struct {
	header  tar.Header
	content []byte
}

func writeReleaseTar(t *testing.T, entries []releaseTarEntry) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "release.tar.gz")
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

func TestExtractTarGzRejectsLinks(t *testing.T) {
	tests := []struct {
		name     string
		typeflag byte
		linkname string
	}{
		{
			name:     "escaping symlink",
			typeflag: tar.TypeSymlink,
			linkname: "..",
		},
		{
			name:     "internal symlink",
			typeflag: tar.TypeSymlink,
			linkname: "versions/v1",
		},
		{
			name:     "hard link",
			typeflag: tar.TypeLink,
			linkname: "miren",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archive := writeReleaseTar(t, []releaseTarEntry{
				{
					header: tar.Header{
						Name:     "link",
						Typeflag: tt.typeflag,
						Mode:     0777,
						Linkname: tt.linkname,
					},
				},
			})

			err := extractTarGz(archive, t.TempDir())
			require.ErrorContains(t, err, "unsupported link type")
		})
	}
}

func TestExtractTarGzRejectsPreExistingSymlinkTraversal(t *testing.T) {
	parent := t.TempDir()
	dest := filepath.Join(parent, "dest")
	outside := filepath.Join(parent, "outside")
	require.NoError(t, os.Mkdir(dest, 0755))
	require.NoError(t, os.Mkdir(outside, 0755))
	require.NoError(t, os.Symlink(outside, filepath.Join(dest, "escape")))

	archive := writeReleaseTar(t, []releaseTarEntry{
		{
			header: tar.Header{
				Name:     "escape/file",
				Typeflag: tar.TypeReg,
				Mode:     0644,
			},
			content: []byte("escaped"),
		},
	})

	err := extractTarGz(archive, dest)
	require.ErrorContains(t, err, "traverses symlink")
	_, err = os.Stat(filepath.Join(outside, "file"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestExtractTarGzRejectsSymlinkRoot(t *testing.T) {
	parent := t.TempDir()
	outside := filepath.Join(parent, "outside")
	dest := filepath.Join(parent, "dest")
	require.NoError(t, os.Mkdir(outside, 0755))
	require.NoError(t, os.Symlink(outside, dest))

	archive := writeReleaseTar(t, []releaseTarEntry{
		{
			header: tar.Header{
				Name:     "file",
				Typeflag: tar.TypeReg,
				Mode:     0644,
			},
			content: []byte("escaped"),
		},
	})

	err := extractTarGz(archive, dest)
	require.ErrorContains(t, err, "archive root is a symlink")
	_, err = os.Stat(filepath.Join(outside, "file"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestExtractTarGzRejectsSiblingPrefixTraversal(t *testing.T) {
	parent := t.TempDir()
	dest := filepath.Join(parent, "dest")
	require.NoError(t, os.Mkdir(dest, 0755))

	archive := writeReleaseTar(t, []releaseTarEntry{
		{
			header: tar.Header{
				Name:     "../dest-evil/outside",
				Typeflag: tar.TypeReg,
				Mode:     0644,
			},
			content: []byte("escaped"),
		},
	})

	err := extractTarGz(archive, dest)
	require.ErrorContains(t, err, "invalid tar entry")
	_, err = os.Stat(filepath.Join(parent, "dest-evil", "outside"))
	require.ErrorIs(t, err, os.ErrNotExist)
}
