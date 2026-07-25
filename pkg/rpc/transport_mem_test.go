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

// memServer registers an in-process message server under a unique name derived
// from the test, exposing the given interface, and returns its mem:// remote.
func memServer(t *testing.T, ctx context.Context, iface *rpc.Interface) string {
	t.Helper()
	r := require.New(t)

	name := "memtest-" + t.Name()
	ss, err := rpc.NewState(ctx, rpc.WithSkipVerify, rpc.WithMemServer(name))
	r.NoError(err)
	ss.Server().ExposeValue("meter", iface)

	return "mem://" + name
}

func memClient(t *testing.T, ctx context.Context, remote, name string) rpc.Client {
	t.Helper()
	r := require.New(t)

	cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(err)

	c, err := cs.Connect(remote, name)
	r.NoError(err)
	return c
}

func TestMemTransport(t *testing.T) {
	t.Run("unary calls over message transport", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		remote := memServer(t, ctx, example.AdaptMeter(&exampleMeter{temp: 42}))
		c := memClient(t, ctx, remote, "meter")

		mc := &example.MeterClient{Client: c}

		res, err := mc.ReadTemperature(ctx, "test")
		r.NoError(err)
		r.Equal("test", res.Reading().Meter())
		r.Equal(float32(42), res.Reading().Temperature())

		// GetSetter returns a capability; calling it follows the cap over mem://.
		res2, err := mc.GetSetter(ctx, "test")
		r.NoError(err)
		res3, err := res2.Setter().SetTemp(ctx, 100)
		r.NoError(err)
		r.Equal(int32(100), res3.Temp())
	})

	t.Run("server calls back on a client-advertised capability", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		remote := memServer(t, ctx, example.AdaptMeterUpdates(&exampleMU{}))
		c := memClient(t, ctx, remote, "meter")

		mc := &example.MeterUpdatesClient{Client: c}

		var up exampleUpdate
		_, err := mc.RegisterUpdates(ctx, &up)
		r.NoError(err)

		r.True(up.gotIt)
		r.Equal("test", up.reading.Meter())
		r.Equal(float32(42), up.reading.Temperature())

		time.Sleep(100 * time.Millisecond)

		up.mu.Lock()
		defer up.mu.Unlock()
		r.True(up.closed)
	})

	t.Run("streaming callbacks over message transport", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		remote := memServer(t, ctx, example.AdaptEmitTemps(&exampleEmit{}))
		c := memClient(t, ctx, remote, "meter")

		mc := &example.EmitTempsClient{Client: c}

		var vals []float32
		recv := stream.StreamRecv(func(val float32) error {
			vals = append(vals, val)
			return nil
		})

		_, err := mc.Emit(ctx, recv)
		r.NoError(err)

		time.Sleep(time.Second)
		r.Equal([]float32{42, 100}, vals)
	})

	// Callstreams share the one session wrapping the connection, so concurrent
	// streaming calls must not steal each other's inbound callbacks. This is the
	// regression test for callstreams no longer dialing a connection each.
	t.Run("concurrent callstreams share one connection", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		remote := memServer(t, ctx, example.AdaptEmitTemps(&exampleEmit{}))
		c := memClient(t, ctx, remote, "meter")

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

				// Each call advertises its own emitter capability. They all ride
				// the same session, so the session must route each server-initiated
				// callback back to the call that advertised it.
				var vals []float32
				recv := stream.StreamRecv(func(val float32) error {
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
				got[i] = vals
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

	t.Run("a capability can be passed to a 3rd party", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		var em exampleMeter
		em.temp = 42

		remote := memServer(t, ctx, example.AdaptMeter(&em))
		c := memClient(t, ctx, remote, "meter")

		mc := &example.MeterClient{Client: c}

		res2, err := mc.GetSetter(ctx, "test")
		r.NoError(err)

		// Second mem server hosting the AdjustTemp service.
		s2, err := rpc.NewState(ctx, rpc.WithSkipVerify, rpc.WithMemServer("memtest-adjust-"+t.Name()))
		r.NoError(err)
		s2.Server().ExposeValue("adjust", example.AdaptAdjustTemp(&exampleAT{}))

		c2, err := s2.Connect("mem://memtest-adjust-"+t.Name(), "adjust")
		r.NoError(err)

		ac := &example.AdjustTempClient{Client: c2}

		_, err = ac.Adjust(ctx, res2.Setter().Export())
		r.NoError(err)

		r.Equal(float32(72), em.temp)
	})

	t.Run("cancelled callstream reports closed", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		m := &blockingMU{started: make(chan struct{})}
		remote := memServer(t, ctx, example.AdaptMeterUpdates(m))
		c := memClient(t, ctx, remote, "meter")

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

	t.Run("propagates panic errors over message transport", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		remote := memServer(t, ctx, example.AdaptMeter(&panicMeter{}))
		c := memClient(t, ctx, remote, "meter")

		mc := &example.MeterClient{Client: c}

		_, err := mc.ReadTemperature(ctx, "test")
		r.Error(err)

		var panicErr cond.ErrPanic
		r.True(errors.As(err, &panicErr), "expected ErrPanic, got %T: %v", err, err)
		r.Contains(panicErr.Message, "test panic message")
	})
}
