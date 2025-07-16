package stream

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

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

type rscReader struct {
	ctx       context.Context
	rsc       *RecvStreamClient[[]byte]
	totalRead int64
	readCount int
	log       *slog.Logger
}

func (r *rscReader) Read(p []byte) (n int, err error) {
	r.readCount++
	readStart := time.Now()
	requestSize := len(p)


	ret, err := r.rsc.Recv(r.ctx, int32(requestSize))
	if err != nil {
		if r.log != nil {
			r.log.Error("rpc stream recv error",
				"error", err,
				"readCount", r.readCount,
				"duration", time.Since(readStart))
		}
		return 0, err
	}

	data := ret.Value()
	dataLen := len(data)

	if dataLen == 0 {
		if r.log != nil {
			r.log.Info("rpc stream EOF",
				"totalBytesRead", r.totalRead,
				"totalReads", r.readCount,
				"duration", time.Since(readStart))
		}
		return 0, io.EOF
	}

	n = copy(p, data)
	r.totalRead += int64(n)


	return n, nil
}

func (r *rscReader) Close() error {
	if r.log != nil {
		r.log.Info("closing rpc stream reader",
			"totalBytesRead", r.totalRead,
			"totalReads", r.readCount)
	}
	return r.rsc.Close()
}

func ToReader(ctx context.Context, x *RecvStreamClient[[]byte], log *slog.Logger) io.ReadCloser {
	if log == nil {
		log = slog.Default()
	}
	log = log.With("component", "rpc.stream.reader")
	return &rscReader{
		ctx: ctx,
		rsc: x,
		log: log,
	}
}

type serveReader struct {
	r   io.Reader
	log *slog.Logger
}

func (s *serveReader) Recv(ctx context.Context, state *RecvStreamRecv[[]byte]) error {
	// This is called when a client wants to read data from the server's reader.
	// The client specifies how much data it can accept (bufSize), but the server
	// may return less if that's all that's available or for protocol efficiency.
	args := state.Args()
	recvStart := time.Now()

	bufSize := args.Count()
	log := s.log


	buf := make([]byte, bufSize)

	n, err := s.r.Read(buf)
	if err != nil {
		if errors.Is(err, io.EOF) {
			log.Info("server stream EOF",
				"duration", time.Since(recvStart))
			if c, ok := s.r.(io.Closer); ok {
				c.Close()
			}
			state.Results().SetValue(nil)
			return nil
		}
		log.Error("server read error",
			"error", err,
			"duration", time.Since(recvStart))
		return err
	}

	buf = buf[:n]

	if n > 1024*1024 {
		log.Info("server sent large chunk",
			"size", n,
			"duration", time.Since(recvStart))
	} else if n < int(bufSize) && n > 1024 {
		// Only warn if we're sending actual data (>1KB) but less than requested
		// Small responses (<1KB) are likely just protocol messages/acknowledgments
		log.Debug("server sent partial data",
			"requested", bufSize,
			"sent", n,
			"duration", time.Since(recvStart))
	}

	state.Results().SetValue(buf)

	return nil
}

func ServeReader(ctx context.Context, r io.Reader, log *slog.Logger) RecvStream[[]byte] {
	if log == nil {
		log = slog.Default()
	}
	log = log.With("component", "rpc.stream.server")
	return &serveReader{r: r, log: log}
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
	result, err := w.wsc.Send(w.ctx, p)
	if err != nil {
		return 0, err
	}

	return int(result.Count()), nil
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
