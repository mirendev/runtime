package disk

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A transfer id names a file under a directory the server owns, and it comes
// from the client, so it must not be usable to point somewhere else.
func TestTransferPathRejectsIdsThatEscapeTheDirectory(t *testing.T) {
	s, _, _, _ := newTestServer(t, []byte("hello"))

	for _, id := range []string{
		"../../etc/passwd",
		"a/b",
		"..",
		".",
		`a\b`,
		"a b",
		"a.part",
		"",
	} {
		_, err := s.transferPath(id)
		require.Error(t, err, "id %q should be rejected", id)
	}
}

func TestTransferPathAcceptsOrdinaryIds(t *testing.T) {
	s, _, _, _ := newTestServer(t, []byte("hello"))

	path, err := s.transferPath("0a1b2c3d-4e5f_6789")
	require.NoError(t, err)
	assert.Equal(t,
		filepath.Join(s.diskDataPath(), transfersDir, "0a1b2c3d-4e5f_6789.part"),
		path)
}

func TestTransferOffsetIsZeroForAnUnknownId(t *testing.T) {
	s, _, _, _ := newTestServer(t, []byte("hello"))

	// Zero rather than an error is what lets a client's first attempt and its
	// retries be the same code path.
	f, err := s.openTransfer("never-seen", 0)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

// The two ends disagreeing about how much arrived is exactly when writing
// anyway produces a file of the right length and the wrong bytes.
func TestOpenTransferRefusesAMismatchedOffset(t *testing.T) {
	s, _, _, _ := newTestServer(t, []byte("hello"))

	f, err := s.openTransfer("t1", 0)
	require.NoError(t, err)
	_, err = f.Write([]byte("0123456789"))
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// Resuming from where the server actually is works.
	f, err = s.openTransfer("t1", 10)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// Claiming to be further along does not.
	_, err = s.openTransfer("t1", 25)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at 10 bytes")

	// Neither does claiming to be behind, which would duplicate bytes.
	_, err = s.openTransfer("t1", 4)
	require.Error(t, err)
}

// Appending is what makes a resumed upload continue rather than overwrite.
func TestOpenTransferAppends(t *testing.T) {
	s, _, _, _ := newTestServer(t, []byte("hello"))

	f, err := s.openTransfer("t1", 0)
	require.NoError(t, err)
	_, err = f.Write([]byte("abc"))
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = s.openTransfer("t1", 3)
	require.NoError(t, err)
	_, err = f.Write([]byte("def"))
	require.NoError(t, err)
	require.NoError(t, f.Close())

	path, err := s.transferPath("t1")
	require.NoError(t, err)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "abcdef", string(got))
}

func TestStagingRoundTrips(t *testing.T) {
	s, _, _, _ := newTestServer(t, []byte("hello"))

	path, err := s.transferPath("t1")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte("compressed"), 0600))

	want := &stagedSnapshot{Disk: "data", ImageSize: 4096, CompressedSize: 10, Checksum: "abc123"}
	require.NoError(t, s.saveStaging("t1", want))

	got, err := s.loadStaging("t1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, *want, *got)
}

// Bytes left behind by a server that died mid-compression have no metadata, and
// must not be mistaken for a snapshot that is ready to send.
func TestStagingWithoutMetadataIsNotUsable(t *testing.T) {
	s, _, _, _ := newTestServer(t, []byte("hello"))

	path, err := s.transferPath("t1")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte("half a snapshot"), 0600))

	got, err := s.loadStaging("t1")
	require.NoError(t, err)
	assert.Nil(t, got)
}

// A file that does not match what the metadata says it should be cannot be
// resumed from: the offsets the client is working with would be meaningless.
func TestStagingThatDoesNotMatchItsMetadataIsDiscarded(t *testing.T) {
	s, _, _, _ := newTestServer(t, []byte("hello"))

	path, err := s.transferPath("t1")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte("short"), 0600))
	require.NoError(t, s.saveStaging("t1", &stagedSnapshot{CompressedSize: 999}))

	got, err := s.loadStaging("t1")
	require.NoError(t, err)
	assert.Nil(t, got)

	// And it is gone, rather than left to be reconsidered next time.
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
}

func TestSweepRemovesAbandonedTransfersAndKeepsFreshOnes(t *testing.T) {
	s, _, _, _ := newTestServer(t, []byte("hello"))

	dir := filepath.Join(s.diskDataPath(), transfersDir)
	require.NoError(t, os.MkdirAll(dir, 0700))

	stale := filepath.Join(dir, "stale.part")
	fresh := filepath.Join(dir, "fresh.part")
	require.NoError(t, os.WriteFile(stale, []byte("old"), 0600))
	require.NoError(t, os.WriteFile(fresh, []byte("new"), 0600))

	old := time.Now().Add(-transferTTL - time.Hour)
	require.NoError(t, os.Chtimes(stale, old, old))

	s.sweepTransfers()

	_, err := os.Stat(stale)
	assert.True(t, os.IsNotExist(err), "a transfer nobody came back for should be reclaimed")
	assert.FileExists(t, fresh, "a transfer still within its window must survive")
}

// The lock is what stops two calls carrying the same id from interleaving their
// appends or truncating each other's staging.
func TestTransferLockSerializesTheSameId(t *testing.T) {
	locks := newKeyedLocks()

	release := locks.acquire("t1")

	entered := make(chan struct{})
	go func() {
		r := locks.acquire("t1")
		close(entered)
		r()
	}()

	select {
	case <-entered:
		t.Fatal("a second holder of the same id must wait")
	case <-time.After(50 * time.Millisecond):
	}

	release()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("releasing the id should have let the waiter through")
	}
}

// Different transfers must not queue behind each other; two operators backing
// up different disks are not related.
func TestTransferLockDoesNotSerializeDifferentIds(t *testing.T) {
	locks := newKeyedLocks()

	release := locks.acquire("t1")
	defer release()

	done := make(chan struct{})
	go func() {
		r := locks.acquire("t2")
		r()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a different transfer id should not have blocked")
	}
}

// Reference counting keeps the map from growing by one entry per backup, and
// must not drop an entry another caller is still holding.
func TestTransferLockReleasesItsBookkeeping(t *testing.T) {
	locks := newKeyedLocks()

	for range 50 {
		locks.acquire("t1")()
	}

	locks.mu.Lock()
	n := len(locks.locks)
	locks.mu.Unlock()
	assert.Zero(t, n, "a fully released id should leave no entry behind")
}

// The sweep has to take both halves of a staged backup. Taking only the bytes
// would leave the metadata behind forever, since nothing looks at a metadata
// file whose .part is gone.
func TestSweepRemovesStagingMetadataToo(t *testing.T) {
	s, _, _, _ := newTestServer(t, []byte("hello"))

	dir := filepath.Join(s.diskDataPath(), transfersDir)
	require.NoError(t, os.MkdirAll(dir, 0700))

	part := filepath.Join(dir, "stale.part")
	meta := filepath.Join(dir, "stale.meta")
	require.NoError(t, os.WriteFile(part, []byte("bytes"), 0600))
	require.NoError(t, os.WriteFile(meta, []byte("{}"), 0600))

	old := time.Now().Add(-transferTTL - time.Hour)
	for _, p := range []string{part, meta} {
		require.NoError(t, os.Chtimes(p, old, old))
	}

	s.sweepTransfers()

	for _, p := range []string{part, meta} {
		_, err := os.Stat(p)
		assert.True(t, os.IsNotExist(err), "%s should have been reclaimed", p)
	}
}
