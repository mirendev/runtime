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

const streamChunkSize = 1024 * 1024 // 1MB chunks for efficient streaming

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
	r io.Reader
}

func (s *serveReader) Recv(ctx context.Context, state *RecvStreamRecv[[]byte]) error {
	args := state.Args()

	// Limit the maximum read size to prevent excessive memory allocation
	readSize := int(args.Count())
	if readSize > streamChunkSize*2 {
		readSize = streamChunkSize * 2
	}

	buf := make([]byte, readSize)

	n, err := s.r.Read(buf)
	if err != nil {
		if errors.Is(err, io.EOF) {
			if c, ok := s.r.(io.Closer); ok {
				c.Close()
			}
			state.Results().SetValue(nil)
			return nil
		}
		return err
	}

	buf = buf[:n]

	state.Results().SetValue(buf)

	return nil
}

func ServeReader(ctx context.Context, r io.Reader) RecvStream[[]byte] {
	return &serveReader{r: r}
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
