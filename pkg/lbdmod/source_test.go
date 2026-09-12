package lbdmod

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSourceVersionMatchesTheCheckedInTree(t *testing.T) {
	// hack/sync-lbd-src.sh writes VERSION beside the source it copied, and CI
	// checks the tree against go.mod. If this is empty the embed is broken.
	version := SourceVersion()
	require.NotEmpty(t, version)
	assert.NotEqual(t, "unknown", version)
}

func TestMaterializeSourceWritesABuildableTree(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src")
	require.NoError(t, materializeSource(dir))

	// Everything the kernel build and lbdctl need, including the vendored LZ4
	// in its subdirectory.
	for _, name := range []string{
		"Makefile",
		"dkms.conf",
		"lbd_main.c",
		"lbd_qcow2.c",
		"lbdctl.c",
		"lbd.h",
		"lz4_kcompat.h",
		"lz4/lz4.c",
		"lz4/lz4.h",
	} {
		info, err := os.Stat(filepath.Join(dir, name))
		require.NoError(t, err, "missing %s", name)
		assert.Positive(t, info.Size(), "%s is empty", name)
	}

	// The build system writes object files next to the source, so the tree has
	// to be writable -- it cannot be a read-only mount of the embed.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lbd.o"), []byte("x"), 0644))

	// The Makefile must still carry the kernel-version probe that lets the
	// module build against recent kernels.
	makefile, err := os.ReadFile(filepath.Join(dir, "Makefile"))
	require.NoError(t, err)
	assert.Contains(t, string(makefile), "LBD_RENAME_PARENT")
	assert.Contains(t, string(makefile), "obj-m := lbd.o")
}

func TestMaterializeSourceIsRepeatable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src")
	require.NoError(t, materializeSource(dir))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lbd_main.c"), []byte("clobbered"), 0644))

	// A retry after a failed build has to restore the source it overwrote.
	require.NoError(t, materializeSource(dir))
	data, err := os.ReadFile(filepath.Join(dir, "lbd_main.c"))
	require.NoError(t, err)
	assert.NotEqual(t, "clobbered", string(data))
}
