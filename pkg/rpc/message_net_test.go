package rpc_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/example"
	"miren.dev/runtime/pkg/rpc/stream"
)

// netTransport describes one of the networked message transports, so both get
// identical coverage from the table below.
type netTransport struct {
	name   string
	listen func() rpc.StateOption
	remote func(*rpc.State) string
}

var netTransports = []netTransport{
	{
		name:   "websocket",
		listen: func() rpc.StateOption { return rpc.WithWSBindAddr("localhost:0") },
		remote: func(s *rpc.State) string { return "wss://" + s.WSListenAddr() },
	},
	{
		name:   "tcp",
		listen: func() rpc.StateOption { return rpc.WithTCPBindAddr("localhost:0") },
		remote: func(s *rpc.State) string { return "tcp://" + s.TCPListenAddr() },
	},
}

// netServer starts a State listening on the given transport and exposes iface
// under name, returning the remote to connect to.
func netServer(t *testing.T, ctx context.Context, tr netTransport, name string, iface *rpc.Interface) string {
	t.Helper()
	r := require.New(t)

	ss, err := rpc.NewState(ctx, rpc.WithSkipVerify, tr.listen())
	r.NoError(err)

	ss.Server().ExposeValue(name, iface)

	return tr.remote(ss)
}

func netClient(t *testing.T, ctx context.Context, remote, name string) rpc.Client {
	t.Helper()
	r := require.New(t)

	cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(err)

	c, err := cs.Connect(remote, name)
	r.NoError(err)

	return c
}

// blockingMU is a MeterUpdates implementation whose handler never replies until
// its context is cancelled (bounded so it can't leak), used to exercise
// caller-side cancellation of a callstream.
type blockingMU struct {
	started chan struct{}
}

func (m *blockingMU) RegisterUpdates(ctx context.Context, call *example.MeterUpdatesRegisterUpdates) error {
	close(m.started)
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
	}
	return cond.Closed("server done")
}

func TestNetMessageTransports(t *testing.T) {
	for _, tr := range netTransports {
		t.Run(tr.name, func(t *testing.T) {
			t.Run("unary calls", func(t *testing.T) {
				r := require.New(t)
				ctx := t.Context()

				remote := netServer(t, ctx, tr, "meter", example.AdaptMeter(&exampleMeter{temp: 42}))
				c := netClient(t, ctx, remote, "meter")

				mc := &example.MeterClient{Client: c}

				res, err := mc.ReadTemperature(ctx, "test")
				r.NoError(err)
				r.Equal("test", res.Reading().Meter())
				r.Equal(float32(42), res.Reading().Temperature())

				// GetSetter returns a capability; calling it round-trips a second
				// unary call over the same session.
				res2, err := mc.GetSetter(ctx, "test")
				r.NoError(err)

				res3, err := res2.Setter().SetTemp(ctx, 100)
				r.NoError(err)
				r.Equal(int32(100), res3.Temp())
			})

			t.Run("server calls back on a client-advertised capability", func(t *testing.T) {
				r := require.New(t)
				ctx := t.Context()

				remote := netServer(t, ctx, tr, "meter", example.AdaptMeterUpdates(&exampleMU{}))
				c := netClient(t, ctx, remote, "meter")

				mc := &example.MeterUpdatesClient{Client: c}

				var up exampleUpdate

				// The server invokes up.Update over the same connection — the core
				// bidirectional capability/callback behavior.
				_, err := mc.RegisterUpdates(ctx, &up)
				r.NoError(err)

				r.True(up.gotIt)
				r.Equal("test", up.reading.Meter())
				r.Equal(float32(42), up.reading.Temperature())

				time.Sleep(100 * time.Millisecond) // wait for the goroutine running Close

				up.mu.Lock()
				defer up.mu.Unlock()
				r.True(up.closed)
			})

			t.Run("streaming callbacks", func(t *testing.T) {
				r := require.New(t)
				ctx := t.Context()

				remote := netServer(t, ctx, tr, "meter", example.AdaptEmitTemps(&exampleEmit{}))
				c := netClient(t, ctx, remote, "meter")

				mc := &example.EmitTempsClient{Client: c}

				var (
					mu   sync.Mutex
					vals []float32
				)
				recv := stream.StreamRecv(func(val float32) error {
					mu.Lock()
					defer mu.Unlock()
					vals = append(vals, val)
					return nil
				})

				_, err := mc.Emit(ctx, recv)
				r.NoError(err)

				time.Sleep(time.Second)

				mu.Lock()
				defer mu.Unlock()
				r.Equal([]float32{42, 100}, vals)
			})

			t.Run("concurrent callstreams share one connection", func(t *testing.T) {
				r := require.New(t)
				ctx := t.Context()

				remote := netServer(t, ctx, tr, "meter", example.AdaptEmitTemps(&exampleEmit{}))
				c := netClient(t, ctx, remote, "meter")

				const calls = 8

				var (
					wg   sync.WaitGroup
					mu   sync.Mutex
					errs []error
					got  = make([][]float32, calls)
				)

				for i := range calls {
					wg.Add(1)
					go func() {
						defer wg.Done()

						var (
							vmu  sync.Mutex
							vals []float32
						)
						recv := stream.StreamRecv(func(val float32) error {
							vmu.Lock()
							defer vmu.Unlock()
							vals = append(vals, val)
							return nil
						})

						mc := &example.EmitTempsClient{Client: c}
						if _, err := mc.Emit(ctx, recv); err != nil {
							mu.Lock()
							errs = append(errs, err)
							mu.Unlock()
							return
						}

						time.Sleep(500 * time.Millisecond)

						vmu.Lock()
						defer vmu.Unlock()
						got[i] = append([]float32(nil), vals...)
					}()
				}

				wg.Wait()

				mu.Lock()
				defer mu.Unlock()
				r.Empty(errs)

				for i := range calls {
					r.Equal([]float32{42, 100}, got[i], "call %d received the wrong values", i)
				}
			})

			t.Run("cancelled callstream reports closed", func(t *testing.T) {
				r := require.New(t)
				ctx := t.Context()

				m := &blockingMU{started: make(chan struct{})}
				remote := netServer(t, ctx, tr, "meter", example.AdaptMeterUpdates(m))
				c := netClient(t, ctx, remote, "meter")

				mc := &example.MeterUpdatesClient{Client: c}

				cctx, cancel := context.WithCancel(ctx)
				go func() {
					<-m.started
					time.Sleep(50 * time.Millisecond)
					cancel()
				}()

				var up exampleUpdate
				_, err := mc.RegisterUpdates(cctx, &up)
				r.Error(err)
				r.True(errors.Is(err, cond.ErrClosed{}), "expected ErrClosed, got %T: %v", err, err)
			})

			t.Run("propagates panic errors", func(t *testing.T) {
				r := require.New(t)
				ctx := t.Context()

				remote := netServer(t, ctx, tr, "meter", example.AdaptMeter(&panicMeter{}))
				c := netClient(t, ctx, remote, "meter")

				mc := &example.MeterClient{Client: c}

				_, err := mc.ReadTemperature(ctx, "test")
				r.Error(err)

				var panicErr cond.ErrPanic
				r.True(errors.As(err, &panicErr), "expected ErrPanic, got %T: %v", err, err)
				r.Contains(panicErr.Message, "test panic message")
			})
		})
	}
}
