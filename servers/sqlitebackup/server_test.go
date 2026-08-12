package sqlitebackup

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superfly/ltx"

	"miren.dev/runtime/api/sqlitebackup/sqlitebackup_v1alpha"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/rpc/stream"
)

// newTestClient wires an in-process client to a server rooted at a temp dir.
func newTestClient(t *testing.T) (*sqlitebackup_v1alpha.SqliteBackupClient, string) {
	t.Helper()

	root := t.TempDir()
	srv, err := NewServer(slog.New(slog.NewTextHandler(io.Discard, nil)), root)
	require.NoError(t, err)

	client := &sqlitebackup_v1alpha.SqliteBackupClient{
		Client: rpc.LocalClient(sqlitebackup_v1alpha.AdaptSqliteBackup(srv)),
	}
	return client, root
}

// buildLTX produces a real single-page LTX file so the server's header parsing
// is exercised rather than stubbed.
func buildLTX(t *testing.T, minTXID, maxTXID ltx.TXID, ts time.Time) []byte {
	t.Helper()

	const pageSize = 512

	var buf bytes.Buffer
	enc, err := ltx.NewEncoder(&buf)
	require.NoError(t, err)

	hdr := ltx.Header{
		Version:   ltx.Version,
		PageSize:  pageSize,
		Commit:    1,
		MinTXID:   minTXID,
		MaxTXID:   maxTXID,
		Timestamp: ts.UnixMilli(),
	}
	// Only a file starting at TXID 1 is a snapshot; anything later has to
	// declare the checksum of the database it expects to be applied onto.
	if minTXID > 1 {
		hdr.PreApplyChecksum = ltx.ChecksumFlag | 1
	}
	require.NoError(t, enc.EncodeHeader(hdr))

	page := make([]byte, pageSize)
	copy(page, "miren sqlite backup test page")
	require.NoError(t, enc.EncodePage(ltx.PageHeader{Pgno: 1}, page))

	enc.SetPostApplyChecksum(ltx.ChecksumFlag | 1)
	require.NoError(t, enc.Close())

	return buf.Bytes()
}

func writeFile(t *testing.T, client *sqlitebackup_v1alpha.SqliteBackupClient, key string, level int32, minTXID, maxTXID ltx.TXID, data []byte) *sqlitebackup_v1alpha.LTXFile {
	t.Helper()

	ctx := context.Background()
	res, err := client.WriteLTXFile(ctx, key, level, uint64(minTXID), uint64(maxTXID),
		stream.ServeReader(ctx, bytes.NewReader(data)))
	require.NoError(t, err)
	return res.File()
}

func readFile(t *testing.T, client *sqlitebackup_v1alpha.SqliteBackupClient, key string, level int32, minTXID, maxTXID ltx.TXID, offset, size int64) []byte {
	t.Helper()

	ctx := context.Background()
	var out bytes.Buffer
	_, err := client.OpenLTXFile(ctx, key, level, uint64(minTXID), uint64(maxTXID), offset, size,
		stream.ServeWriter(ctx, &out))
	require.NoError(t, err)
	return out.Bytes()
}

