package stream

import (
	"context"
	"errors"
	"io"

	rpc "miren.dev/runtime/pkg/rpc"
)

//go:generate go run ../cmd/rpcgen/main.go -pkg stream -input stream.yml -output stream.gen.go

type Sender[T any] struct {
	c   *rpc.NetworkClient
	ssc SendStreamClient[T]
}

func ClientSend[T any](c *rpc.NetworkClient) (*Sender[T], error) {
	ssc := SendStreamClient[T]{Client: c}
	return &Sender[T]{c: c, ssc: ssc}, nil
}

func (s *Sender[T]) Send(ctx context.Context, value T) error {
	_, err := s.ssc.Send(ctx, value)
	if err != nil {
		return err
	}

	return nil
}

type Receiver[T any] struct {
	fn func(T) error
}

func (r *Receiver[T]) Send(ctx context.Context, state *SendStreamSend[T]) error {
	return r.fn(state.Args().Value())
}

func ReadStream[T any](fn func(T) error) *rpc.Interface {
	return AdaptSendStream[T](&Receiver[T]{fn: fn})
}

func StreamRecv[T any](fn func(T) error) SendStream[T] {
	return &Receiver[T]{fn: fn}
}

const streamChunkSize = 10 * 1024 * 1024 // 10MB chunks for efficient streaming over high-latency links

// bulkBatchSize is the minimum number of bytes a bulk-mode ServeReader waits
// to accumulate before responding to a Recv. The Recv↔Read loop is one
// round-trip per response, so without batching, each gzip flush (often a
// few hundred bytes) becomes its own RTT, capping a transcontinental deploy
// at ~2.5 KB/s. 1 MB lifts the ceiling to roughly batch/RTT (~10 MB/s at
// 100 ms RTT). Opt in via WithBulkBatching; interactive callers like exec
// stdin must not use this, or they would stall until the buffer fills.
const bulkBatchSize = 1024 * 1024

type rscReader struct {
	ctx    context.Context
	rsc    *RecvStreamClient[[]byte]
	buffer []byte
	offset int
}

func (r *rscReader) Read(p []byte) (n int, err error) {
	// If we have buffered data, return it first
	if r.offset < len(r.buffer) {
		n = copy(p, r.buffer[r.offset:])
		r.offset += n
		return n, nil
	}

	// Request a large chunk to minimize RPC round-trips
	requestSize := max(len(p), streamChunkSize)

	ret, err := r.rsc.Recv(r.ctx, int32(requestSize))
	if err != nil {
		return 0, err
	}

	data := ret.Value()

	if len(data) == 0 {
		return 0, io.EOF
	}

	// If the returned data fits in p, return it directly
	if len(data) <= len(p) {
		return copy(p, data), nil
	}

	// Otherwise, buffer the extra data for next read
	n = copy(p, data)
	r.buffer = data
	r.offset = n
	return n, nil
}

func (r *rscReader) Close() error {
	return r.rsc.Close()
}

func ToReader(ctx context.Context, x *RecvStreamClient[[]byte]) io.ReadCloser {
	return &rscReader{ctx: ctx, rsc: x}
}

type serveReader struct {
	r        io.Reader
	minBatch int // 0 means stream as bytes arrive; >0 means batch until ≥minBatch.
}

// ServeReaderOption configures a ServeReader. The zero-config default is
// streaming: each underlying Read becomes one Recv response so callers like
// interactive stdin pipes don't wait for a buffer to fill.
type ServeReaderOption func(*serveReader)

// WithBulkBatching tells the ServeReader to wait until at least bulkBatchSize
// bytes are available before responding to a Recv (or until EOF). Use for
// bulk transfers where the round-trip per chunk is the bottleneck, like
// shipping a deploy tarball. Must NOT be used for interactive streams —
// it would stall every key the user types until the buffer fills.
func WithBulkBatching() ServeReaderOption {
	return func(s *serveReader) { s.minBatch = bulkBatchSize }
}

