package commands

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestReleaseBinaryVersion(t *testing.T) {
	writeBinary := func(t *testing.T, content string) string {
		t.Helper()

		releaseDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(releaseDir, "miren"), []byte(content), 0755))
		return releaseDir
	}

	t.Run("returns version output", func(t *testing.T) {
		releaseDir := writeBinary(t, "#!/bin/sh\necho v1.2.3\n")

		out, err := releaseBinaryVersion(context.Background(), releaseDir)
		require.NoError(t, err)
		require.Equal(t, "v1.2.3\n", string(out))
	})

	t.Run("includes stderr on failure", func(t *testing.T) {
		releaseDir := writeBinary(t, "#!/bin/sh\necho broken release >&2\nexit 1\n")

		_, err := releaseBinaryVersion(context.Background(), releaseDir)
		require.ErrorContains(t, err, "broken release")
	})

	t.Run("honors context deadline", func(t *testing.T) {
		releaseDir := writeBinary(t, "#!/bin/sh\nexec sleep 5\n")
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		_, err := releaseBinaryVersion(ctx, releaseDir)
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

func TestPromoteReleaseDir(t *testing.T) {
	writeMarker := func(t *testing.T, dir, content string) {
		t.Helper()
		require.NoError(t, os.Mkdir(dir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "marker"), []byte(content), 0644))
	}

	t.Run("installs a new release", func(t *testing.T) {
		parent := t.TempDir()
		staging := filepath.Join(parent, "staging")
		release := filepath.Join(parent, "release")
		writeMarker(t, staging, "new")

		backup, err := promoteReleaseDir(staging, release, false)
		require.NoError(t, err)
		require.Empty(t, backup)
		content, err := os.ReadFile(filepath.Join(release, "marker"))
		require.NoError(t, err)
		require.Equal(t, []byte("new"), content)
	})

	t.Run("replaces an existing release and keeps a backup", func(t *testing.T) {
		parent := t.TempDir()
		staging := filepath.Join(parent, "staging")
		release := filepath.Join(parent, "release")
		writeMarker(t, staging, "new")
		writeMarker(t, release, "old")

		backup, err := promoteReleaseDir(staging, release, true)
		require.NoError(t, err)
		require.NotEmpty(t, backup)
		content, err := os.ReadFile(filepath.Join(release, "marker"))
		require.NoError(t, err)
		require.Equal(t, []byte("new"), content)
		content, err = os.ReadFile(filepath.Join(backup, "marker"))
		require.NoError(t, err)
		require.Equal(t, []byte("old"), content)
	})

	t.Run("does not replace without force", func(t *testing.T) {
		parent := t.TempDir()
		staging := filepath.Join(parent, "staging")
		release := filepath.Join(parent, "release")
		writeMarker(t, staging, "new")
		writeMarker(t, release, "old")

		_, err := promoteReleaseDir(staging, release, false)
		require.ErrorContains(t, err, "already exists")
		content, err := os.ReadFile(filepath.Join(release, "marker"))
		require.NoError(t, err)
		require.Equal(t, []byte("old"), content)
	})

	t.Run("restores the previous release when promotion fails", func(t *testing.T) {
		parent := t.TempDir()
		staging := filepath.Join(parent, "missing-staging")
		release := filepath.Join(parent, "release")
		writeMarker(t, release, "old")

		backup, err := promoteReleaseDir(staging, release, true)
		require.ErrorContains(t, err, "promoting staged release")
		require.Empty(t, backup)
		content, err := os.ReadFile(filepath.Join(release, "marker"))
		require.NoError(t, err)
		require.Equal(t, []byte("old"), content)
	})
}
