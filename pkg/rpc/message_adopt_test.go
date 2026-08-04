package rpc_test

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/example"
	"miren.dev/runtime/pkg/rpc/stream"
)

// envelopeConn is a MessageConn owned by the test rather than by rpc, standing
// in for a socket carrying somebody else's envelope protocol: every payload is
// wrapped on the way out and unwrapped on the way in, and rpc never sees the
// underlying pipe. Critically there is no way to open a second one — if rpc
// tried to dial another connection for a callstream, these tests would fail.
type envelopeConn struct {
	mu   sync.Mutex
	peer *envelopeConn

	recv chan []byte
	done chan struct{}
	once sync.Once

	// envelopes counts payloads that made a round trip through the wrapper, so
	// a test can assert traffic actually crossed the boundary.
	envelopes *int64
	countMu   *sync.Mutex
}

const envelopePrefix = "ENV:"

func newEnvelopePair() (*envelopeConn, *envelopeConn) {
	var (
		count int64
		cmu   sync.Mutex
	)

	done := make(chan struct{})
	a := &envelopeConn{recv: make(chan []byte, 64), done: done, envelopes: &count, countMu: &cmu}
	b := &envelopeConn{recv: make(chan []byte, 64), done: done, envelopes: &count, countMu: &cmu}
	a.peer, b.peer = b, a

	return a, b
}

func (c *envelopeConn) Send(b []byte) error {
	// Wrap: this is the caller's protocol, opaque to rpc.
	framed := append([]byte(envelopePrefix), b...)

	c.countMu.Lock()
	*c.envelopes++
	c.countMu.Unlock()

	c.mu.Lock()
	peer := c.peer
	c.mu.Unlock()

	select {
	case peer.recv <- framed:
		return nil
	case <-c.done:
		return net.ErrClosed
	}
}

func (c *envelopeConn) Recv() ([]byte, error) {
	select {
	case b := <-c.recv:
		return b[len(envelopePrefix):], nil // unwrap
	case <-c.done:
		select {
		case b := <-c.recv:
			return b[len(envelopePrefix):], nil
		default:
			return nil, io.EOF
		}
	}
}

func (c *envelopeConn) Close() error {
	c.once.Do(func() { close(c.done) })
	return nil
}

func (c *envelopeConn) count() int64 {
	c.countMu.Lock()
	defer c.countMu.Unlock()
	return *c.envelopes
}

// recordingAuthenticator returns a fixed identity and remembers the credentials
// it was handed, so a test can assert what actually reached it.
type recordingAuthenticator struct {
	identity *rpc.Identity

	mu   sync.Mutex
	last *rpc.Credentials
}

func (a *recordingAuthenticator) Authenticate(
	ctx context.Context,
	creds *rpc.Credentials,
) (*rpc.Identity, error) {
	a.mu.Lock()
	a.last = creds
	a.mu.Unlock()

	return a.identity, nil
}

func (a *recordingAuthenticator) lastAuthorization() string {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.last == nil {
		return ""
	}
	return a.last.Authorization
}

// denyAll rejects every authorization check.
type denyAll struct{}

func (denyAll) Authorize(ctx context.Context, identity *rpc.Identity, resource, action string) error {
	return errors.New("denied by policy")
}

// identityMeter records the identity its handler ran under.
type identityMeter struct {
	seen **rpc.Identity
}

func (m *identityMeter) ReadTemperature(ctx context.Context, call *example.MeterReadTemperature) error {
	*m.seen = rpc.IdentityFromContext(ctx)

	res := call.Results()
	reading := new(example.Reading)
	reading.SetMeter(call.Args().Name())
	reading.SetTemperature(1)
	res.SetReading(reading)

	return nil
}

func (m *identityMeter) GetSetter(ctx context.Context, call *example.MeterGetSetter) error {
	return nil
}