func (s *serveReader) Recv(ctx context.Context, state *RecvStreamRecv[[]byte]) error {
	args := state.Args()

	// Limit the maximum read size to prevent excessive memory allocation
	readSize := min(int(args.Count()), streamChunkSize*2)

	buf := make([]byte, readSize)

	var (
		n   int
		err error
	)
	if s.minBatch > 0 {
		minBatch := min(s.minBatch, readSize)
		n, err = io.ReadAtLeast(s.r, buf, minBatch)
	} else {
		// Streaming mode: ship whatever the underlying reader yields right
		// now, so per-byte interactive sources (tty stdin, etc.) don't stall
		// waiting to fill a buffer.
		n, err = s.r.Read(buf)
	}

	if err != nil {
		// EOF (no bytes) or short-read EOF (partial final batch in bulk
		// mode) are both the natural end of stream. Anything else is real.
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			if n == 0 {
				if c, ok := s.r.(io.Closer); ok {
					c.Close()
				}
				state.Results().SetValue(nil)
				return nil
			}
			// Ship the partial batch without closing s.r — the next Recv
			// will hit a clean (0, EOF) and close there. Closing now would
			// turn the next Read into ErrClosedPipe and surface as an RPC
			// error instead of an orderly end-of-stream.
		} else {
			return err
		}
	}

	state.Results().SetValue(buf[:n])

	return nil
}

func ServeReader(ctx context.Context, r io.Reader, opts ...ServeReaderOption) RecvStream[[]byte] {
	s := &serveReader{r: r}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type serveWriter struct {
	w io.Writer
}

func (s *serveWriter) Send(ctx context.Context, state *SendStreamSend[[]byte]) error {
	args := state.Args()

	n, err := s.w.Write(args.Value())
	if err != nil {
		return err
	}

	state.Results().SetCount(int32(n))

	return nil
}

func ServeWriter(ctx context.Context, w io.Writer) SendStream[[]byte] {
	return &serveWriter{w: w}
}

type wscWriter struct {
	ctx context.Context
	wsc *SendStreamClient[[]byte]
}

func (w *wscWriter) Write(p []byte) (n int, err error) {
	// For large writes, send in chunks to avoid overwhelming buffers
	remaining := p
	totalWritten := 0

	for len(remaining) > 0 {
		chunk := remaining
		if len(chunk) > streamChunkSize {
			chunk = remaining[:streamChunkSize]
		}

		result, err := w.wsc.Send(w.ctx, chunk)
		if err != nil {
			return totalWritten, err
		}

		written := int(result.Count())
		totalWritten += written
		remaining = remaining[written:]

		// If we didn't write the full chunk, stop here
		if written < len(chunk) {
			break
		}
	}

	return totalWritten, nil
}

func (w *wscWriter) Close() error {
	return w.wsc.Close()
}

func ToWriter(ctx context.Context, x *SendStreamClient[[]byte]) io.WriteCloser {
	return &wscWriter{ctx: ctx, wsc: x}
}

type chanReader[T any] struct {
	ch <-chan T
}

func (c *chanReader[T]) Recv(ctx context.Context, state *RecvStreamRecv[T]) error {
	// Consume args from decoder to prevent leftover data from being
	// interpreted as the next stream request
	_ = state.Args()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case v, ok := <-c.ch:
		if !ok {
			return io.EOF
		}

		state.Results().SetValue(v)
		return nil
	}
}

func ChanReader[T any](ch <-chan T) RecvStream[T] {
	return &chanReader[T]{ch: ch}
}

func ChanWriter[T any](ctx context.Context, rs *RecvStreamClient[T], ch chan<- T) {
	go func() {
		defer close(ch)
		defer rs.Close()

		for {
			ret, err := rs.Recv(ctx, 1)
			if err != nil {
				return
			}

			ch <- ret.Value()
		}
	}()
}

type callbackSender[T any] struct {
	fn func(T) error
}

func (c *callbackSender[T]) Send(ctx context.Context, state *SendStreamSend[T]) error {
	return c.fn(state.Args().Value())
}

func Callback[T any](f func(T) error) SendStream[T] {
	return &callbackSender[T]{fn: f}
}
