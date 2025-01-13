package stream

import (
	"context"
	"testing"

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

		var vals []int

		serv.ExposeValue("stream", ReadStream(func(val int) error {
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

		r.Equal([]int{42, 100, 111}, vals)
	})

	t.Run("can send a stream of structs", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ss, err := rpc.NewState(ctx, rpc.WithSkipVerify)
		r.NoError(err)

		serv := ss.Server()

		var vals []*Thing

		serv.ExposeValue("stream", ReadStream(func(val *Thing) error {
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

		r.Equal([]*Thing{
			{Name: "foo"},
		}, vals)

	})
}
