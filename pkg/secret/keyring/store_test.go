package keyring

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLog() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestEnsureGeneratesThenReloadsTheSameRing(t *testing.T) {
	dir := t.TempDir()

	first, err := Ensure(testLog(), dir)
	require.NoError(t, err)

	second, err := Ensure(testLog(), dir)
	require.NoError(t, err)

	assert.Equal(t, first.CurrentID(), second.CurrentID())
	assert.Equal(t, first.Keys(), second.Keys())
}

// A value sealed before a restart has to open after it, which is the whole
// reason the ring is persisted at all.
func TestEnsureReloadsAKeyThatCanOpenExistingValues(t *testing.T) {
	dir := t.TempDir()

	first, err := Ensure(testLog(), dir)
	require.NoError(t, err)

	value := []byte("sk_live")
	sealed, err := first.Seal(value)
	require.NoError(t, err)

	reloaded, err := Ensure(testLog(), dir)
	require.NoError(t, err)

	got, err := reloaded.Open(sealed)
	require.NoError(t, err)
	assert.Equal(t, value, got)
}

func TestEnsureWritesTheKeyringOwnerOnly(t *testing.T) {
	dir := t.TempDir()

	_, err := Ensure(testLog(), dir)
	require.NoError(t, err)

	info, err := os.Stat(Path(dir))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(fileMode), info.Mode().Perm())
}

// Regenerating over a keyring that exists but cannot be parsed would orphan
// every secret the cluster holds, so it must fail instead.
func TestEnsureRefusesToRegenerateOverACorruptKeyring(t *testing.T) {
	dir := t.TempDir()
	path := Path(dir)

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte("not json"), fileMode))

	_, err := Ensure(testLog(), dir)
	require.Error(t, err)

	// The unreadable file is still there rather than replaced.
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, "not json", string(data))
}

func TestEnsureRejectsAnUnsupportedStoreVersion(t *testing.T) {
	dir := t.TempDir()
	path := Path(dir)

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(`{"version":99,"current":"a","keys":[]}`), fileMode))

	_, err := Ensure(testLog(), dir)
	assert.Error(t, err)
}

func TestSaveRoundTripsARotatedRing(t *testing.T) {
	dir := t.TempDir()
	path := Path(dir)

	ring, err := Generate()
	require.NoError(t, err)
	rotated, _, err := ring.Rotate()
	require.NoError(t, err)

	require.NoError(t, Save(path, rotated))

	loaded, err := Ensure(testLog(), dir)
	require.NoError(t, err)

	assert.Equal(t, rotated.CurrentID(), loaded.CurrentID())
	assert.Len(t, loaded.Keys(), 2)
}

// Save replaces the file atomically, so a reader never observes a truncated
// ring — which would be indistinguishable from key loss.
func TestSaveLeavesNoTemporaryFilesBehind(t *testing.T) {
	dir := t.TempDir()
	path := Path(dir)

	ring, err := Generate()
	require.NoError(t, err)
	require.NoError(t, Save(path, ring))

	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, Filename, entries[0].Name())
}
