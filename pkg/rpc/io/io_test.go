package io

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	rpc "miren.dev/runtime/pkg/rpc"
)

func TestReader(t *testing.T) {
	r := require.New(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pr, pw := io.Pipe()

	s := ServeReader(pr)

	ss, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(err)

	serv := ss.Server()

	serv.ExposeValue("meter", AdaptReader(s))

	cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(err)

	c, err := cs.Connect(ss.ListenAddr(), "meter")
	r.NoError(err)

	mc := ReaderClient{Client: c}

	cr := AsReader(ctx, mc)

	go func() {
		pw.Write([]byte("hello rpc"))
	}()

	buf := make([]byte, 100)

	n, err := cr.Read(buf)
	r.NoError(err)

	r.Equal("hello rpc", string(buf[:n]))
}

func TestWriter(t *testing.T) {
	r := require.New(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pr, pw := io.Pipe()

	s := ServeWriter(pw)

	ss, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(err)

	serv := ss.Server()

	serv.ExposeValue("meter", AdaptWriter(s))

	cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(err)

	c, err := cs.Connect(ss.ListenAddr(), "meter")
	r.NoError(err)

	mc := WriterClient{Client: c}

	cr := AsWriter(ctx, mc)

	go func() {
		cr.Write([]byte("hello rpc"))
	}()

	buf := make([]byte, 100)

	n, err := pr.Read(buf)
	r.NoError(err)

	r.Equal("hello rpc", string(buf[:n]))
}
