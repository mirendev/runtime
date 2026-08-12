package commands

import (
	"io"
	"sync/atomic"
)

// The detach sequence, matching docker's: Ctrl-P Ctrl-Q.
//
// An attached run needs a way out that is not "kill the client". Raw mode
// clears ISIG, so Ctrl-C is delivered to the workload as a byte rather than
// interrupting the terminal -- which is correct, since the point of attaching
// is that your keystrokes reach the command. Without an escape sequence the
// only way to leave an 8-hour console session is to kill the process from
// another terminal, and the help text's promise that "disconnecting is not
// cancelling" is unreachable in practice.
const (
	ctrlP = 0x10
	ctrlQ = 0x11
)

// detachReader passes keystrokes through to an attached run, watching for the
// detach sequence.
//
// A lone Ctrl-P is held back until the next byte arrives, because it is only
// meaningful as a prefix once the following byte is known. That delays one
// keystroke of a genuine Ctrl-P (previous-line in readline and emacs) until the
// user types again, which is the same trade docker makes.
type detachReader struct {
	src      io.Reader
	onDetach func()

	armed  bool
	buf    []byte
	closed bool

	detached atomic.Bool
	scratch  []byte
}

func newDetachReader(src io.Reader, onDetach func()) *detachReader {
	return &detachReader{src: src, onDetach: onDetach}
}

// Detached reports whether the user asked to leave, as opposed to the run
// ending on its own. The caller needs the distinction: a detached run is still
// going and has no exit code to report yet.
func (d *detachReader) Detached() bool { return d.detached.Load() }

// Read never returns (0, nil).
//
// io.Reader permits it, but this reader feeds an RPC stream that does not: a
// zero-length payload is how the transport spells EOF (pkg/rpc/stream's reader
// returns io.EOF for an empty Recv), so returning one ends the container's stdin
// for the rest of the session. Holding a Ctrl-P back produces exactly that
// nothing-yet state, which made a single Ctrl-P -- previous-line in readline --
// silently sever input, and left Ctrl-P Ctrl-Q unable to escape either, since
// the server had stopped reading. So a held byte loops and waits for the next
// one rather than reporting an empty read.
func (d *detachReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	for {
		if len(d.buf) > 0 {
			n := copy(p, d.buf)
			d.buf = d.buf[n:]
			return n, nil
		}
		if d.closed {
			return 0, io.EOF
		}

		if len(d.scratch) < len(p) {
			d.scratch = make([]byte, len(p))
		}

		n, err := d.src.Read(d.scratch[:len(p)])

		out := make([]byte, 0, n+1)
		for _, b := range d.scratch[:n] {
			if d.armed {
				d.armed = false
				if b == ctrlQ {
					d.detached.Store(true)
					d.closed = true
					if d.onDetach != nil {
						d.onDetach()
					}
					break
				}
				// Not the sequence after all, so the held Ctrl-P was a real
				// keystroke and belongs in the stream ahead of this byte.
				out = append(out, ctrlP)
			}
			if b == ctrlP {
				d.armed = true
				continue
			}
			out = append(out, b)
		}

		if len(out) > 0 {
			copied := copy(p, out)
			d.buf = append(d.buf[:0], out[copied:]...)
			return copied, nil
		}

		if err != nil {
			d.closed = true
			return 0, err
		}

		// Nothing to deliver yet: everything read so far is a held Ctrl-P
		// awaiting the byte that decides what it means. Wait for that byte.
	}
}
