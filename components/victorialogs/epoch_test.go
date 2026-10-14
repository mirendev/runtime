//go:build linux

package victorialogs

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func inode(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Sys().(*syscall.Stat_t).Ino
}

func TestSeedEpochLinksDataAndLeavesPreviousIntact(t *testing.T) {
	root := t.TempDir()
	previous := filepath.Join(root, "victorialogs")
	current := filepath.Join(root, "victorialogs-v2")
	part := filepath.Join("partitions", "20261014", "datadb", "18DE7056F2432E5D")
	writeFile(t, filepath.Join(previous, part, "metadata.json"), `{"FormatVersion":1}`)
	writeFile(t, filepath.Join(previous, "partitions", "20261014", "datadb", "parts.json"), `["18DE7056F2432E5D"]`)
	writeFile(t, filepath.Join(previous, "partitions", "20261014", "datadb", "parts.json.tmp.1"), "torn write")
	writeFile(t, filepath.Join(previous, "flock.lock"), "")
	writeFile(t, filepath.Join(previous, componentSpecFile), "sha256:stale\n")
	before := directoryHashes(t, previous)

	stopped := 0
	seeded, err := seedEpoch(slog.New(slog.NewTextHandler(io.Discard, nil)), previous, current, func() error { stopped++; return nil })
	require.NoError(t, err)
	require.True(t, seeded)
	require.Equal(t, 1, stopped, "writers must stop before anything is linked")
	require.Equal(t, inode(t, filepath.Join(previous, part, "metadata.json")), inode(t, filepath.Join(current, part, "metadata.json")))
	require.NoFileExists(t, filepath.Join(current, "flock.lock"), "a shared lock inode would tie the two stores together")
	require.NoFileExists(t, filepath.Join(current, "partitions", "20261014", "datadb", "parts.json.tmp.1"))
	require.NoFileExists(t, filepath.Join(current, componentSpecFile), "the previous epoch's spec describes a different directory")
	require.FileExists(t, filepath.Join(current, seedStateFile))
	require.Equal(t, before, directoryHashes(t, previous))
}

func TestSeedEpochSkipsFreshInstall(t *testing.T) {
	root := t.TempDir()
	seeded, err := seedEpoch(slog.New(slog.NewTextHandler(io.Discard, nil)), filepath.Join(root, "victorialogs"), filepath.Join(root, "victorialogs-v2"), func() error {
		t.Fatal("nothing to stop on a fresh install")
		return nil
	})
	require.NoError(t, err)
	require.False(t, seeded)
	require.NoDirExists(t, filepath.Join(root, "victorialogs-v2"))
}

func TestSeedEpochKeepsCurrentWhilePreviousIsUnchanged(t *testing.T) {
	root := t.TempDir()
	previous := filepath.Join(root, "victorialogs")
	current := filepath.Join(root, "victorialogs-v2")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	writeFile(t, filepath.Join(previous, "partitions", "p", "parts.json"), "[]")
	_, err := seedEpoch(log, previous, current, func() error { return nil })
	require.NoError(t, err)
	writeFile(t, filepath.Join(current, "partitions", "p", "new-part"), "written by v1.52")
	// Starting v1.0 without writing touches only flock.lock.
	writeFile(t, filepath.Join(previous, "flock.lock"), "")

	seeded, err := seedEpoch(log, previous, current, func() error {
		t.Fatal("an unchanged previous epoch must not stop anything")
		return nil
	})
	require.NoError(t, err)
	require.False(t, seeded)
	require.FileExists(t, filepath.Join(current, "partitions", "p", "new-part"))
}

// A rollback lets the older release write to its own epoch. The next upgrade
// must carry those logs forward rather than resume the store it left behind.
func TestSeedEpochReseedsAfterOlderReleaseWrites(t *testing.T) {
	root := t.TempDir()
	previous := filepath.Join(root, "victorialogs")
	current := filepath.Join(root, "victorialogs-v2")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	writeFile(t, filepath.Join(previous, "partitions", "p", "parts.json"), "[]")
	_, err := seedEpoch(log, previous, current, func() error { return nil })
	require.NoError(t, err)
	writeFile(t, filepath.Join(current, "partitions", "p", "abandoned-part"), "written before the rollback")
	writeFile(t, filepath.Join(previous, "partitions", "p", "rollback-part"), "written by v1.0 after the rollback")

	seeded, err := seedEpoch(log, previous, current, func() error { return nil })
	require.NoError(t, err)
	require.True(t, seeded)
	require.FileExists(t, filepath.Join(current, "partitions", "p", "rollback-part"))
	require.NoFileExists(t, filepath.Join(current, "partitions", "p", "abandoned-part"))
	replaced, err := filepath.Glob(current + ".replaced-*")
	require.NoError(t, err)
	require.Len(t, replaced, 1)
	require.FileExists(t, filepath.Join(replaced[0], "partitions", "p", "abandoned-part"), "the abandoned store is set aside, not deleted")
}
