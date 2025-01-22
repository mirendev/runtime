package stream

import (
	"context"
	"errors"
	"io"

	rpc "miren.dev/runtime/pkg/rpc"
)

//go:generate go run ../cmd/rpcgen/main.go -pkg stream -input stream.yml -output stream.gen.go

type Sender[T any] struct {
	c   *rpc.Client
	ssc SendStreamClient[T]
}

func ClientSend[T any](c *rpc.Client) (*Sender[T], error) {
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
	ctx context.Context
	rsc *RecvStreamClient[[]byte]
}

func (r *rscReader) Read(p []byte) (n int, err error) {
	ret, err := r.rsc.Recv(r.ctx, int32(len(p)))
	if err != nil {
		return 0, err
	}

	data := ret.Value()

	if len(data) == 0 {
		return 0, io.EOF
	}

	return copy(p, data), nil
}

func ToReader(ctx context.Context, x *RecvStreamClient[[]byte]) io.Reader {
	return &rscReader{ctx: ctx, rsc: x}
}

type serveReader struct {
	r io.Reader
}

func (s *serveReader) Recv(ctx context.Context, state *RecvStreamRecv[[]byte]) error {
	args := state.Args()

	buf := make([]byte, args.Count())

	n, err := s.r.Read(buf)
	if err != nil {
		if errors.Is(err, io.EOF) {
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
	result, err := w.wsc.Send(w.ctx, p)
	if err != nil {
		return 0, err
	}

	return int(result.Count()), nil
}

func ToWriter(ctx context.Context, x *SendStreamClient[[]byte]) io.Writer {
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
