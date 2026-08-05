package rpc

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// readN reads exactly n bytes from r (the mux stream chunks writes into frames).
func readN(t *testing.T, r io.Reader, n int) []byte {
	t.Helper()
	buf := make([]byte, n)
	_, err := io.ReadFull(r, buf)
	require.NoError(t, err)
	return buf
}

func TestMsgMux(t *testing.T) {
	t.Run("client opens, server accepts, bidirectional bytes", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		ca, cb := newMemPipe()
		client := newMsgSession(ca, true, 0)
		server := newMsgSession(cb, false, 0)

		st, err := client.OpenStreamSync(ctx)
		r.NoError(err)

		_, err = st.Write([]byte("hello"))
		r.NoError(err)

		srvStream, err := server.AcceptStream(ctx)
		r.NoError(err)
		r.Equal([]byte("hello"), readN(t, srvStream, 5))

		// reply server -> client
		_, err = srvStream.Write([]byte("world"))
		r.NoError(err)
		r.Equal([]byte("world"), readN(t, st, 5))

		r.NoError(st.Close())
		r.NoError(server.Close())
		r.NoError(client.Close())
	})

	t.Run("server can also open streams to client", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		ca, cb := newMemPipe()
		client := newMsgSession(ca, true, 0)
		server := newMsgSession(cb, false, 0)

		// server-initiated stream
		ss, err := server.OpenStreamSync(ctx)
		r.NoError(err)
		_, err = ss.Write([]byte("ping"))
		r.NoError(err)

		cs, err := client.AcceptStream(ctx)
		r.NoError(err)
		r.Equal([]byte("ping"), readN(t, cs, 4))

		client.Close()
		server.Close()
	})

	t.Run("FIN delivers EOF after buffered data", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		ca, cb := newMemPipe()
		client := newMsgSession(ca, true, 0)
		server := newMsgSession(cb, false, 0)

		st, err := client.OpenStreamSync(ctx)
		r.NoError(err)
		_, err = st.Write([]byte("data"))
		r.NoError(err)
		r.NoError(st.Close()) // FIN

		ss, err := server.AcceptStream(ctx)
		r.NoError(err)
		r.Equal([]byte("data"), readN(t, ss, 4))

		// next read sees EOF
		_, err = ss.Read(make([]byte, 1))
		r.ErrorIs(err, io.EOF)

		client.Close()
		server.Close()
	})

	t.Run("concurrent streams do not interleave", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		ca, cb := newMemPipe()
		client := newMsgSession(ca, true, 0)
		server := newMsgSession(cb, false, 0)

		// Server echoes each accepted stream.
		go func() {
			for {
				ss, err := server.AcceptStream(ctx)
				if err != nil {
					return
				}
				go func(s rpcStream) {
					buf := make([]byte, 8)
					n, _ := io.ReadFull(s, buf)
					s.Write(buf[:n])
					s.Close()
				}(ss)
			}
		}()

		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				st, err := client.OpenStreamSync(ctx)
				r.NoError(err)
				payload := []byte{byte(i), byte(i), byte(i), byte(i), byte(i), byte(i), byte(i), byte(i)}
				_, err = st.Write(payload)
				r.NoError(err)
				r.Equal(payload, readN(t, st, 8))
				st.Close()
			}(i)
		}
		wg.Wait()

		client.Close()
		server.Close()
	})

	t.Run("CancelRead unblocks a blocked reader", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		ca, cb := newMemPipe()
		client := newMsgSession(ca, true, 0)
		server := newMsgSession(cb, false, 0)

		st, err := client.OpenStreamSync(ctx)
		r.NoError(err)
		_, err = st.Write([]byte("x")) // ensure server sees the stream
		r.NoError(err)
		_, err = server.AcceptStream(ctx)
		r.NoError(err)

		done := make(chan error, 1)
		go func() {
			_, err := st.Read(make([]byte, 8))
			done <- err
		}()

		// drain the one byte we wrote isn't on this side; the read above blocks.
		st.CancelRead(cancelReadCode)
		r.ErrorIs(<-done, errStreamCancel)

		client.Close()
		server.Close()
	})

	t.Run("large payload spans multiple frames", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		ca, cb := newMemPipe()
		client := newMsgSession(ca, true, 0)
		server := newMsgSession(cb, false, 0)

		big := make([]byte, defaultMaxFrameData*2+1234)
		for i := range big {
			big[i] = byte(i)
		}

		go func() {
			st, _ := client.OpenStreamSync(ctx)
			st.Write(big)
			st.Close()
		}()

		ss, err := server.AcceptStream(ctx)
		r.NoError(err)
		got, err := io.ReadAll(ss)
		r.NoError(err)
		r.Equal(big, got)

		client.Close()
		server.Close()
	})

	_ = context.Background
}