func TestWriteAndOpenRoundTrip(t *testing.T) {
	client, root := newTestClient(t)

	ts := time.UnixMilli(time.Now().UnixMilli()).UTC()
	data := buildLTX(t, 1, 1, ts)

	info := writeFile(t, client, "app-demo", 0, 1, 1, data)
	require.Equal(t, int32(0), info.Level())
	require.Equal(t, uint64(1), info.MinTxid())
	require.Equal(t, uint64(1), info.MaxTxid())
	require.Equal(t, int64(len(data)), info.Size())
	require.True(t, ts.Equal(standard.FromTimestamp(info.CreatedAt())),
		"CreatedAt should come from the LTX header timestamp")

	require.Equal(t, data, readFile(t, client, "app-demo", 0, 1, 1, 0, 0))

	// Layout must match litestream's file replica client so the two are comparable.
	path := filepath.Join(root, "app-demo", "level-0", ltx.FormatFilename(1, 1))
	st, err := os.Stat(path)
	require.NoError(t, err, "expected file at litestream-compatible path %s", path)
	require.True(t, ts.Equal(st.ModTime().UTC()), "mtime should carry the transaction timestamp")

	// No temp file should survive a successful write.
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestOpenLTXFileOffsetAndSize(t *testing.T) {
	client, _ := newTestClient(t)

	data := buildLTX(t, 1, 1, time.Now())
	writeFile(t, client, "app-demo", 0, 1, 1, data)

	require.Equal(t, data[10:], readFile(t, client, "app-demo", 0, 1, 1, 10, 0),
		"offset should skip leading bytes")
	require.Equal(t, data[:16], readFile(t, client, "app-demo", 0, 1, 1, 0, 16),
		"size should bound the read")
	require.Equal(t, data[10:26], readFile(t, client, "app-demo", 0, 1, 1, 10, 16),
		"offset and size should compose")
}

func TestListLTXFiles(t *testing.T) {
	client, _ := newTestClient(t)
	ctx := context.Background()

	// Write out of order to prove the server sorts by transaction ID.
	for _, txid := range []ltx.TXID{3, 1, 2} {
		writeFile(t, client, "app-demo", 0, txid, txid, buildLTX(t, txid, txid, time.Now()))
	}
	// A different level and a different key must not leak into results.
	writeFile(t, client, "app-demo", 1, 1, 3, buildLTX(t, 1, 3, time.Now()))
	writeFile(t, client, "app-other", 0, 9, 9, buildLTX(t, 9, 9, time.Now()))

	res, err := client.ListLTXFiles(ctx, "app-demo", 0, 0)
	require.NoError(t, err)
	files := res.Files()
	require.Len(t, files, 3)
	require.Equal(t, []uint64{1, 2, 3}, []uint64{files[0].MinTxid(), files[1].MinTxid(), files[2].MinTxid()})

	// seek skips everything below the given transaction ID.
	res, err = client.ListLTXFiles(ctx, "app-demo", 0, 2)
	require.NoError(t, err)
	require.Len(t, res.Files(), 2)

	// An untouched level reads as empty, not as an error.
	res, err = client.ListLTXFiles(ctx, "app-demo", 7, 0)
	require.NoError(t, err)
	require.Empty(t, res.Files())

	// An entirely unknown key likewise.
	res, err = client.ListLTXFiles(ctx, "app-missing", 0, 0)
	require.NoError(t, err)
	require.Empty(t, res.Files())
}

func TestDeleteLTXFiles(t *testing.T) {
	client, _ := newTestClient(t)
	ctx := context.Background()

	for _, txid := range []ltx.TXID{1, 2} {
		writeFile(t, client, "app-demo", 0, txid, txid, buildLTX(t, txid, txid, time.Now()))
	}

	var target sqlitebackup_v1alpha.LTXFile
	target.SetLevel(0)
	target.SetMinTxid(1)
	target.SetMaxTxid(1)

	// A file that was never written is included to prove deletes are idempotent;
	// litestream retries retention passes and must be able to finish a partial one.
	var absent sqlitebackup_v1alpha.LTXFile
	absent.SetLevel(0)
	absent.SetMinTxid(99)
	absent.SetMaxTxid(99)

	_, err := client.DeleteLTXFiles(ctx, "app-demo", []*sqlitebackup_v1alpha.LTXFile{&target, &absent})
	require.NoError(t, err)

	res, err := client.ListLTXFiles(ctx, "app-demo", 0, 0)
	require.NoError(t, err)
	require.Len(t, res.Files(), 1)
	require.Equal(t, uint64(2), res.Files()[0].MinTxid())
}

func TestDeleteAll(t *testing.T) {
	client, root := newTestClient(t)
	ctx := context.Background()

	writeFile(t, client, "app-demo", 0, 1, 1, buildLTX(t, 1, 1, time.Now()))
	writeFile(t, client, "app-other", 0, 1, 1, buildLTX(t, 1, 1, time.Now()))

	_, err := client.DeleteAll(ctx, "app-demo")
	require.NoError(t, err)

	require.NoDirExists(t, filepath.Join(root, "app-demo"))
	require.DirExists(t, filepath.Join(root, "app-other"), "other keys must be untouched")

	// Deleting a key that holds nothing is not an error.
	_, err = client.DeleteAll(ctx, "app-demo")
	require.NoError(t, err)
}

// Backup keys become directory names, so a key that could escape the root must
// be refused outright rather than sanitized into something surprising.
func TestRejectsUnsafeKeys(t *testing.T) {
	client, root := newTestClient(t)
	ctx := context.Background()

	for _, key := range []string{"", "..", "../escape", "a/b", "/abs", ".hidden", "trailing/"} {
		t.Run("key="+key, func(t *testing.T) {
			_, err := client.ListLTXFiles(ctx, key, 0, 0)
			require.Error(t, err, "key %q should be rejected", key)

			_, err = client.DeleteAll(ctx, key)
			require.Error(t, err, "key %q should be rejected", key)
		})
	}

	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Empty(t, entries, "no rejected key should have created anything")
}

func TestRejectsNegativeLevel(t *testing.T) {
	client, _ := newTestClient(t)

	_, err := client.ListLTXFiles(context.Background(), "app-demo", -1, 0)
	require.Error(t, err)
}

// A body that is not an LTX file must be refused before anything lands on disk.
func TestWriteRejectsNonLTXBody(t *testing.T) {
	client, root := newTestClient(t)
	ctx := context.Background()

	_, err := client.WriteLTXFile(ctx, "app-demo", 0, 1, 1,
		stream.ServeReader(ctx, bytes.NewReader([]byte("this is not an ltx file"))))
	require.Error(t, err)

	require.NoFileExists(t, filepath.Join(root, "app-demo", "level-0", ltx.FormatFilename(1, 1)))
}

// Replication re-sends a transaction range after a retry, so an overwrite must
// fully replace the previous contents.
func TestWriteReplacesExistingFile(t *testing.T) {
	client, _ := newTestClient(t)

	writeFile(t, client, "app-demo", 0, 1, 1, buildLTX(t, 1, 1, time.Now()))

	newer := buildLTX(t, 1, 1, time.Now().Add(time.Hour))
	writeFile(t, client, "app-demo", 0, 1, 1, newer)

	require.Equal(t, newer, readFile(t, client, "app-demo", 0, 1, 1, 0, 0))

	res, err := client.ListLTXFiles(context.Background(), "app-demo", 0, 0)
	require.NoError(t, err)
	require.Len(t, res.Files(), 1, "overwrite must not leave a duplicate")
}

// Two writers racing on one transaction range must not corrupt each other. They
// write to distinct temp files and both rename onto the same final path, so the
// result is one whole file rather than an interleaved or truncated one.
func TestConcurrentWritesToSameRange(t *testing.T) {
	client, root := newTestClient(t)

	data := buildLTX(t, 1, 1, time.Now())

	const writers = 8
	errs := make(chan error, writers)
	start := make(chan struct{})
	for i := 0; i < writers; i++ {
		go func() {
			ctx := context.Background()
			<-start
			_, err := client.WriteLTXFile(ctx, "app-demo", 0, 1, 1,
				stream.ServeReader(ctx, bytes.NewReader(data)))
			errs <- err
		}()
	}
	close(start)

	for i := 0; i < writers; i++ {
		require.NoError(t, <-errs, "concurrent writes to one range should all succeed")
	}

	// Exactly one stored file, byte-identical to what was sent, and no temp
	// files left behind by the writers that lost the race.
	require.Equal(t, data, readFile(t, client, "app-demo", 0, 1, 1, 0, 0))

	entries, err := os.ReadDir(filepath.Join(root, "app-demo", "level-0"))
	require.NoError(t, err)
	require.Len(t, entries, 1, "expected only the final file, got %v", entryNames(entries))
	require.Equal(t, ltx.FormatFilename(1, 1), entries[0].Name())
}

// A failed write must not leave its temp file behind.
func TestFailedWriteLeavesNoTempFile(t *testing.T) {
	client, root := newTestClient(t)
	ctx := context.Background()

	// Write one good file so the level directory exists.
	writeFile(t, client, "app-demo", 0, 1, 1, buildLTX(t, 1, 1, time.Now()))

	_, err := client.WriteLTXFile(ctx, "app-demo", 0, 2, 2,
		stream.ServeReader(ctx, bytes.NewReader([]byte("not an ltx file"))))
	require.Error(t, err)

	entries, err := os.ReadDir(filepath.Join(root, "app-demo", "level-0"))
	require.NoError(t, err)
	require.Len(t, entries, 1, "failed write should leave nothing behind, got %v", entryNames(entries))
}

// Debris from a crashed write is reclaimed, but a temp file young enough to
// belong to an in-flight upload is left alone.
func TestListReclaimsStaleTempFilesOnly(t *testing.T) {
	client, root := newTestClient(t)
	ctx := context.Background()

	writeFile(t, client, "app-demo", 0, 1, 1, buildLTX(t, 1, 1, time.Now()))
	levelDir := filepath.Join(root, "app-demo", "level-0")

	stale := filepath.Join(levelDir, tmpFilePrefix+"stale")
	require.NoError(t, os.WriteFile(stale, []byte("debris"), 0o644))
	old := time.Now().Add(-2 * staleTmpAge)
	require.NoError(t, os.Chtimes(stale, old, old))

	fresh := filepath.Join(levelDir, tmpFilePrefix+"inflight")
	require.NoError(t, os.WriteFile(fresh, []byte("in progress"), 0o644))

	res, err := client.ListLTXFiles(ctx, "app-demo", 0, 0)
	require.NoError(t, err)
	require.Len(t, res.Files(), 1, "temp files must never be listed as stored files")

	require.NoFileExists(t, stale, "a temp file older than staleTmpAge is crash debris")
	require.FileExists(t, fresh, "a recent temp file may belong to a live write")
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
