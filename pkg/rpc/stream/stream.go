package stream

import (
	"context"
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
		return err
	}

	buf = buf[:n]

	state.Results().SetValue(buf)

	return nil
}

func ServeReader(ctx context.Context, r io.Reader) RecvStream[[]byte] {
	return &serveReader{r: r}
}