// adoptPair wires two States over one caller-owned connection: serverEnd is
// served, clientEnd is dialled. Returns the client for the named object.
func adoptPair(
	t *testing.T,
	ctx context.Context,
	iface *rpc.Interface,
	name string,
	opts ...rpc.MessageSessionOption,
) (rpc.Client, *envelopeConn) {
	t.Helper()
	r := require.New(t)

	ss, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(err)
	ss.Server().ExposeValue(name, iface)

	cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(err)

	serverEnd, clientEnd := newEnvelopePair()

	go ss.ServeMessageConn(ctx, serverEnd, opts...) //nolint:errcheck // ends with ctx

	c, err := cs.ClientFromMessageConn(ctx, clientEnd, name, opts...)
	r.NoError(err)

	return c, clientEnd
}

func TestAdoptedMessageConn(t *testing.T) {
	t.Run("unary calls over a caller-owned connection", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		c, conn := adoptPair(t, ctx, example.AdaptMeter(&exampleMeter{temp: 42}), "meter")

		mc := &example.MeterClient{Client: c}

		res, err := mc.ReadTemperature(ctx, "test")
		r.NoError(err)
		r.Equal("test", res.Reading().Meter())
		r.Equal(float32(42), res.Reading().Temperature())

		r.NotZero(conn.count(), "traffic should have crossed the envelope boundary")
	})

	// A capability returned from a call carries no dialable address on an adopted
	// connection, so following it must reuse the session it arrived on.
	t.Run("follows a returned capability over the same connection", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		c, _ := adoptPair(t, ctx, example.AdaptMeter(&exampleMeter{temp: 42}), "meter")

		mc := &example.MeterClient{Client: c}

		res, err := mc.GetSetter(ctx, "test")
		r.NoError(err)

		res2, err := res.Setter().SetTemp(ctx, 100)
		r.NoError(err)
		r.Equal(int32(100), res2.Temp())
	})

	t.Run("server calls back over the same connection", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		c, _ := adoptPair(t, ctx, example.AdaptMeterUpdates(&exampleMU{}), "meter")

		mc := &example.MeterUpdatesClient{Client: c}

		var up exampleUpdate
		_, err := mc.RegisterUpdates(ctx, &up)
		r.NoError(err)

		r.True(up.gotIt)
		r.Equal("test", up.reading.Meter())
		r.Equal(float32(42), up.reading.Temperature())
	})

	t.Run("streaming callbacks over a caller-owned connection", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		c, _ := adoptPair(t, ctx, example.AdaptEmitTemps(&exampleEmit{}), "meter")

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

		time.Sleep(500 * time.Millisecond)

		mu.Lock()
		defer mu.Unlock()
		r.Equal([]float32{42, 100}, vals)
	})

	// The whole point of an adopted connection: there is no second connection to
	// dial, so every concurrent callstream must multiplex onto the one we were
	// given.
	t.Run("concurrent callstreams multiplex onto the one connection", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		c, _ := adoptPair(t, ctx, example.AdaptEmitTemps(&exampleEmit{}), "meter")

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

	// A message transport has no Authorization header and no TLS handshake, so a
	// bearer token rides in the operation frame instead. Without this, cloudauth
	// and oidcauth cannot identify a caller over one of these connections at all.
	t.Run("carries a bearer token to the authenticator", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		auth := &recordingAuthenticator{
			identity: &rpc.Identity{Subject: "user-42", Method: rpc.AuthMethodJWT},
		}

		ss, err := rpc.NewState(ctx, rpc.WithSkipVerify, rpc.WithAuthenticator(auth))
		r.NoError(err)

		var seen *rpc.Identity
		iface := example.AdaptMeter(&identityMeter{seen: &seen})
		ss.Server().ExposeValue("meter", iface)

		cs, err := rpc.NewState(ctx, rpc.WithSkipVerify, rpc.WithBearerToken("secret-token"))
		r.NoError(err)

		serverEnd, clientEnd := newEnvelopePair()
		go ss.ServeMessageConn(ctx, serverEnd) //nolint:errcheck // ends with ctx

		c, err := cs.ClientFromMessageConn(ctx, clientEnd, "meter")
		r.NoError(err)

		mc := &example.MeterClient{Client: c}
		_, err = mc.ReadTemperature(ctx, "test")
		r.NoError(err)

		r.Equal("Bearer secret-token", auth.lastAuthorization())
		r.NotNil(seen)
		r.Equal("user-42", seen.Subject)
		r.Equal(rpc.AuthMethodJWT, seen.Method)
	})

	// A token that is present but fails to authenticate must fail the call, not
	// silently downgrade to the (weaker) capability identity.
	t.Run("rejects a present-but-invalid bearer token", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		// identity nil => the token did not authenticate.
		auth := &recordingAuthenticator{identity: nil}

		ss, err := rpc.NewState(ctx, rpc.WithSkipVerify, rpc.WithAuthenticator(auth))
		r.NoError(err)

		var seen *rpc.Identity
		ss.Server().ExposeValue("meter", example.AdaptMeter(&identityMeter{seen: &seen}))

		cs, err := rpc.NewState(ctx, rpc.WithSkipVerify, rpc.WithBearerToken("bad-token"))
		r.NoError(err)

		serverEnd, clientEnd := newEnvelopePair()
		go ss.ServeMessageConn(ctx, serverEnd) //nolint:errcheck // ends with ctx

		c, err := cs.ClientFromMessageConn(ctx, clientEnd, "meter")
		r.NoError(err)

		mc := &example.MeterClient{Client: c}
		_, err = mc.ReadTemperature(ctx, "test")
		r.Error(err)
		r.Nil(seen, "handler must not run on an invalid token")
	})

	// An authorization denial must reach the caller as a permission error. It
	// travels as its own opReply status, so without explicit handling it surfaces
	// as an unrecognised-protocol-status error instead.
	t.Run("reports an authorization denial as a permission error", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		ss, err := rpc.NewState(ctx, rpc.WithSkipVerify,
			rpc.WithAuthenticator(&recordingAuthenticator{
				identity: &rpc.Identity{Subject: "nobody", Method: rpc.AuthMethodJWT},
			}),
			rpc.WithAuthorizer(denyAll{}),
		)
		r.NoError(err)
		ss.Server().ExposeValue("meter", example.AdaptMeter(&exampleMeter{temp: 42}))

		cs, err := rpc.NewState(ctx, rpc.WithSkipVerify, rpc.WithBearerToken("token"))
		r.NoError(err)

		serverEnd, clientEnd := newEnvelopePair()
		go ss.ServeMessageConn(ctx, serverEnd) //nolint:errcheck // ends with ctx

		c, err := cs.ClientFromMessageConn(ctx, clientEnd, "meter")
		r.NoError(err)

		mc := &example.MeterClient{Client: c}
		_, err = mc.ReadTemperature(ctx, "test")
		r.Error(err)
		r.NotContains(err.Error(), "unknown response status")
		r.Contains(err.Error(), "denied by policy")
	})

	// A frame cap below the payload size forces msgmux to split a write across
	// several messages, which is what an envelope with its own size limit needs.
	t.Run("respects a frame size below the payload size", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		c, conn := adoptPair(t, ctx,
			example.AdaptMeter(&exampleMeter{temp: 42}), "meter",
			rpc.WithMaxFrameSize(64),
		)

		mc := &example.MeterClient{Client: c}

		res, err := mc.ReadTemperature(ctx, "a-meter-name-long-enough-to-need-splitting-across-frames")
		r.NoError(err)
		r.Equal(float32(42), res.Reading().Temperature())

		// With a 64-byte cap the request and response payloads each span several
		// frames, so many more envelopes cross the boundary than the handful an
		// unsplit exchange (open, opRequest, args, reply, result, close) needs. A
		// low count would mean the cap was ignored.
		r.Greater(conn.count(), int64(10))
	})
}
