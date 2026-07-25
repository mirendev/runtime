package stream

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	rpc "miren.dev/runtime/pkg/rpc"
)

type Thing struct {
	Name string `json:"name"`
}

func TestStream(t *testing.T) {
	t.Run("can send a stream of values", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ss, err := rpc.NewState(ctx, rpc.WithSkipVerify)
		r.NoError(err)

		serv := ss.Server()

		// The handler runs on the server's goroutine while the assertion below
		// reads from the test's, so the accumulator needs a lock.
		var (
			mu   sync.Mutex
			vals []int
		)

		serv.ExposeValue("stream", ReadStream(func(val int) error {
			mu.Lock()
			defer mu.Unlock()

			vals = append(vals, val)
			return nil
		}))

		cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
		r.NoError(err)

		c, err := cs.Connect(ss.ListenAddr(), "stream")
		r.NoError(err)

		css, err := ClientSend[int](c)
		r.NoError(err)

		r.NoError(css.Send(ctx, 42))
		r.NoError(css.Send(ctx, 100))
		r.NoError(css.Send(ctx, 111))

		mu.Lock()
		defer mu.Unlock()

		r.Equal([]int{42, 100, 111}, vals)
	})

	t.Run("can send a stream of structs", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ss, err := rpc.NewState(ctx, rpc.WithSkipVerify)
		r.NoError(err)

		serv := ss.Server()

		// The handler runs on the server's goroutine while the assertion below
		// reads from the test's, so the accumulator needs a lock.
		var (
			mu   sync.Mutex
			vals []*Thing
		)

		serv.ExposeValue("stream", ReadStream(func(val *Thing) error {
			mu.Lock()
			defer mu.Unlock()

			vals = append(vals, val)
			return nil
		}))

		cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
		r.NoError(err)

		c, err := cs.Connect(ss.ListenAddr(), "stream")
		r.NoError(err)

		css, err := ClientSend[*Thing](c)
		r.NoError(err)

		r.NoError(css.Send(ctx, &Thing{Name: "foo"}))

		mu.Lock()
		defer mu.Unlock()

		r.Equal([]*Thing{
			{Name: "foo"},
		}, vals)

	})

	t.Run("ChanWriter closes output channel when stream ends", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Server side: expose a RecvStream backed by a channel
		sourceCh := make(chan int)

		ss, err := rpc.NewState(ctx, rpc.WithSkipVerify)
		r.NoError(err)

		serv := ss.Server()
		serv.ExposeValue("stream", AdaptRecvStream(ChanReader(sourceCh)))

		// Client side: connect and wrap as RecvStreamClient
		cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
		r.NoError(err)

		c, err := cs.Connect(ss.ListenAddr(), "stream")
		r.NoError(err)

		rsc := NewRecvStreamClient[int](c)

		// Wire ChanWriter: reads from rsc, writes to outputCh
		outputCh := make(chan int, 10)
		ChanWriter(ctx, rsc, outputCh)

		// Send a value through and verify it arrives
		sourceCh <- 42
		select {
		case v := <-outputCh:
			r.Equal(42, v)
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for value")
		}

		// Close the source channel, which should cause ChanWriter to
		// close the output channel (this is the bug fix for MIR-622)
		close(sourceCh)

		select {
		case _, ok := <-outputCh:
			r.False(ok, "output channel should be closed")
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for output channel to close")
		}
	})
}

// drippyReader returns one byte per Read. Used as a worst-case source for
// the streaming-vs-batching tests: in streaming mode each drip becomes its
// own Recv response, in batching mode many drips collapse into one.
type drippyReader struct {
	mu     sync.Mutex
	data   []byte
	pos    int
	closed bool
}

func (d *drippyReader) Read(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return 0, io.ErrClosedPipe
	}
	if d.pos >= len(d.data) {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = d.data[d.pos]
	d.pos++
	return 1, nil
}

func (d *drippyReader) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return nil
}

// serveAndCount wires a ServeReader-backed RecvStream into a fresh RPC pair
// and drives the stream by calling Recv directly so we can count how many
// responses it took to drain the source. Returns the assembled bytes and
// the chunk count (a chunk being one non-empty Recv response; the final
// zero-byte EOF response is not counted).
func serveAndCount(t *testing.T, src io.Reader, opts ...ServeReaderOption) ([]byte, int) {
	t.Helper()
	r := require.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ss, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(err)
	ss.Server().ExposeValue("stream", AdaptRecvStream(ServeReader(ctx, src, opts...)))

	cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(err)
	c, err := cs.Connect(ss.ListenAddr(), "stream")
	r.NoError(err)

	rsc := NewRecvStreamClient[[]byte](c)

	var out []byte
	var chunks int
	for {
		res, err := rsc.Recv(ctx, streamChunkSize)
		r.NoError(err)
		v := res.Value()
		if len(v) == 0 {
			break
		}
		chunks++
		out = append(out, v...)
		if chunks > 10_000_000 {
			t.Fatal("runaway: chunk count exceeded sanity ceiling")
		}
	}
	return out, chunks
}

func TestServeReader(t *testing.T) {
	t.Run("WithBulkBatching collapses a drip source into a handful of Recv responses", func(t *testing.T) {
		r := require.New(t)

		// 4 MB delivered one byte at a time. Bulk mode batches at 1 MB,
		// so we expect ~4 data chunks plus the final partial chunk; the
		// assertion ceiling has slack to absorb the partial.
		const total = 4 * 1024 * 1024
		src := &drippyReader{data: make([]byte, total)}
		for i := range src.data {
			src.data[i] = byte(i)
		}

		got, chunks := serveAndCount(t, src, WithBulkBatching())

		r.Equal(src.data, got, "all bytes round-trip intact")
		r.LessOrEqual(chunks, 6, "expected ≤6 chunks under 1 MB batching, got %d", chunks)
	})

	t.Run("streaming mode (default) emits one chunk per underlying Read", func(t *testing.T) {
		r := require.New(t)

		// 1024 bytes from the drip source; without batching every byte
		// should land in its own Recv response, exactly like interactive
		// stdin would. If this ever ratchets down, it means we
		// accidentally re-enabled batching for non-bulk callers.
		const total = 1024
		src := &drippyReader{data: make([]byte, total)}
		for i := range src.data {
			src.data[i] = byte(i)
		}

		got, chunks := serveAndCount(t, src)

		r.Equal(src.data, got)
		r.Equal(total, chunks, "streaming mode must emit one chunk per byte from a drip source")
	})

	t.Run("tiny bulk-batched stream below the floor still terminates cleanly", func(t *testing.T) {
		r := require.New(t)

		src := &drippyReader{data: []byte("hello world")}
		got, chunks := serveAndCount(t, src, WithBulkBatching())

		r.Equal([]byte("hello world"), got)
		r.Equal(1, chunks, "partial-final batch should ship in a single chunk")
	})

	t.Run("empty stream terminates without error", func(t *testing.T) {
		r := require.New(t)

		src := &drippyReader{data: nil}
		got, chunks := serveAndCount(t, src, WithBulkBatching())

		r.Empty(got)
		r.Equal(0, chunks)
	})
}
