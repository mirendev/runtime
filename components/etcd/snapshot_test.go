package etcd

import (
	"crypto/sha256"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRestoreArgsMatchMemberIdentity pins the restore command line to the
// same member name and peer URL etcd itself is started with. If these drift,
// etcd refuses to start on the restored data directory, which is exactly the
// failure a rollback cannot afford.
func TestRestoreArgsMatchMemberIdentity(t *testing.T) {
	config := EtcdConfig{Name: "miren-etcd", PeerPort: 12380, InitialToken: "tok"}
	config.applyDefaults()
	etcd := etcdArgs(config, computeTuning(8<<30, 0))
	restore := restoreArgs(config)

	require.Equal(t, []string{"/usr/local/bin/etcdutl", "snapshot", "restore", "/snapshot.db"}, restore[:4])
	require.Equal(t, "/restore/etcd", flagValue(restore, "--data-dir"))
	for _, flag := range []string{"--name", "--initial-cluster", "--initial-advertise-peer-urls", "--initial-cluster-token"} {
		require.Equal(t, flagValue(etcd, flag), flagValue(restore, flag), flag)
	}
	require.Equal(t, "miren-etcd=http://localhost:12380", flagValue(restore, "--initial-cluster"))
}

func TestVerifySnapshot(t *testing.T) {
	dir := t.TempDir()
	db := make([]byte, 4096*3)
	for i := range db {
		db[i] = byte(i)
	}
	sum := sha256.Sum256(db)

	good := filepath.Join(dir, "good.db")
	require.NoError(t, os.WriteFile(good, append(db, sum[:]...), 0o600))
	require.NoError(t, VerifySnapshot(good))

	corrupt := filepath.Join(dir, "corrupt.db")
	flipped := append([]byte(nil), db...)
	flipped[100] ^= 0xff
	require.NoError(t, os.WriteFile(corrupt, append(flipped, sum[:]...), 0o600))
	require.ErrorContains(t, VerifySnapshot(corrupt), "corrupt")

	bare := filepath.Join(dir, "bare.db")
	require.NoError(t, os.WriteFile(bare, db, 0o600))
	require.ErrorContains(t, VerifySnapshot(bare), "no checksum trailer")

	require.ErrorIs(t, VerifySnapshot(filepath.Join(dir, "missing.db")), os.ErrNotExist)
}

func TestBackupPruneKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"01A", "01B", "01C", "01D"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, id+snapshotSuffix), nil, 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "unrelated.txt"), nil, 0o600))

	b := Backup{Dir: dir, Keep: 2, Log: slog.Default()}
	require.NoError(t, b.prune())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	require.Equal(t, []string{"01C.etcd.db", "01D.etcd.db", "unrelated.txt"}, names)
}

func TestPruneReplacedDirsKeepsTheNamedOne(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"etcd.replaced-old1", "etcd.replaced-old2", "etcd.replaced-new", "etcd", "etcd-certs"} {
		require.NoError(t, os.Mkdir(filepath.Join(dir, name), 0o700))
	}
	e := NewEtcdComponent(slog.Default(), nil, "test", dir)
	require.NoError(t, e.pruneReplacedDirs(filepath.Join(dir, "etcd.replaced-new")))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	require.Equal(t, []string{"etcd", "etcd-certs", "etcd.replaced-new"}, names)
}
