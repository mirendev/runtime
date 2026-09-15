package containerboot

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func newBoot(t *testing.T, image string) Boot {
	t.Helper()
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "image-miren")
	require.NoError(t, os.WriteFile(imagePath, []byte(image), 0755))
	return Boot{
		ReleaseDir:  filepath.Join(dir, "release"),
		ImageBinary: imagePath,
		Log:         slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
}

func releaseContents(t *testing.T, b Boot) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(b.ReleaseDir, "miren"))
	require.NoError(t, err)
	return string(data)
}

func marker(t *testing.T, b Boot) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(b.ReleaseDir, imageMarker))
	require.NoError(t, err)
	return string(data)
}

func TestPrepareSeedsFreshVolume(t *testing.T) {
	b := newBoot(t, "image-v1")
	// The image bakes the bundle into the volume before the first boot, so the
	// binary is there but nothing says which image put it there.
	require.NoError(t, os.MkdirAll(b.ReleaseDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(b.ReleaseDir, "miren"), []byte("image-v1"), 0755))

	bin, err := b.Prepare(context.Background())
	require.NoError(t, err)
	require.Equal(t, filepath.Join(b.ReleaseDir, "miren"), bin)
	require.Equal(t, "image-v1", releaseContents(t, b))
	require.NotEmpty(t, marker(t, b))

	info, err := os.Stat(bin)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0755), info.Mode().Perm())
}

func TestPrepareKeepsInPlaceUpgrade(t *testing.T) {
	b := newBoot(t, "image-v1")
	_, err := b.Prepare(context.Background())
	require.NoError(t, err)
	first := marker(t, b)

	// An upgrade replaced the volume's binary while the image stayed put.
	require.NoError(t, os.WriteFile(filepath.Join(b.ReleaseDir, "miren"), []byte("upgraded-v2"), 0755))

	bin, err := b.Prepare(context.Background())
	require.NoError(t, err)
	require.Equal(t, filepath.Join(b.ReleaseDir, "miren"), bin)
	require.Equal(t, "upgraded-v2", releaseContents(t, b), "the volume's binary is authoritative when the image has not changed")
	require.Equal(t, first, marker(t, b))
}

func TestPrepareReseedsWhenImageChanges(t *testing.T) {
	b := newBoot(t, "image-v1")
	_, err := b.Prepare(context.Background())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(b.ReleaseDir, "miren"), []byte("upgraded-v2"), 0755))

	// A reinstall with a different image, against the same volume. Whether the
	// image is newer or older, it is what the operator asked to run.
	require.NoError(t, os.WriteFile(b.ImageBinary, []byte("image-v3"), 0755))
	bin, err := b.Prepare(context.Background())
	require.NoError(t, err)
	require.Equal(t, filepath.Join(b.ReleaseDir, "miren"), bin)
	require.Equal(t, "image-v3", releaseContents(t, b))

	// Now the marker matches the new image, so a further in-place upgrade sticks.
	require.NoError(t, os.WriteFile(filepath.Join(b.ReleaseDir, "miren"), []byte("upgraded-v4"), 0755))
	_, err = b.Prepare(context.Background())
	require.NoError(t, err)
	require.Equal(t, "upgraded-v4", releaseContents(t, b))
}

func TestPrepareRestoresMissingBinary(t *testing.T) {
	b := newBoot(t, "image-v1")
	_, err := b.Prepare(context.Background())
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(b.ReleaseDir, "miren")))

	bin, err := b.Prepare(context.Background())
	require.NoError(t, err)
	require.Equal(t, "image-v1", releaseContents(t, b))
	require.Equal(t, filepath.Join(b.ReleaseDir, "miren"), bin)
}

func TestPrepareLeavesOtherFilesAlone(t *testing.T) {
	b := newBoot(t, "image-v1")
	require.NoError(t, os.MkdirAll(b.ReleaseDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(b.ReleaseDir, "containerd"), []byte("containerd"), 0755))

	_, err := b.Prepare(context.Background())
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(b.ReleaseDir, "containerd"))
	require.NoError(t, err)
	require.Equal(t, "containerd", string(data))

	entries, err := os.ReadDir(b.ReleaseDir)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	require.ElementsMatch(t, []string{"miren", "containerd", imageMarker}, names, "no staging files left behind")
}
