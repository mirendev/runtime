package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/cond"
)

// Retrying a refusal six times only makes the operator wait longer to read a
// message that will say the same thing every time.
func TestRefusalsAreNotRetried(t *testing.T) {
	refusals := []error{
		cond.ValidationFailure("disk-backup", `disk "data" is in use`),
		cond.NotFound("disk", "data"),
		fmt.Errorf("wrapped: %w", cond.ValidationFailure("disk-backup", "no cloud")),
	}
	for _, err := range refusals {
		assert.False(t, resumable(err), "should not retry: %v", err)
	}
}

// A dropped connection is exactly what resume exists for.
func TestTransportFailuresAreRetried(t *testing.T) {
	assert.True(t, resumable(errors.New("connection reset by peer")))
	assert.True(t, resumable(errors.New("the cluster's link to the cloud dropped")))
}

func TestNilIsNotRetried(t *testing.T) {
	assert.False(t, resumable(nil))
}

// Two transfers must not collide in the server's staging directory.
func TestTransferIDsAreDistinct(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := newTransferID()
		require.NoError(t, err)
		require.NotEmpty(t, id)
		assert.False(t, seen[id], "transfer ids must not repeat")
		seen[id] = true
	}
}

// The id names a file on the server, which validates it — so the ids we
// generate have to be ones it accepts.
func TestTransferIDsUseOnlySafeCharacters(t *testing.T) {
	id, err := newTransferID()
	require.NoError(t, err)

	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			t.Fatalf("transfer id %q contains %q, which the server rejects", id, r)
		}
	}
}

// closeCountingFile stands in for the snapshot file a restore uploads from,
// recording whether anything closed it out from under us.
type closeCountingFile struct {
	*os.File
	closes int
}

func (f *closeCountingFile) Close() error {
	f.closes++
	return f.File.Close()
}

// A retry has to be able to seek the snapshot again, and the case that catches
// this is the one where the upload fully succeeded and the server then rejected
// it: the stream reaches EOF, closes what it was reading from, and every later
// attempt fails on the seek rather than on anything real.
//
// readerOnly is what prevents that, by handing the stream something with no
// Close to find.
func TestReaderOnlyHidesCloseFromTheStream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snap.miren.zst")
	require.NoError(t, os.WriteFile(path, []byte("a snapshot's worth of bytes"), 0600))

	f, err := os.Open(path)
	require.NoError(t, err)
	tracked := &closeCountingFile{File: f}
	defer tracked.File.Close()

	// What the stream helper does when it reaches the end: close the reader if
	// it can. Wrapped, there is nothing to close.
	var wrapped io.Reader = readerOnly{tracked}
	_, isCloser := wrapped.(io.Closer)
	assert.False(t, isCloser, "the stream must not be able to close the caller's file")

	// Read it to the end, the way a completed upload does.
	_, err = io.ReadAll(wrapped)
	require.NoError(t, err)
	assert.Zero(t, tracked.closes, "reading to EOF must not have closed the file")

	// And a retry can still rewind and re-send it.
	_, err = tracked.Seek(0, io.SeekStart)
	require.NoError(t, err, "a retry after a complete upload must still be able to seek")

	again, err := io.ReadAll(readerOnly{tracked})
	require.NoError(t, err)
	assert.Equal(t, "a snapshot's worth of bytes", string(again))
}

// The unwrapped file is the shape that caused the bug, so pin the difference:
// handed the file directly, the stream helper would find a Closer to call.
func TestAnUnwrappedFileWouldBeClosedByTheStream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snap.miren.zst")
	require.NoError(t, os.WriteFile(path, []byte("bytes"), 0600))

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	var plain io.Reader = f
	_, isCloser := plain.(io.Closer)
	assert.True(t, isCloser,
		"an *os.File is a Closer, which is why it has to be wrapped before it is streamed")
}
