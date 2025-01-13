package stream

import (
	"context"

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
