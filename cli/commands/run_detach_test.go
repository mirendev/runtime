package commands

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readAll drains a detachReader the way the RPC stream does, one modest buffer
// at a time, so the tests exercise the same chunk boundaries.
func readAll(t *testing.T, d *detachReader, bufSize int) string {
	t.Helper()

	var got strings.Builder
	buf := make([]byte, bufSize)
	for {
		n, err := d.Read(buf)
		got.Write(buf[:n])
		if err == io.EOF {
			return got.String()
		}
		require.NoError(t, err)
	}
}

func TestDetachReaderPassesOrdinaryInputThrough(t *testing.T) {
	d := newDetachReader(strings.NewReader("hello world\n"), func() {})

	assert.Equal(t, "hello world\n", readAll(t, d, 64))
	assert.False(t, d.Detached())
}

func TestDetachReaderStopsOnTheSequence(t *testing.T) {
	var detached bool
	d := newDetachReader(
		strings.NewReader("before\x10\x11after"),
		func() { detached = true },
	)

	// Everything before the sequence reaches the container; nothing after it
	// does, since the client is leaving.
	assert.Equal(t, "before", readAll(t, d, 64))
	assert.True(t, d.Detached())
	assert.True(t, detached, "the detach callback must fire so the RPC is canceled")
}

// A lone Ctrl-P is a real keystroke -- previous-line in readline -- and has to
// survive, or attaching would silently break line editing.
func TestDetachReaderPassesLoneCtrlPThrough(t *testing.T) {
	d := newDetachReader(strings.NewReader("a\x10b"), func() {})

	assert.Equal(t, "a\x10b", readAll(t, d, 64))
	assert.False(t, d.Detached())
}

// Ctrl-P followed by anything else is not the sequence, including the case
// where the two bytes land in different reads. Splitting them is the normal
// case for a person typing, since each keystroke arrives on its own.
func TestDetachReaderSpansReadBoundaries(t *testing.T) {
	t.Run("sequence split across reads", func(t *testing.T) {
		var detached bool
		d := newDetachReader(
			iotest(t, "x\x10", "\x11y"),
			func() { detached = true },
		)

		assert.Equal(t, "x", readAll(t, d, 64))
		assert.True(t, detached)
	})

	t.Run("false start split across reads", func(t *testing.T) {
		d := newDetachReader(iotest(t, "x\x10", "zy"), func() {
			t.Fatal("must not detach on Ctrl-P followed by an ordinary byte")
		})

		assert.Equal(t, "x\x10zy", readAll(t, d, 64))
	})
}

// A one-byte buffer forces the reader to hold data across calls, which is where
// an in-place filter would corrupt or drop bytes.
func TestDetachReaderHandlesTinyBuffers(t *testing.T) {
	d := newDetachReader(strings.NewReader("a\x10bcd"), func() {})

	assert.Equal(t, "a\x10bcd", readAll(t, d, 1))
}

// chunkReader hands out one predetermined chunk per Read, modelling a terminal
// delivering keystrokes as they are typed.
type chunkReader struct{ chunks []string }

func (c *chunkReader) Read(p []byte) (int, error) {
	if len(c.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, c.chunks[0])
	if n < len(c.chunks[0]) {
		c.chunks[0] = c.chunks[0][n:]
		return n, nil
	}
	c.chunks = c.chunks[1:]
	return n, nil
}

func iotest(t *testing.T, chunks ...string) io.Reader {
	t.Helper()
	return &chunkReader{chunks: chunks}
}

// The reader feeds an RPC stream where a zero-length payload means EOF, so
// (0, nil) is not merely discouraged here -- it severs the container's stdin
// for the rest of the session. A held-back Ctrl-P is the state that produces
// it, and Ctrl-P alone is previous-line in readline: common enough that this
// broke interactive use outright, and left the detach sequence unable to
// rescue it because the server had stopped reading.
func TestDetachReaderNeverReportsAnEmptyRead(t *testing.T) {
	// One byte per Read, so the held Ctrl-P is alone in its read exactly as it
	// is when a person types it.
	d := newDetachReader(iotest(t, "\x10", "l", "s", "\n"), func() {
		t.Fatal("Ctrl-P followed by ordinary bytes is not the detach sequence")
	})

	buf := make([]byte, 64)
	for {
		n, err := d.Read(buf)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		require.NotZero(t, n, "a zero-length read is EOF to the stream and kills stdin")
	}
}

// The same invariant at the moment of detaching: the sequence itself produces
// no bytes, and must arrive as EOF rather than an empty read.
func TestDetachReaderReportsEOFNotAnEmptyReadOnDetach(t *testing.T) {
	var detached bool
	d := newDetachReader(iotest(t, "\x10", "\x11"), func() { detached = true })

	buf := make([]byte, 64)
	n, err := d.Read(buf)
	assert.Zero(t, n)
	assert.Equal(t, io.EOF, err)
	assert.True(t, detached)
}
